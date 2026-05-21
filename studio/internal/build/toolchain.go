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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Toolchain struct {
	Prefix  string
	As      string
	Ld      string
	Objcopy string
	Size    string // optional; used for per-section memory breakdown
}

// MissingError is returned when one or more required toolchain binaries
// can't be found. The Studio UI uses errors.As to detect this case and
// offer a one-click install of the managed toolchain. Callers that don't
// care about the structured form can just print err and continue.
type MissingError struct {
	Prefix    string
	Searched  []string // PATH dirs and extra dirs that were searched
	Missing   []string // e.g. "arm-none-eabi-as", "arm-none-eabi-ld"
}

func (e *MissingError) Error() string {
	if len(e.Searched) == 0 {
		return fmt.Sprintf("toolchain binaries not found on PATH: %s",
			strings.Join(e.Missing, ", "))
	}
	return fmt.Sprintf("toolchain binaries not found (searched %d locations): %s",
		len(e.Searched), strings.Join(e.Missing, ", "))
}

// Detect resolves a toolchain by prefix using PATH. Kept for backwards
// compatibility with the CLI and tests; callers that want the full hybrid
// search (bundled paths, ~/.ticktrace/toolchain, Homebrew, etc.) should
// use the higher-level toolchain package, which calls DetectIn.
func Detect(prefix string) (*Toolchain, error) {
	return DetectIn(prefix, nil)
}

// DetectIn resolves a toolchain by prefix, looking in extraDirs first and
// then falling back to PATH. Returns *MissingError if any required binary
// (as/ld/objcopy) can't be found anywhere.
func DetectIn(prefix string, extraDirs []string) (*Toolchain, error) {
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
		if path, ok := lookIn(extraDirs, full); ok {
			*bin.dst = path
			continue
		}
		if path, err := exec.LookPath(full); err == nil {
			*bin.dst = path
			continue
		}
		missing = append(missing, full)
	}
	if len(missing) > 0 {
		return nil, &MissingError{
			Prefix:   prefix,
			Searched: extraDirs,
			Missing:  missing,
		}
	}
	// size is optional; old toolchains may lack it. Absence isn't a fatal
	// error; the engine just skips per-section breakdown.
	sizeName := prefix + "size"
	if path, ok := lookIn(extraDirs, sizeName); ok {
		t.Size = path
	} else if path, err := exec.LookPath(sizeName); err == nil {
		t.Size = path
	}
	return t, nil
}

// IsMissing is a convenience for callers that just want to know whether
// an error indicates a missing toolchain (vs. e.g. a permissions problem).
func IsMissing(err error) bool {
	var me *MissingError
	return errors.As(err, &me)
}

// lookIn searches extraDirs (in order) for an executable named bin. On
// Windows, .exe is appended automatically. Returns the full path and true
// if found.
func lookIn(extraDirs []string, bin string) (string, bool) {
	candidates := []string{bin}
	if runtime.GOOS == "windows" && !strings.HasSuffix(bin, ".exe") {
		candidates = []string{bin + ".exe", bin}
	}
	for _, dir := range extraDirs {
		if dir == "" {
			continue
		}
		for _, c := range candidates {
			full := filepath.Join(dir, c)
			if isExecutable(full) {
				return full, true
			}
		}
	}
	return "", false
}

func (t *Toolchain) Version(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "?"
	}
	first, _, _ := bytes.Cut(out, []byte("\n"))
	return string(first)
}

// isExecutable reports whether p is a regular file the current user can
// execute. On Windows, presence of the file is sufficient (perm bits are
// not the right test there).
func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode().Perm()&0o111 != 0
}
