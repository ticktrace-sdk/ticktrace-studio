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
	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/usbx"
	"github.com/ticktrace-sdk/rp-asm/tools/manifest"
)

// Slot addresses (mirror studio/internal/build/bootloader.go and
// include/bootloader.inc). The footer occupies the last 256 B of each
// 480 KiB slot.
const (
	slotABase   uint32 = 0x10008000
	slotAFooter uint32 = 0x1007FF00
	slotBBase   uint32 = 0x10080000
	slotBFooter uint32 = 0x100F7F00
)

// SlotInfo is the parsed state of one app slot, as recovered from its footer
// over PICOBOOT on a board in BOOTSEL mode.
type SlotInfo struct {
	Name       string // "A" or "B"
	Base       uint32 // 0x10008000 / 0x10080000
	FooterAddr uint32
	Valid      bool                // footer parsed cleanly
	Footer     manifest.FooterData // zero value if !Valid
}

// ReadBootInfo opens the first BOOTSEL device, exits XIP, reads both slot
// footers, and parses them. Returns slot info for both A and B even when
// one (or both) is empty/invalid; caller inspects Valid + Footer.Status.
func ReadBootInfo() ([]SlotInfo, error) {
	dev, err := usbx.Open(usbx.OpenOptions{})
	if err != nil {
		return nil, err
	}
	defer dev.Close()

	c := rpasmboot.NewClient(dev)
	if err := c.IfReset(); err != nil {
		return nil, fmt.Errorf("if_reset: %w", err)
	}
	if err := c.ExclusiveAccess(rpasmboot.Exclusive); err != nil {
		return nil, fmt.Errorf("exclusive_access: %w", err)
	}
	// EXIT_XIP is required before reading flash via PICOBOOT; otherwise the
	// QSPI is in execute-in-place mode and the bootrom returns junk.
	if err := c.ExitXIP(); err != nil {
		return nil, fmt.Errorf("exit_xip: %w", err)
	}

	slots := []SlotInfo{
		{Name: "A", Base: slotABase, FooterAddr: slotAFooter},
		{Name: "B", Base: slotBBase, FooterAddr: slotBFooter},
	}
	for i := range slots {
		data, err := c.Read(slots[i].FooterAddr, manifest.Footer)
		if err != nil {
			return slots, fmt.Errorf("read slot %s footer: %w", slots[i].Name, err)
		}
		f, err := manifest.Unmarshal(data)
		if err != nil {
			// Erased flash or unrecognised content; leave Valid=false.
			continue
		}
		slots[i].Valid = true
		slots[i].Footer = f
	}
	return slots, nil
}

// StatusName returns a human label for a footer status code.
func StatusName(s manifest.Status) string {
	switch s {
	case manifest.StatusEmpty:
		return "empty"
	case manifest.StatusStaged:
		return "staged"
	case manifest.StatusTrying:
		return "trying"
	case manifest.StatusGood:
		return "good"
	case manifest.StatusBad:
		return "bad"
	default:
		return fmt.Sprintf("0x%08x", uint32(s))
	}
}
