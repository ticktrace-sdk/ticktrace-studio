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

package project

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

func Load(path string) (*Project, error) {
	p := &Project{}
	if _, err := toml.DecodeFile(path, p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("%s: project missing name", path)
	}
	if p.Target == "" {
		return nil, fmt.Errorf("%s: project missing target", path)
	}
	if p.Layout == "" {
		p.Layout = "flash"
	}
	if p.Layout != "flash" && p.Layout != "sram" {
		return nil, fmt.Errorf("%s: layout must be \"flash\" or \"sram\", got %q", path, p.Layout)
	}
	if p.Bootloader != nil {
		if p.Layout != "flash" {
			return nil, fmt.Errorf("%s: bootloader requires layout=\"flash\", got %q", path, p.Layout)
		}
		switch p.Bootloader.TSBL {
		case "bypass", "ab":
		default:
			return nil, fmt.Errorf("%s: bootloader.tsbl must be \"bypass\" or \"ab\", got %q", path, p.Bootloader.TSBL)
		}
	}
	if p.Features == nil {
		p.Features = make(map[string]bool)
	}
	p.Path = path
	return p, nil
}
