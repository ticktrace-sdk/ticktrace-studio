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
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// BoardState describes whether an RP2350 / RPI-RP2 board is reachable in
// BOOTSEL mode right now. Filled by scanning /proc/mounts for the canonical
// drive labels. picotool USB-only access isn't checked here; it would require
// either invoking picotool (subprocess cost) or libusb directly; the mount
// check is sufficient because picotool's own load path also uses USB and
// effectively requires the same board attachment state.
type BoardState struct {
	// InBootsel is true when at least one BOOTSEL-mounted drive was found.
	InBootsel bool
	// Mountpoint is the first matching drive's mountpoint (empty if none).
	Mountpoint string
}

// DetectBoard returns the current board state by scanning /proc/mounts for a
// vfat mount whose basename is "RPI-RP2" or "RP2350".
func DetectBoard() BoardState {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return BoardState{}
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 3 {
			continue
		}
		mp := fields[1]
		base := filepath.Base(mp)
		if base == "RPI-RP2" || base == "RP2350" {
			return BoardState{InBootsel: true, Mountpoint: mp}
		}
	}
	return BoardState{}
}
