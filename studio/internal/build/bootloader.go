// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Copyright (C) 2026 Amken LLC <https://amken.io>
//
// This file is part of the Amken RP2350 Assembly SDK.
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

package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/amken3d/rp-asm/tools/firmware"
	"github.com/amken3d/rp-asm/tools/manifest"
)

// Bootloader chain layout (RP2350, mirrors include/bootloader.inc and
// docs/bootloader.md). Hardcoded for v1: studio is RP2350-only and these
// addresses are wired into the SSBL/TSBL source. Lift to target TOML when
// a second board needs different geometry.
const (
	ssblBase       uint32 = 0x10000000
	tsblBase       uint32 = 0x10001000
	tsblFooterAddr uint32 = 0x10006F00
	slotABase      uint32 = 0x10008000
	slotAFooter    uint32 = 0x1007FF00

	ldScriptSSBL    = "../link/ssbl.ld"
	ldScriptTSBL    = "../link/tsbl.ld"
	ldScriptSlotA   = "../link/app_at_0x10008000.ld"
	ldScriptSlotB   = "../link/app_at_0x10080000.ld"
	srcSSBL         = "../src/ssbl/ssbl.S"
	srcCRC32        = "../src/crc32.S"
	tsblSrcTemplate = "../src/tsbl/tsbl_%s.S"
)

// Slot capacities (mirror include/bootloader.inc). SSBL gets the first 4 KiB.
// TSBL gets 24 KiB ending at the footer at 0x10006F00 (so its payload caps
// at 24 KiB - 256 B; we report the full slot as capacity since the footer
// is the slot's own metadata). Slot A/B are 480 KiB each.
const (
	capSSBL  uint64 = 0x1000   // 4 KiB
	capTSBL  uint64 = 0x6000   // 24 KiB
	capSlot  uint64 = 0x78000  // 480 KiB
	slotBBase uint32 = 0x10080000
)

