// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Amken LLC <https://www.amken.us>

package toolchain

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Manifest describes one pinned cross-platform release of the ARM GNU
// toolchain. Studio downloads the entry that matches the current host and
// extracts it under ~/.ticktrace/toolchain/<Version>/. Pinning the version
// here (rather than tracking "latest") means a given Studio build always
// produces byte-identical firmware on every developer's machine.
type Manifest struct {
	// Version is both the user-visible label ("14.2.1-1.1") and the
	// directory name under ~/.ticktrace/toolchain/.
	Version string `json:"version"`

	// Source identifies the upstream release we're pinning. Useful for
	// audit logs and for users who want to verify the URLs against
	// upstream's own SHASUMS file before trusting our manifest.
	Source string `json:"source"`

	// Entries is keyed by "<goos>/<goarch>" e.g. "darwin/arm64".
	Entries map[string]Entry `json:"entries"`
}

// Entry is one platform's download.
type Entry struct {
	// URL is the direct download URL. Must be HTTPS.
	URL string `json:"url"`

	// SHA256 is the lowercase hex digest of the archive. Empty values
	// are rejected by Install — we never run an unverified binary.
	SHA256 string `json:"sha256"`

	// Archive declares how to unpack. "tar.gz" or "zip".
	Archive string `json:"archive"`

	// StripComponents removes N leading path components when extracting,
	// the same way `tar --strip-components` does. Most upstream archives
	// have a single top-level directory we want to skip so that bin/
	// lands directly under ~/.ticktrace/toolchain/<version>/.
	StripComponents int `json:"strip_components"`

	// Size is the expected archive size in bytes. Used to drive accurate
	// progress reporting; 0 means "unknown, fall back to Content-Length".
	Size int64 `json:"size"`
}

//go:embed manifest.json
var embeddedManifest []byte

func loadManifest() (*Manifest, error) {
	m := &Manifest{}
	if err := json.Unmarshal(embeddedManifest, m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest version is empty")
	}
	if len(m.Entries) == 0 {
		return nil, fmt.Errorf("manifest has no entries")
	}
	return m, nil
}

// entryFor returns the manifest entry for a platform plus a status:
//   ok=true        - entry exists and is shippable.
//   ok=false, reason=="missing" - no entry at all for that platform.
//   ok=false, reason=="unconfigured" - entry exists but URL/SHA256 are
//     placeholders. Lets the caller print "fill in manifest.json" rather
//     than the misleading "no managed toolchain for platform".
func (m *Manifest) entryFor(platform string) (Entry, string, bool) {
	e, ok := m.Entries[platform]
	if !ok {
		return Entry{}, "missing", false
	}
	if e.URL == "" || e.SHA256 == "" {
		return Entry{}, "unconfigured", false
	}
	return e, "", true
}
