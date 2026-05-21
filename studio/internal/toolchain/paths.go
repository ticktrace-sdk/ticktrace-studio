// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Amken LLC <https://www.amken.us>

package toolchain

import (
	"os"
	"path/filepath"
	"runtime"
)

// candidate is one location to probe for a usable toolchain, paired with
// the Source we'll report if it matches.
type candidate struct {
	source Source
	dir    string
}

// candidates returns the ordered list of directories to search before
// falling back to PATH. Each entry is platform-specific; non-existent
// dirs are kept (build.DetectIn just won't find anything in them).
//
// Order matters: a Studio-managed install beats a system-managed one so
// that "Install via Studio" gives users a deterministic, version-pinned
// experience even when Homebrew also has a (potentially older) toolchain.
func candidates() []candidate {
	var out []candidate

	if dir, ok := managedBinDir(); ok {
		out = append(out, candidate{source: SourceManaged, dir: dir})
	}

	if dir, ok := bundledBinDir(); ok {
		out = append(out, candidate{source: SourceBundled, dir: dir})
	}

	out = append(out, platformCandidates()...)
	return out
}

// managedBinDir returns the bin/ directory of the most-recently-installed
// Studio-managed toolchain under ~/.ticktrace/toolchain/, or false if no
// such install exists yet.
func managedBinDir() (string, bool) {
	root, err := managedRoot()
	if err != nil {
		return "", false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	// Pick the newest by mtime. Almost always there's only one, but if
	// the user has flipped versions we want the latest install rather
	// than alphabetical order (14.2.1 > 14.10.0 lexicographically, oops).
	var best os.DirEntry
	var bestTime int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if best == nil || fi.ModTime().UnixNano() > bestTime {
			best = e
			bestTime = fi.ModTime().UnixNano()
		}
	}
	if best == nil {
		return "", false
	}
	return filepath.Join(root, best.Name(), "bin"), true
}

// bundledBinDir returns the bin/ of a toolchain shipped next to the
// Studio executable, if any. Used by full-fat installers that bundle
// the toolchain so first-run works offline.
func bundledBinDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(dir, "toolchain", "bin"),
		filepath.Join(dir, "..", "toolchain", "bin"),
		// macOS .app bundle: Contents/Resources/toolchain/bin
		filepath.Join(dir, "..", "Resources", "toolchain", "bin"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs, true
		}
	}
	return "", false
}

// platformCandidates returns OS-specific locations to check for an
// existing toolchain installed outside of Studio (Homebrew, ARM's own
// installer, scoop, etc.). Empty on Linux because apt-installed binutils
// is already on PATH and gets picked up by the bare-PATH fallback.
func platformCandidates() []candidate {
	switch runtime.GOOS {
	case "darwin":
		return []candidate{
			{SourceHomebrew, "/opt/homebrew/bin"},   // Apple Silicon brew
			{SourceHomebrew, "/usr/local/bin"},      // Intel brew
			{SourceArmOff, "/Applications/ARM/bin"}, // ARM official .pkg
		}
	case "windows":
		out := []candidate{}
		// ARM official installer drops into "Program Files (x86)\Arm GNU
		// Toolchain arm-none-eabi\<version>\bin" — glob the parent.
		for _, base := range []string{
			os.Getenv("ProgramFiles") + `\Arm GNU Toolchain arm-none-eabi`,
			os.Getenv("ProgramFiles(x86)") + `\Arm GNU Toolchain arm-none-eabi`,
		} {
			if base == `\Arm GNU Toolchain arm-none-eabi` {
				continue // env var was empty
			}
			matches, _ := filepath.Glob(filepath.Join(base, "*", "bin"))
			for _, m := range matches {
				out = append(out, candidate{SourceArmOff, m})
			}
		}
		// scoop: ~/scoop/apps/<pkg>/current/bin
		if home, err := os.UserHomeDir(); err == nil {
			matches, _ := filepath.Glob(filepath.Join(home, "scoop", "apps", "*", "current", "bin"))
			for _, m := range matches {
				out = append(out, candidate{SourceScoop, m})
			}
		}
		return out
	}
	return nil
}

// managedRoot is ~/.ticktrace/toolchain. Created lazily by Install.
func managedRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ticktrace", "toolchain"), nil
}

// managedDir is the per-version subdirectory under managedRoot. Install
// extracts the release into a tmp sibling and renames it into place to
// keep the on-disk state always-consistent.
func managedDir(version string) (string, error) {
	root, err := managedRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, version), nil
}

// currentPlatform returns the manifest key for this host, e.g.
// "darwin/arm64" or "windows/amd64".
func currentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
