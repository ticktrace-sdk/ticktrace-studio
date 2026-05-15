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

package catalog

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

func Load(root string) (*Catalog, error) {
	cat := &Catalog{
		Modules: make(map[string]*Module),
		Targets: make(map[string]*Target),
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "targets"+string(filepath.Separator)) {
			t := &Target{}
			if _, err := toml.DecodeFile(path, t); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if t.Name == "" {
				return fmt.Errorf("%s: target missing name", path)
			}
			cat.Targets[t.Name] = t
			return nil
		}
		m := &Module{}
		if _, err := toml.DecodeFile(path, m); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if m.Symbol == "" {
			return fmt.Errorf("%s: module missing symbol", path)
		}
		m.Path = path
		if _, dup := cat.Modules[m.Symbol]; dup {
			return fmt.Errorf("%s: duplicate symbol %q", path, m.Symbol)
		}
		cat.Modules[m.Symbol] = m
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return cat, nil
}
