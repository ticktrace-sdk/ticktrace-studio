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
	"path/filepath"
	"reflect"
	"testing"
)

// TestSaveLoadRoundTrip confirms a Project written by Save can be read back
// by Load with all fields intact, including the studio-only fields.
func TestSaveLoadRoundTrip(t *testing.T) {
	p := &Project{
		Name:         "demo",
		Target:       "rp2350-arm",
		Layout:       "flash",
		RpasmVersion: "1.0",
		StudioMode:   "examples",
		ExampleName:  "pio_blink_demo",
		Features:     map[string]bool{"PIO": true, "ADC": false},
		UserSource:   UserSource{Files: []string{"examples/pio_blink_demo.S"}},
	}
	path := filepath.Join(t.TempDir(), "test.rpasm.toml")
	if err := Save(p, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	q, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	q.Path = "" // not part of the on-disk schema
	expected := *p
	expected.Path = ""
	if !reflect.DeepEqual(expected, *q) {
		t.Errorf("round-trip mismatch:\n  saved: %+v\n  loaded: %+v", expected, *q)
	}
}
