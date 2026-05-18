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

// Package usbx is a thin OS USB transport for the RP2350 PICOBOOT interface.
// It exposes just enough surface for the rpasmboot package: bulk IN/OUT plus
// the two PICOBOOT control requests (IF_RESET, IF_CMD_STATUS).
//
// Concrete transports live in build-tagged files: usbfs_linux.go is the only
// one implemented in v1. macOS (IOKit) and Windows (WinUSB) are deferred.
package usbx

import (
	"errors"
	"time"
)

const (
	VendorRaspberryPi    uint16 = 0x2e8a
	ProductRP2350Bootsel uint16 = 0x000f
	ProductRP2040Bootsel uint16 = 0x0003 // reference only; v1 is RP2350-only
)

// PICOBOOT interface descriptor (RP2350 datasheet §5.4). The MSC interface
// on the same device uses class 0x08, so class==0xff is sufficient to
// identify PICOBOOT. We don't filter on bSubClass/bProtocol because real
// devices ship with sub=0, proto=0 (the SDK header's RESET_INTERFACE_PROTOCOL
// is for the application-level stdio reset interface, not BOOTSEL).
const PicobootClass uint8 = 0xff

// PICOBOOT control-request codes (bRequest on a class-targeted setup packet).
const (
	IfRequestReset     uint8 = 0x41 // host -> device, wLength 0
	IfRequestCmdStatus uint8 = 0x42 // device -> host, wLength 16
)

// Direction of a USB control transfer.
type Direction int

const (
	Out Direction = iota
	In
)

// ControlSetup is what the caller fills for a control transfer. The transport
// supplies wIndex (= interface number) and serializes the eight-byte setup
// packet itself.
type ControlSetup struct {
	BmRequestType uint8
	BRequest      uint8
	WValue        uint16
	Dir           Direction
}

type DeviceInfo struct {
	Vendor  uint16
	Product uint16
	Serial  string
	BusAddr string // e.g. "/dev/bus/usb/003/042" on Linux; opaque elsewhere
}

// Device is the transport contract rpasmboot consumes. Implementations are
// not safe for concurrent use; rpasmboot serializes all calls.
type Device interface {
	Info() DeviceInfo
	Control(setup ControlSetup, data []byte, timeout time.Duration) (int, error)
	BulkOut(data []byte, timeout time.Duration) (int, error)
	BulkIn(buf []byte, timeout time.Duration) (int, error)
	Close() error
}

type OpenOptions struct {
	// SerialFilter, if non-empty, restricts enumeration to a device whose
	// iSerialNumber contains this substring. Empty means "exactly one".
	SerialFilter string
}

var (
	ErrNoDevice        = errors.New("no RP2350 BOOTSEL device found")
	ErrMultipleDevices = errors.New("multiple BOOTSEL devices found; specify SerialFilter")
	ErrTimeout         = errors.New("usb transfer timed out")
	ErrStalled         = errors.New("usb endpoint stalled")
)
