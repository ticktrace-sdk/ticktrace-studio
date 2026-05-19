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

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/flash"
	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/rpasmboot"
	"github.com/ticktrace-sdk/ticktrace-studio/studio/internal/usbx"
)

const udevRulePath = "/etc/udev/rules.d/99-rpasmboot.rules"

const udevRuleContent = `# rpasmboot: Raspberry Pi RP2 BOOTSEL USB access for non-root users
SUBSYSTEM=="usb", ATTRS{idVendor}=="2e8a", MODE="0666", TAG+="uaccess"
`

// checkBoard reports udev state (Linux only) and lists any attached BOOTSEL
// devices. Returns non-nil when an actionable problem is present (e.g. udev
// rule missing) so callers can use it as an exit-code signal.
func checkBoard() error {
	fmt.Println("[board]")
	if runtime.GOOS == "linux" {
		if _, err := os.Stat(udevRulePath); err == nil {
			fmt.Printf("  udev:    %s present\n", udevRulePath)
		} else {
			fmt.Printf("  udev:    %s missing; non-root flashing will fail with EACCES\n", udevRulePath)
			fmt.Println("  to install, run:")
			fmt.Printf("    sudo tee %s > /dev/null <<'RULE'\n%sRULE\n", udevRulePath, udevRuleContent)
			fmt.Println("    sudo udevadm control --reload && sudo udevadm trigger")
		}
	}
	cands, err := usbx.Enumerate()
	if err != nil {
		fmt.Printf("  ERROR enumerating USB: %s\n", err)
		return err
	}
	if len(cands) == 0 {
		fmt.Println("  boards:  none in BOOTSEL right now (hold BOOTSEL while plugging in to enter)")
		return nil
	}
	for _, c := range cands {
		fmt.Printf("  board:   %s  vid:%04x pid:%04x serial:%s\n",
			c.Info.BusAddr, c.Info.Vendor, c.Info.Product, c.Info.Serial)
	}
	return nil
}

func cmdReboot(args []string) int {
	fs := flag.NewFlagSet("reboot", flag.ExitOnError)
	bootsel := fs.Bool("bootsel", false, "reboot into BOOTSEL instead of the application")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dev, err := usbx.Open(usbx.OpenOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer dev.Close()

	c := rpasmboot.NewClient(dev)
	if err := c.IfReset(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var rerr error
	if *bootsel {
		rerr = c.RebootToBootsel()
		fmt.Fprintln(os.Stderr, "rebooting into BOOTSEL")
	} else {
		rerr = c.RebootToApp()
		fmt.Fprintln(os.Stderr, "rebooting into app")
	}
	// REBOOT2 races device disappearance; treat transport errors as soft.
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "reboot returned %v (device likely already detached)\n", rerr)
	}
	return 0
}

func cmdInfo(args []string) int {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cands, err := usbx.Enumerate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(cands) == 0 {
		fmt.Fprintln(os.Stderr, "no RP2350 BOOTSEL devices found")
		return 1
	}
	for _, c := range cands {
		fmt.Printf("device:  %s\n", c.Info.BusAddr)
		fmt.Printf("  vid:    0x%04x\n", c.Info.Vendor)
		fmt.Printf("  pid:    0x%04x\n", c.Info.Product)
		fmt.Printf("  serial: %s\n", c.Info.Serial)
	}
	return 0
}

func cmdBootInfo(args []string) int {
	fs := flag.NewFlagSet("bootinfo", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	slots, err := flash.ReadBootInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, s := range slots {
		if !s.Valid {
			fmt.Printf("slot %s @ 0x%08x: (empty; footer has no RPBL magic)\n", s.Name, s.Base)
			continue
		}
		fmt.Printf("slot %s @ 0x%08x: status=%s seq=%d payload=%d B crc=0x%08x\n",
			s.Name, s.Base, flash.StatusName(s.Footer.Status), s.Footer.Seq, s.Footer.PayloadSize, s.Footer.CRC32)
	}
	return 0
}
