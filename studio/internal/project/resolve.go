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

package project

import (
	"fmt"
	"sort"

	"github.com/amken3d/rp-asm/studio/internal/catalog"
)

type Resolved struct {
	Project *Project
	Target  *catalog.Target
	Modules []*catalog.Module
	Sources []string
}

func Resolve(p *Project, cat *catalog.Catalog) (*Resolved, error) {
	tgt, ok := cat.Targets[p.Target]
	if !ok {
		return nil, fmt.Errorf("unknown target %q (known: %v)", p.Target, targetNames(cat))
	}

	enabled := map[string]bool{}
	for _, m := range cat.Modules {
		if m.Default {
			enabled[m.Symbol] = true
		}
	}
	for sym, on := range p.Features {
		if _, ok := cat.Modules[sym]; !ok {
			return nil, fmt.Errorf("project enables unknown symbol %q", sym)
		}
		enabled[sym] = on
	}

	var mods []*catalog.Module
	for sym, on := range enabled {
		if !on {
			continue
		}
		mods = append(mods, cat.Modules[sym])
	}
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].Order != mods[j].Order {
			return mods[i].Order < mods[j].Order
		}
		return mods[i].Symbol < mods[j].Symbol
	})

	for _, m := range mods {
		for _, req := range m.Requires {
			if !enabled[req] {
				return nil, fmt.Errorf("%s requires %s, but it is disabled", m.Symbol, req)
			}
		}
	}

	srcs := make([]string, 0, len(mods)*2+len(p.UserSource.Files))
	for _, m := range mods {
		srcs = append(srcs, m.Sources...)
	}
	srcs = append(srcs, p.UserSource.Files...)

	return &Resolved{
		Project: p,
		Target:  tgt,
		Modules: mods,
		Sources: srcs,
	}, nil
}

func targetNames(cat *catalog.Catalog) []string {
	out := make([]string, 0, len(cat.Targets))
	for n := range cat.Targets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
