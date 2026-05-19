// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Copyright (C) 2026 Amken LLC <https://www.amken.us>
//
// This file is part of the ticktrace Assembly SDK.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see
// <https://www.gnu.org/licenses/>.
//
// A commercial license is available from Amken LLC for use cases that
// cannot comply with the AGPL. See COMMERCIAL-LICENSE.md.

package build_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amken3d/rp-asm/studio/internal/build"
	"github.com/amken3d/rp-asm/studio/internal/catalog"
	"github.com/amken3d/rp-asm/studio/internal/project"
)

// TestGoldenBlinky asserts that rpasm's blinky output is byte-identical to
// the Makefile's. Requires arm-none-eabi-as on PATH and that `make build/blinky.bin`
// has been run from the parent ticktrace directory beforehand (we run it).
func TestGoldenBlinky(t *testing.T) {
	if _, err := exec.LookPath("arm-none-eabi-as"); err != nil {
		t.Skip("arm-none-eabi-as not on PATH")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(root)
	makefile := filepath.Join(parent, "Makefile")
	if _, err := os.Stat(makefile); err != nil {
		t.Skipf("no Makefile at %s: %v", makefile, err)
	}

	mk := exec.Command("make", "-C", parent, "build/blinky.bin", "build/blinky.uf2")
	mk.Stderr = os.Stderr
	if err := mk.Run(); err != nil {
		t.Fatalf("make: %v", err)
	}
	wantBin := filepath.Join(parent, "build", "blinky.bin")
	wantUf2 := filepath.Join(parent, "build", "blinky.uf2")
	wantBinBytes, err := os.ReadFile(wantBin)
	if err != nil {
		t.Fatal(err)
	}
	wantUf2Bytes, err := os.ReadFile(wantUf2)
	if err != nil {
		t.Fatal(err)
	}

	cat, err := catalog.Load(filepath.Join(root, "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.Load(filepath.Join(root, "testdata", "blinky.rpasm.toml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := project.Resolve(proj, cat)
	if err != nil {
		t.Fatal(err)
	}
	tc, err := build.Detect(res.Target.ToolchainPrefix)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	result, err := build.Build(&build.Options{
		Resolved:  res,
		Root:      root,
		SDKRoot:   parent,
		OutDir:    outDir,
		Toolchain: tc,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	gotBin, err := os.ReadFile(result.Bin)
	if err != nil {
		t.Fatal(err)
	}
	gotUf2, err := os.ReadFile(result.Uf2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(gotBin, wantBinBytes) {
		t.Errorf(".bin differs: got %d bytes, want %d bytes", len(gotBin), len(wantBinBytes))
	}
	if !bytes.Equal(gotUf2, wantUf2Bytes) {
		t.Errorf(".uf2 differs: got %d bytes, want %d bytes", len(gotUf2), len(wantUf2Bytes))
	}
}
