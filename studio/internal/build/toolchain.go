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

package build

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Toolchain struct {
	Prefix  string
	As      string
	Ld      string
	Objcopy string
	Size    string // optional; used for per-section memory breakdown
}

func Detect(prefix string) (*Toolchain, error) {
	t := &Toolchain{Prefix: prefix}
	missing := []string{}
	for _, bin := range []struct {
		dst  *string
		name string
	}{
		{&t.As, "as"},
		{&t.Ld, "ld"},
		{&t.Objcopy, "objcopy"},
	} {
		full := prefix + bin.name
		path, err := exec.LookPath(full)
		if err != nil {
			missing = append(missing, full)
			continue
		}
		*bin.dst = path
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("toolchain binaries not found on PATH: %s", strings.Join(missing, ", "))
	}
	// size is optional; old toolchains may lack it. Absence isn't a fatal
	// error; the engine just skips per-section breakdown.
	if path, err := exec.LookPath(prefix + "size"); err == nil {
		t.Size = path
	}
	return t, nil
}

func (t *Toolchain) Version(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "?"
	}
	first, _, _ := bytes.Cut(out, []byte("\n"))
	return string(first)
}
