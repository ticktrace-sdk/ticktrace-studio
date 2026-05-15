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

//go:build linux

package usbx

import (
	"testing"
	"time"
)

// TestEnumerateNoCrash exercises the sysfs walk without expecting a board.
// On a CI host with no /sys/bus/usb at all, Enumerate returns an error; on a
// typical dev box it returns an empty (or one-element) slice. Either way the
// code path must not panic.
func TestEnumerateNoCrash(t *testing.T) {
	cands, err := Enumerate()
	if err != nil {
		t.Logf("Enumerate returned error (acceptable in sandboxed envs): %v", err)
		return
	}
	t.Logf("found %d BOOTSEL candidate(s)", len(cands))
	for _, c := range cands {
		t.Logf("  %s VID:%04x PID:%04x iface=%d in=0x%02x out=0x%02x",
			c.devNode, c.Info.Vendor, c.Info.Product, c.ifaceNum, c.inEP, c.outEP)
	}
}

func TestToMs(t *testing.T) {
	cases := []struct {
		in   int64 // ns
		want uint32
	}{
		{0, 0},
		{-1, 0},
		{1_000_000, 1},
		{3_000_000_000, 3000},
	}
	for _, c := range cases {
		if got := toMs(time.Duration(c.in)); got != c.want {
			t.Errorf("toMs(%d ns) = %d, want %d", c.in, got, c.want)
		}
	}
}
