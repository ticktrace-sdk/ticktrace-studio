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

type Project struct {
	Name         string          `toml:"name"`
	Target       string          `toml:"target"`
	Layout       string          `toml:"layout"`
	RpasmVersion string          `toml:"rpasm_version"`
	Features     map[string]bool `toml:"features"`
	UserSource   UserSource      `toml:"user_source"`
	Bootloader   *Bootloader     `toml:"bootloader,omitempty"`

	// Studio-only fields (CLI ignores them). Persisted so the GUI can restore
	// which mode/example was active without losing user intent.
	StudioMode  string `toml:"studio_mode,omitempty"`  // "examples" | "custom"
	ExampleName string `toml:"example_name,omitempty"` // name of selected example (Examples mode)

	Path string `toml:"-"`
}

type UserSource struct {
	Files []string `toml:"files"`
}

// Bootloader, when non-nil, makes Build produce a complete chain UF2
// (firmware_<name>.uf2) containing SSBL + TSBL + app + footers. The app is
// linked at the slot-A base (0x10008000) instead of the bare-metal flash
// origin. TSBL flavor selects what kind of boot policy the chain ships with.
type Bootloader struct {
	TSBL string `toml:"tsbl"` // "bypass" | "ab"
}