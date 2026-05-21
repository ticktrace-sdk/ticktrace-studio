// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Copyright (C) 2026 Amken LLC <https://www.amken.us>
//
// This file is part of the ticktrace Assembly SDK.
// Licensed under AGPL-3.0-or-later; commercial license available.
// See LICENSE and COMMERCIAL-LICENSE.md in the root of this repository.

// Package toolchain finds, installs, and manages the ARM none-eabi
// toolchain that the SDK build needs (arm-none-eabi-as / -ld / -objcopy).
//
// It is the higher-level companion to internal/build.Detect:
//
//   - Resolve searches every plausible location for a usable toolchain
//     (Studio-managed, bundled next to the executable, Homebrew, ARM
//     official installer, scoop, PATH) and returns a structured Status.
//
//   - Install downloads the pinned cross-platform GCC release for the
//     current OS/arch, verifies its SHA-256, and extracts it atomically
//     under ~/.ticktrace/toolchain/<version>/. The UI calls this when
//     Resolve reports the toolchain is missing.
//
// build.Detect remains the low-level primitive; this package layers
// search-path discovery on top.
package toolchain

import (
	"context"
	"fmt"

	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/build"
)

// DefaultPrefix is the binutils prefix the SDK uses everywhere.
const DefaultPrefix = "arm-none-eabi-"

// Source describes where a resolved toolchain came from. The UI uses this
// to render the "Toolchain: managed (v14.2.1) — change…" line in settings.
type Source string

const (
	SourceManaged   Source = "managed"   // ~/.ticktrace/toolchain/<ver>/bin
	SourceBundled   Source = "bundled"   // shipped next to the Studio executable
	SourceHomebrew  Source = "homebrew"  // /opt/homebrew/bin or /usr/local/bin
	SourceArmOff    Source = "arm"       // ARM's official Windows/macOS installer
	SourceScoop     Source = "scoop"     // Windows scoop shims
	SourcePath      Source = "path"      // anywhere else on $PATH
	SourceNotFound  Source = "not-found" // missing — UI should offer Install
)

// Status describes the result of Resolve. If Toolchain is non-nil the SDK
// can build immediately; if it's nil, Source == SourceNotFound and the UI
// should call Install (or let the user point at an existing directory).
type Status struct {
	Source    Source
	Dir       string // bin directory the binaries were found in, or "" if Source==SourceNotFound
	Toolchain *build.Toolchain
	Hint      string // human-readable next step when Toolchain is nil
}

// Resolve walks the search-path hierarchy (managed → bundled → known
// platform paths → PATH) looking for a usable arm-none-eabi toolchain.
// It never makes network calls. Returns Status with Source==SourceNotFound
// if nothing is found; callers can then surface an install prompt.
func Resolve(prefix string) (*Status, error) {
	if prefix == "" {
		prefix = DefaultPrefix
	}

	for _, c := range candidates() {
		tc, err := build.DetectIn(prefix, []string{c.dir})
		if err == nil {
			return &Status{
				Source:    c.source,
				Dir:       c.dir,
				Toolchain: tc,
			}, nil
		}
		if !build.IsMissing(err) {
			return nil, fmt.Errorf("toolchain probe %s: %w", c.dir, err)
		}
	}

	// Last resort: bare PATH lookup (covers Linux apt-installed binutils
	// and any custom $PATH the user has set up).
	if tc, err := build.Detect(prefix); err == nil {
		return &Status{
			Source:    SourcePath,
			Toolchain: tc,
		}, nil
	}

	return &Status{
		Source: SourceNotFound,
		Hint: "No ARM toolchain found. Install the managed toolchain " +
			"(~150 MB) or point Studio at an existing arm-none-eabi-as.",
	}, nil
}

// Install downloads the pinned toolchain for the current GOOS/GOARCH into
// the managed directory (~/.ticktrace/toolchain/<version>/), verifies the
// SHA-256, and extracts it atomically. Progress is reported via the
// progress callback as (bytesDone, bytesTotal); total is -1 if unknown.
// Cancellation via ctx aborts the download cleanly.
func Install(ctx context.Context, progress func(done, total int64)) (*Status, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	entry, reason, ok := m.entryFor(currentPlatform())
	if !ok {
		switch reason {
		case "unconfigured":
			return nil, fmt.Errorf("platform %s is in the manifest but its SHA256 is unset (fill in internal/toolchain/manifest.json with the upstream digest from %s)", currentPlatform(), m.Source)
		default:
			return nil, fmt.Errorf("platform %s is not in the toolchain manifest", currentPlatform())
		}
	}
	dest, err := managedDir(m.Version)
	if err != nil {
		return nil, err
	}
	if err := installRelease(ctx, entry, dest, progress); err != nil {
		return nil, err
	}
	// Re-resolve so we return a populated Toolchain pointing at the new
	// install rather than reconstructing one by hand.
	return Resolve(DefaultPrefix)
}
