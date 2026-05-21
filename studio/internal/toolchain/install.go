// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Amken LLC <https://www.amken.us>

package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// installRelease performs the full install flow: download to a temp file,
// verify SHA-256, extract under a sibling temp directory, and rename into
// place atomically. If anything fails partway, the temp artifacts are
// cleaned up and the existing install (if any) is left untouched.
//
// progress is called as bytes stream from the HTTP response; it may be
// nil. ctx cancellation aborts the download.
func installRelease(ctx context.Context, e Entry, dest string, progress func(done, total int64)) error {
	// Fast path: if dest already has a working bin/arm-none-eabi-as, skip
	// the network round-trip. Lets Install be idempotent (the UI can call
	// it on every boot without re-downloading).
	if existingAsValid(dest) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmpArchive, err := os.CreateTemp(filepath.Dir(dest), ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpArchive.Name())
	defer tmpArchive.Close()

	if err := downloadVerified(ctx, e, tmpArchive, progress); err != nil {
		return err
	}
	if _, err := tmpArchive.Seek(0, io.SeekStart); err != nil {
		return err
	}

	stage, err := os.MkdirTemp(filepath.Dir(dest), ".unpack-*")
	if err != nil {
		return err
	}
	cleanupStage := true
	defer func() {
		if cleanupStage {
			os.RemoveAll(stage)
		}
	}()

	switch e.Archive {
	case "tar.gz":
		if err := extractTarGz(tmpArchive, stage, e.StripComponents); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	case "zip":
		fi, _ := tmpArchive.Stat()
		if err := extractZip(tmpArchive.Name(), fi.Size(), stage, e.StripComponents); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	default:
		return fmt.Errorf("unsupported archive type %q", e.Archive)
	}

	// Atomic-ish swap. If dest exists, move it aside and only delete the
	// old copy once the new one is in place — protects against the user
	// reinstalling on top of a working toolchain and losing both copies.
	var backup string
	if _, err := os.Stat(dest); err == nil {
		backup = dest + ".old"
		os.RemoveAll(backup)
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("snapshot previous install: %w", err)
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		if backup != "" {
			os.Rename(backup, dest) // best-effort rollback
		}
		return fmt.Errorf("install: %w", err)
	}
	cleanupStage = false
	if backup != "" {
		os.RemoveAll(backup)
	}
	return nil
}

func existingAsValid(dest string) bool {
	as := filepath.Join(dest, "bin", "arm-none-eabi-as")
	if _, err := os.Stat(as); err == nil {
		return true
	}
	if _, err := os.Stat(as + ".exe"); err == nil {
		return true
	}
	return false
}

// downloadVerified streams the URL into w while computing SHA-256, then
// fails if the digest doesn't match e.SHA256. We never write the archive
// to its final extracted form before this check passes.
func downloadVerified(ctx context.Context, e Entry, w io.Writer, progress func(done, total int64)) error {
	if e.SHA256 == "" {
		return fmt.Errorf("manifest entry has no sha256 (refusing to install unverified binary from %s)", e.URL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ticktrace-studio/installer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", e.URL, resp.StatusCode)
	}

	total := e.Size
	if total <= 0 {
		total = resp.ContentLength // may be -1 if chunked
	}

	hasher := sha256.New()
	pr := &progressReader{
		r:        resp.Body,
		total:    total,
		callback: progress,
	}
	if _, err := io.Copy(io.MultiWriter(w, hasher), pr); err != nil {
		return err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, e.SHA256) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, e.SHA256)
	}
	return nil
}

type progressReader struct {
	r        io.Reader
	done     int64
	total    int64
	callback func(done, total int64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.done += int64(n)
		if p.callback != nil {
			p.callback(p.done, p.total)
		}
	}
	return n, err
}

// extractTarGz unpacks src into dst, stripping the first stripComponents
// path elements. Symlinks pointing outside dst are rejected (zip-slip).
func extractTarGz(src io.Reader, dst string, stripComponents int) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name, ok := stripLeading(hdr.Name, stripComponents)
		if !ok {
			continue
		}
		target := filepath.Join(dst, name)
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) && target != filepath.Clean(dst) {
			return fmt.Errorf("entry escapes destination: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0o777|0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Reject symlinks that resolve outside dst.
			abs := filepath.Join(filepath.Dir(target), hdr.Linkname)
			if !strings.HasPrefix(filepath.Clean(abs), filepath.Clean(dst)) {
				return fmt.Errorf("symlink escapes destination: %s -> %s", hdr.Name, hdr.Linkname)
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// extractZip unpacks the zip at path (size bytes) into dst. Windows is
// the primary target; symlinks are not honored.
func extractZip(path string, size int64, dst string, stripComponents int) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name, ok := stripLeading(f.Name, stripComponents)
		if !ok {
			continue
		}
		target := filepath.Join(dst, name)
		if !strings.HasPrefix(target, filepath.Clean(dst)+string(os.PathSeparator)) && target != filepath.Clean(dst) {
			return fmt.Errorf("entry escapes destination: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

// stripLeading removes the first n path components from name. Returns
// ("", false) if the path is shorter than n components, meaning the
// caller should skip the entry (it's a parent dir we're flattening away).
func stripLeading(name string, n int) (string, bool) {
	if n <= 0 {
		return filepath.FromSlash(name), true
	}
	parts := strings.Split(strings.TrimPrefix(filepath.ToSlash(name), "./"), "/")
	if len(parts) <= n {
		return "", false
	}
	return filepath.Join(parts[n:]...), true
}
