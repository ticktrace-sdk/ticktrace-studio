// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Copyright (C) 2026 Amken LLC <https://www.amken.us>
//
// This file is part of the ticktrace Assembly SDK.
// Licensed under AGPL-3.0-or-later; commercial license available.
// See LICENSE and COMMERCIAL-LICENSE.md in the root of this repository.

//go:build !linux

package usbx

import "errors"

// Stubs for platforms where we haven't wired up a native USB transport
// yet (macOS via IOKit, Windows via WinUSB). The package still compiles
// so the rest of Studio can build cross-platform; PICOBOOT flashing
// errors at run time, and callers fall back to the drag-and-drop
// "drive" method which works everywhere.
//
// ErrUnsupportedPlatform is what Open/Enumerate return here so callers
// can distinguish "no device found" (ErrNoDevice) from "this OS has no
// USB transport yet."

// Candidate mirrors the public shape of the Linux Candidate so callers
// like cmd/rpasm/board.go that touch c.Info compile uniformly. The
// platform-private fields the Linux build uses are simply absent.
type Candidate struct {
	Info DeviceInfo
}

// ErrUnsupportedPlatform is returned by Open and Enumerate on any host
// where a native USB transport hasn't been implemented yet.
var ErrUnsupportedPlatform = errors.New("USB BOOTSEL flashing isn't implemented on this platform yet; flash with --method drive instead")

// Enumerate returns no devices and the unsupported-platform error so
// callers can decide to fall back to drive flashing rather than error
// out. The empty slice rather than nil keeps `range cands` safe.
func Enumerate() ([]Candidate, error) {
	return []Candidate{}, ErrUnsupportedPlatform
}

// Open is the equivalent of Linux's usbfs Open: it accepts the same
// options shape so call sites are identical, and errors out with
// ErrUnsupportedPlatform.
func Open(opts OpenOptions) (Device, error) {
	_ = opts
	return nil, ErrUnsupportedPlatform
}
