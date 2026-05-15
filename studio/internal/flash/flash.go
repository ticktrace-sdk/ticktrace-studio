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

// Package flash copies a UF2 image to an RP2350 in BOOTSEL mode.
//
// Two strategies, tried in order:
//  1. rpasmboot — our in-tree PICOBOOT client (preferred; zero external deps).
//  2. Direct file copy to a BOOTSEL-mounted drive (RPI-RP2 / RP2350 label).
package flash

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Method string

const (
	MethodRpasmboot Method = "rpasmboot"
	MethodDrive     Method = "drive"
)

type Result struct {
	Method Method
	Target string // device path / mount point used
}

type Options struct {
	Uf2Path string
	Prefer  Method // "" = auto
	Log     func(string)
	// Stdout/Stderr are retained for the drive-copy path's diagnostic output;
	// rpasmboot logs via Log. The GUI passes writers that funnel lines into
	// the build-log pane.
	Stdout io.Writer
	Stderr io.Writer
}

// Flash performs the chosen strategy. Auto = rpasmboot first, drive fallback.
func Flash(opts *Options) (*Result, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string) {}
	}
	if _, err := os.Stat(opts.Uf2Path); err != nil {
		return nil, fmt.Errorf("uf2 not found: %w", err)
	}

	switch opts.Prefer {
	case MethodRpasmboot:
		return doRpasmboot(opts.Uf2Path, logf)
	case MethodDrive:
		return doDrive(opts.Uf2Path, logf)
	}

	// Auto: rpasmboot first, drive copy fallback.
	r, err := doRpasmboot(opts.Uf2Path, logf)
	if err == nil {
		return r, nil
	}
	logf("rpasmboot failed: " + err.Error())
	logf("falling back to drive copy")
	return doDrive(opts.Uf2Path, logf)
}

func doDrive(uf2 string, logf func(string)) (*Result, error) {
	mp, err := findBootselMount()
	if err != nil {
		return nil, err
	}
	logf("found BOOTSEL drive: " + mp)
	dst := filepath.Join(mp, filepath.Base(uf2))
	if err := copyFile(uf2, dst); err != nil {
		return nil, fmt.Errorf("copy %s -> %s: %w", uf2, dst, err)
	}
	logf("wrote " + dst)
	return &Result{Method: MethodDrive, Target: mp}, nil
}

// findBootselMount walks /proc/mounts for a vfat mount whose basename is
// "RPI-RP2" or "RP2350" (the labels the RP2040/RP2350 bootrom advertises in
// BOOTSEL mode; udisks2 mounts the label as the directory name).
func findBootselMount() (string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", err
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
			if _, err := os.Stat(mp); err == nil {
				return mp, nil
			}
		}
	}
	if err := scan.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no RPI-RP2 / RP2350 BOOTSEL drive mounted (hold BOOTSEL, plug in the board)")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
