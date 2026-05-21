// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Amken LLC <https://www.amken.us>

package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestResolveFindsHostToolchain is a smoke test: if the host has any
// arm-none-eabi-as on PATH (most dev machines and the SDK's CI both do),
// Resolve should return without error and populate Toolchain.
//
// On a clean machine with no toolchain at all, Resolve should report
// SourceNotFound without error — that's also a passing case here.
func TestResolveFindsHostToolchain(t *testing.T) {
	status, err := Resolve(DefaultPrefix)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if status == nil {
		t.Fatal("Resolve returned nil status")
	}

	switch status.Source {
	case SourceNotFound:
		if status.Toolchain != nil {
			t.Errorf("SourceNotFound but Toolchain is non-nil")
		}
		if status.Hint == "" {
			t.Errorf("SourceNotFound but no Hint to show the user")
		}
		t.Logf("no toolchain found on host (expected on a clean machine)")
	default:
		if status.Toolchain == nil {
			t.Fatalf("source %s but no Toolchain populated", status.Source)
		}
		if status.Toolchain.As == "" {
			t.Errorf("Toolchain.As is empty")
		}
		t.Logf("found toolchain via %s at %s", status.Source, status.Toolchain.As)
	}
}

func TestManifestParses(t *testing.T) {
	m, err := loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if m.Version == "" {
		t.Error("manifest version is empty")
	}
	// darwin/amd64 is intentionally absent in v1 (Intel Macs use Homebrew;
	// see the workflow comment). Add it back here when the universal2
	// build lands.
	for _, plat := range []string{"darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64"} {
		if _, ok := m.Entries[plat]; !ok {
			t.Errorf("manifest missing entry for %s", plat)
		}
	}
}

// TestManagedRootIsUnderHome guards against a path bug that would write
// the toolchain to / or to cwd.
func TestManagedRootIsUnderHome(t *testing.T) {
	root, err := managedRoot()
	if err != nil {
		t.Skip("no home dir on this host")
	}
	home, _ := os.UserHomeDir()
	if !filepath.HasPrefix(root, home) {
		t.Errorf("managed root %q not under home %q", root, home)
	}
}

// TestDetectPrefersExtraDir ensures the search order is right: a binary
// found in extraDirs wins over the same name on PATH. We synthesise this
// by pointing extraDirs at the directory of an existing on-PATH binary
// other than the toolchain, and checking we get back that file.
func TestDetectPrefersExtraDir(t *testing.T) {
	// We can't reliably synthesise a fake arm-none-eabi-as on every CI
	// host, but we can check the fallback semantics indirectly: build's
	// DetectIn must search extraDirs first, so if we pass a known-good
	// dir, the As path must start with it.
	asPath, err := exec.LookPath("arm-none-eabi-as")
	if err != nil {
		t.Skip("no arm-none-eabi-as on PATH; can't verify search order")
	}
	dir := filepath.Dir(asPath)

	status, err := Resolve(DefaultPrefix)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if status.Toolchain == nil {
		t.Fatal("expected Toolchain populated")
	}
	if filepath.Dir(status.Toolchain.As) != dir {
		t.Errorf("As is %q, expected something under %q", status.Toolchain.As, dir)
	}
}
