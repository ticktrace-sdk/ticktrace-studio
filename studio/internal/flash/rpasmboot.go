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

package flash

import (
	"fmt"

	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/rpasmboot"
	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/uf2"
	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/usbx"
)

// RP2350 memory regions.
const (
	flashBase = 0x10000000
	flashEnd  = 0x11000000 // 16 MiB XIP window
	sramBase  = 0x20000000
	sramEnd   = 0x20082000 // 520 KiB across SRAM0..9
)

// chunkSize is the write granularity we hand to PICOBOOT. 4 KiB matches one
// flash sector and is small enough to keep timeouts comfortable.
const chunkSize = 4096

// doRpasmboot loads a UF2 over PICOBOOT using our in-tree rpasmboot client.
// Supports three patterns: pure XIP (all flash ranges), SRAM-only (all SRAM
// ranges, boots via REBOOT2 RAM_IMAGE), and mixed (flash + SRAM ranges in one
// UF2, common when SRAM holds pre-loaded data and code XIPs from flash).
func doRpasmboot(uf2Path string, logf func(string)) (*Result, error) {
	img, err := uf2.ParseFile(uf2Path)
	if err != nil {
		return nil, fmt.Errorf("parse uf2: %w", err)
	}
	if len(img.Ranges) == 0 {
		return nil, fmt.Errorf("uf2 has no data ranges")
	}

	var flashRanges, sramRanges []uf2.Range
	for _, r := range img.Ranges {
		switch {
		case isFlashAddr(r.Addr) && isFlashAddr(r.Addr+uint32(len(r.Data))-1):
			flashRanges = append(flashRanges, r)
		case isSRAMAddr(r.Addr) && isSRAMAddr(r.Addr+uint32(len(r.Data))-1):
			sramRanges = append(sramRanges, r)
		default:
			return nil, fmt.Errorf("range at 0x%08x +%d is outside flash and SRAM regions", r.Addr, len(r.Data))
		}
	}

	dev, err := usbx.Open(usbx.OpenOptions{})
	if err != nil {
		return nil, err
	}
	defer dev.Close()
	logf(fmt.Sprintf("opened %s (serial=%s)", dev.Info().BusAddr, dev.Info().Serial))

	c := rpasmboot.NewClient(dev)
	if err := c.IfReset(); err != nil {
		return nil, err
	}
	if err := c.ExclusiveAccess(rpasmboot.ExclusiveAndEject); err != nil {
		return nil, fmt.Errorf("exclusive access: %w", err)
	}
	if len(flashRanges) > 0 {
		if err := c.ExitXIP(); err != nil {
			return nil, fmt.Errorf("exit xip: %w", err)
		}
	}

	for _, r := range flashRanges {
		if r.Addr%rpasmboot.FlashPageSize != 0 {
			return nil, fmt.Errorf("flash range start 0x%08x not page-aligned; v1 requires page-aligned UF2 ranges", r.Addr)
		}
		eraseAddr, eraseSize := alignFlashErase(r.Addr, uint32(len(r.Data)))
		logf(fmt.Sprintf("flash erase 0x%08x +%d", eraseAddr, eraseSize))
		if err := c.FlashErase(eraseAddr, eraseSize); err != nil {
			return nil, err
		}
		data := padToPage(r.Data)
		logf(fmt.Sprintf("flash write 0x%08x +%d", r.Addr, len(data)))
		for off := 0; off < len(data); off += chunkSize {
			end := off + chunkSize
			if end > len(data) {
				end = len(data)
			}
			if err := c.Write(r.Addr+uint32(off), data[off:end]); err != nil {
				return nil, err
			}
		}
	}

	for _, r := range sramRanges {
		logf(fmt.Sprintf("sram write 0x%08x +%d", r.Addr, len(r.Data)))
		for off := 0; off < len(r.Data); off += chunkSize {
			end := off + chunkSize
			if end > len(r.Data) {
				end = len(r.Data)
			}
			if err := c.Write(r.Addr+uint32(off), r.Data[off:end]); err != nil {
				return nil, err
			}
		}
	}

	if err := doReboot(c, flashRanges, sramRanges, logf); err != nil {
		// REBOOT2 races device disappearance; transport errors on the ACK are
		// expected when the device resets mid-transfer.
		logf(fmt.Sprintf("reboot returned %v (device likely already detached, ignoring)", err))
	}
	return &Result{Method: MethodRpasmboot, Target: dev.Info().BusAddr}, nil
}

// doReboot picks the appropriate REBOOT2 mode for the image just loaded:
//   - any flash content present  -> Normal (boot via flash boot path)
//   - SRAM only                  -> RAM_IMAGE (bootrom locates vector table
//                                   inside the given [base, size) window)
func doReboot(c *rpasmboot.Client, flashRanges, sramRanges []uf2.Range, logf func(string)) error {
	if len(flashRanges) > 0 {
		logf("reboot normal")
		return c.Reboot2(rpasmboot.Reboot2TypeNormal|rpasmboot.Reboot2ToArm, 100, 0, 0)
	}
	base, size := sramSpan(sramRanges)
	logf(fmt.Sprintf("reboot ram_image base=0x%08x size=%d", base, size))
	return c.Reboot2(rpasmboot.Reboot2TypeRAMImage|rpasmboot.Reboot2ToArm, 100, base, size)
}

func sramSpan(ranges []uf2.Range) (uint32, uint32) {
	if len(ranges) == 0 {
		return 0, 0
	}
	lo := ranges[0].Addr
	hi := ranges[0].Addr + uint32(len(ranges[0].Data))
	for _, r := range ranges[1:] {
		if r.Addr < lo {
			lo = r.Addr
		}
		if e := r.Addr + uint32(len(r.Data)); e > hi {
			hi = e
		}
	}
	return lo, hi - lo
}

func isFlashAddr(a uint32) bool { return a >= flashBase && a < flashEnd }
func isSRAMAddr(a uint32) bool  { return a >= sramBase && a < sramEnd }

// alignFlashErase widens [addr, addr+size) outward to sector boundaries.
func alignFlashErase(addr, size uint32) (uint32, uint32) {
	const sec = rpasmboot.FlashSectorSize
	start := addr - (addr % sec)
	end := addr + size
	if end%sec != 0 {
		end += sec - (end % sec)
	}
	return start, end - start
}

// padToPage rounds the data length up to the next page boundary, padding the
// tail with 0xff (matching erased-flash content).
func padToPage(data []byte) []byte {
	const pg = rpasmboot.FlashPageSize
	rem := uint32(len(data)) % pg
	if rem == 0 {
		return data
	}
	out := make([]byte, uint32(len(data))+(pg-rem))
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = 0xff
	}
	return out
}