// buildBootloaderChain builds SSBL and the chosen TSBL flavor, computes
// footers, and stitches them with the app binary into firmware_<name>.uf2.
// Returns the final UF2 path and a BootloaderUsage breakdown. The app .bin
// is already produced by the caller; only the linker script is the caller's
// responsibility to pick.
func buildBootloaderChain(opts *Options, appBin []byte) (string, *BootloaderUsage, error) {
	bl := opts.Resolved.Project.Bootloader
	if bl == nil {
		return "", nil, fmt.Errorf("buildBootloaderChain called without [bootloader]")
	}

	stdout, stderr := stdouts(opts)

	ssblBytes, err := buildStage(opts, "ssbl",
		[]string{srcSSBL, srcCRC32},
		ldScriptSSBL,
		stdout, stderr)
	if err != nil {
		return "", nil, fmt.Errorf("ssbl: %w", err)
	}

	tsblSrc := fmt.Sprintf(tsblSrcTemplate, bl.TSBL)
	tsblBytes, err := buildStage(opts, "tsbl_"+bl.TSBL,
		[]string{tsblSrc, srcCRC32},
		ldScriptTSBL,
		stdout, stderr)
	if err != nil {
		return "", nil, fmt.Errorf("tsbl %s: %w", bl.TSBL, err)
	}

	tsblFooter := manifest.FooterData{Seq: 1, Status: manifest.StatusGood}
	tsblFooter.Compute(tsblBytes)

	// App is treated as slot A, seq=1, good. Slot B is intentionally left
	// blank in v1 — tsbl_ab will see only A valid and pick it; tsbl_bypass
	// ignores B regardless. Multi-slot installs need a second project or a
	// future studio feature (e.g. `make staged` style cli flag).
	appFooter := manifest.FooterData{Seq: 1, Status: manifest.StatusGood}
	appFooter.Compute(appBin)

	pieces := []firmware.Piece{
		{Name: "ssbl", LoadAddr: ssblBase, Data: ssblBytes},
		{Name: "tsbl_" + bl.TSBL, LoadAddr: tsblBase, Data: tsblBytes},
		{Name: "tsbl_footer", LoadAddr: tsblFooterAddr, Data: tsblFooter.Marshal()},
		{Name: "app", LoadAddr: slotABase, Data: appBin},
		{Name: "app_footer", LoadAddr: slotAFooter, Data: appFooter.Marshal()},
	}

	fwPath := filepath.Join(opts.OutDir, "firmware_"+opts.Resolved.Project.Name+".uf2")
	f, err := os.Create(fwPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	if err := firmware.Pack(f, pieces); err != nil {
		return "", nil, err
	}

	usage := &BootloaderUsage{
		Stages: []BootloaderStage{
			{Name: "SSBL", Base: ssblBase, Used: uint64(len(ssblBytes)), Capacity: capSSBL},
			{Name: "TSBL-" + bl.TSBL, Base: tsblBase, Used: uint64(len(tsblBytes)), Capacity: capTSBL},
			{Name: "Slot A", Base: slotABase, Used: uint64(len(appBin)), Capacity: capSlot},
			{Name: "Slot B", Base: slotBBase, Used: 0, Capacity: capSlot},
		},
	}
	return fwPath, usage, nil
}

// buildSlotOnlyUF2 packs just the app + its footer at the chosen slot's
// addresses, skipping SSBL/TSBL entirely. Used for A/B field updates that
// shouldn't disturb the bootloader. Slot A app uses seq=1; slot B uses
// seq=2 (so TSBL-ab's higher-seq-wins selector picks B over A by default —
// the freshly-pushed slot).
func buildSlotOnlyUF2(opts *Options, appBin []byte) (string, *BootloaderUsage, error) {
	var (
		base, footerAddr uint32
		seq              uint32
		stageName        string
		fileTag          string
	)
	switch opts.Slot {
	case "a":
		base, footerAddr = slotABase, slotAFooter
		seq, stageName, fileTag = 1, "Slot A", "slot_a"
	case "b":
		// Slot B footer = last 256 B of [0x10080000, 0x10080000+480 KiB).
		base, footerAddr = slotBBase, slotBBase+uint32(capSlot)-256
		seq, stageName, fileTag = 2, "Slot B", "slot_b"
	default:
		return "", nil, fmt.Errorf("buildSlotOnlyUF2: unknown slot %q", opts.Slot)
	}

	footer := manifest.FooterData{Seq: seq, Status: manifest.StatusGood}
	footer.Compute(appBin)

	pieces := []firmware.Piece{
		{Name: "app", LoadAddr: base, Data: appBin},
		{Name: "app_footer", LoadAddr: footerAddr, Data: footer.Marshal()},
	}

	fwPath := filepath.Join(opts.OutDir,
		fileTag+"_"+opts.Resolved.Project.Name+".uf2")
	f, err := os.Create(fwPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	if err := firmware.Pack(f, pieces); err != nil {
		return "", nil, err
	}

	usage := &BootloaderUsage{
		Stages: []BootloaderStage{
			{Name: stageName, Base: base, Used: uint64(len(appBin)), Capacity: capSlot},
		},
	}
	return fwPath, usage, nil
}

// buildStage assembles + links one bootloader stage (SSBL or TSBL), returning
// the .bin bytes. Object/ELF/bin/map files are written under OutDir with the
// given stage prefix so they don't collide with the app's outputs.
func buildStage(opts *Options, stageName string, sources []string, ldScript string, stdout, stderr io.Writer) ([]byte, error) {
	tgt := opts.Resolved.Target
	workDir := filepath.Dir(opts.Root)
	stageDir := filepath.Join(opts.OutDir, "_"+stageName)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return nil, err
	}

	asArgs := append([]string{}, tgt.AsFlags...)
	for _, inc := range tgt.AsIncludes {
		asArgs = append(asArgs, "-I", resolve(opts.Root, inc))
	}
	asArgs = append(asArgs, "-I", workDir)

	var objs []string
	for _, src := range sources {
		srcPath := resolve(opts.Root, src)
		objPath := filepath.Join(stageDir, objectName(src)+".o")
		args := append([]string{}, asArgs...)
		args = append(args, "-o", objPath, srcPath)
		if err := runCmd(opts.Toolchain.As, args, workDir, stdout, stderr, opts.Verbose); err != nil {
			return nil, fmt.Errorf("as %s: %w", src, err)
		}
		objs = append(objs, objPath)
	}

	elf := filepath.Join(stageDir, stageName+".elf")
	mapFile := filepath.Join(stageDir, stageName+".map")
	ldArgs := []string{"-T", resolve(opts.Root, ldScript)}
	ldArgs = append(ldArgs, tgt.LdFlags...)
	ldArgs = append(ldArgs, "-Map="+mapFile, "-o", elf)
	ldArgs = append(ldArgs, objs...)
	if err := runCmd(opts.Toolchain.Ld, ldArgs, workDir, stdout, stderr, opts.Verbose); err != nil {
		return nil, fmt.Errorf("ld: %w", err)
	}

	bin := filepath.Join(stageDir, stageName+".bin")
	if err := runCmd(opts.Toolchain.Objcopy, []string{"-O", "binary", elf, bin}, workDir, stdout, stderr, opts.Verbose); err != nil {
		return nil, fmt.Errorf("objcopy: %w", err)
	}
	return os.ReadFile(bin)
}

// stdouts returns the user-supplied stdout/stderr writers with the same
// defaults engine.Build uses. Factored out so bootloader.go can reuse them.
func stdouts(opts *Options) (io.Writer, io.Writer) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdout, stderr
}
