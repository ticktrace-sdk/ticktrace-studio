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

package catalog

type Module struct {
	Symbol      string   `toml:"symbol"`
	Name        string   `toml:"name"`
	Category    string   `toml:"category"`
	Order       int      `toml:"order"`
	Default     bool     `toml:"default"`
	Description string   `toml:"description"`
	Sources     []string `toml:"sources"`
	Includes    []string `toml:"includes"`
	Requires    []string `toml:"requires"`

	Path string `toml:"-"`
}

type Target struct {
	Name            string   `toml:"name"`
	Arch            string   `toml:"arch"`
	ToolchainPrefix string   `toml:"toolchain_prefix"`
	AsFlags         []string `toml:"as_flags"`
	AsIncludes      []string `toml:"as_includes"`
	LdFlags         []string `toml:"ld_flags"`
	LdScriptFlash   string   `toml:"ld_script_flash"`
	LdScriptSram    string   `toml:"ld_script_sram"`
	FlashLoadAddr   uint32   `toml:"flash_load_addr"`
	SramLoadAddr    uint32   `toml:"sram_load_addr"`
	Uf2FamilyID     uint32   `toml:"uf2_family_id"`
}

type Catalog struct {
	Modules map[string]*Module
	Targets map[string]*Target
}