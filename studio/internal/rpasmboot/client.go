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

package rpasmboot

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/amken3d/rp-asm/studio/internal/usbx"
)

// Default transfer timeouts. Picotool uses 3s for the cmd send, 10s for data
// phases; we mirror those, with a separate (longer) erase timeout because
// large flash erases can be slow.
var (
	DefaultCmdTimeout   = 3 * time.Second
	DefaultDataTimeout  = 10 * time.Second
	DefaultEraseTimeout = 60 * time.Second
)

// Client issues PICOBOOT commands over a usbx.Device. Not safe for concurrent
// use; callers must serialize.
type Client struct {
	dev   usbx.Device
	token atomic.Uint32

	CmdTimeout   time.Duration
	DataTimeout  time.Duration
	EraseTimeout time.Duration

	// InterfaceNumber is the PICOBOOT interface index reported by the device.
	// It's only needed if the transport doesn't already bind to it.
	InterfaceNumber uint8
}

func NewClient(dev usbx.Device) *Client {
	c := &Client{
		dev:          dev,
		CmdTimeout:   DefaultCmdTimeout,
		DataTimeout:  DefaultDataTimeout,
		EraseTimeout: DefaultEraseTimeout,
	}
	c.token.Store(1)
	return c
}

func (c *Client) nextToken() uint32 { return c.token.Add(1) - 1 }

// transact runs one PICOBOOT round-trip: cmd OUT, optional data phase, ACK on
// opposite endpoint. dataOut is non-nil for OUT-direction cmds; dataIn is the
// buffer the IN-direction reads into. Caller sets transferLength to match.
func (c *Client) transact(op string, cmdID byte, argSize byte, args []byte, transferLength uint32, dataOut []byte, dataIn []byte, dataTimeout time.Duration) error {
	tok := c.nextToken()
	cmd := encodeCmd(tok, cmdID, transferLength, argSize, args)

	if _, err := c.dev.BulkOut(cmd, c.CmdTimeout); err != nil {
		return &Error{Op: op, CmdID: cmdID, Cause: fmt.Errorf("send cmd: %w", err)}
	}

	isIn := cmdID&InCmdBit != 0

	if transferLength != 0 {
		if isIn {
			if uint32(len(dataIn)) < transferLength {
				return &Error{Op: op, CmdID: cmdID, Cause: fmt.Errorf("dataIn buffer %d < transferLength %d", len(dataIn), transferLength)}
			}
			n, err := c.dev.BulkIn(dataIn[:transferLength], dataTimeout)
			if err != nil {
				return c.wrapWithStatus(op, cmdID, fmt.Errorf("recv data: %w", err))
			}
			if uint32(n) != transferLength {
				return c.wrapWithStatus(op, cmdID, fmt.Errorf("short read: got %d/%d", n, transferLength))
			}
		} else {
			if uint32(len(dataOut)) < transferLength {
				return &Error{Op: op, CmdID: cmdID, Cause: fmt.Errorf("dataOut %d < transferLength %d", len(dataOut), transferLength)}
			}
			n, err := c.dev.BulkOut(dataOut[:transferLength], dataTimeout)
			if err != nil {
				return c.wrapWithStatus(op, cmdID, fmt.Errorf("send data: %w", err))
			}
			if uint32(n) != transferLength {
				return c.wrapWithStatus(op, cmdID, fmt.Errorf("short write: sent %d/%d", n, transferLength))
			}
		}
	}

	// ACK is a zero-length transfer on the *opposite* direction endpoint.
	// Picotool requests 1 byte and tolerates 0; we do the same for compat.
	var spoon [1]byte
	if isIn {
		_, err := c.dev.BulkOut(spoon[:0], c.CmdTimeout)
		if err != nil {
			return c.wrapWithStatus(op, cmdID, fmt.Errorf("ack out: %w", err))
		}
	} else {
		_, err := c.dev.BulkIn(spoon[:], c.CmdTimeout)
		if err != nil {
			return c.wrapWithStatus(op, cmdID, fmt.Errorf("ack in: %w", err))
		}
	}
	return nil
}

// wrapWithStatus attempts to fetch IF_CMD_STATUS and attach its code to the
// error. If the status fetch itself fails, the original cause is preserved.
func (c *Client) wrapWithStatus(op string, cmdID byte, cause error) error {
	st, statusErr := c.CmdStatus()
	if statusErr != nil || st.Code == StatusOK {
		return &Error{Op: op, CmdID: cmdID, Cause: cause}
	}
	return &Error{Op: op, CmdID: cmdID, Status: st.Code, Cause: cause}
}

// CmdStatus reads the device's IF_CMD_STATUS reply for the most recent command.
func (c *Client) CmdStatus() (CmdStatus, error) {
	buf := make([]byte, StatusSize)
	setup := usbx.ControlSetup{
		BmRequestType: 0xc1, // dir=IN, type=vendor, recipient=interface
		BRequest:      usbx.IfRequestCmdStatus,
		WValue:        0,
		Dir:           usbx.In,
	}
	n, err := c.dev.Control(setup, buf, c.CmdTimeout)
	if err != nil {
		return CmdStatus{}, fmt.Errorf("rpasmboot: IF_CMD_STATUS: %w", err)
	}
	if n < StatusSize {
		return CmdStatus{}, fmt.Errorf("rpasmboot: IF_CMD_STATUS short read: %d", n)
	}
	return ParseStatus(buf)
}

// IfReset issues the IF_RESET control request, which clears any stale stalled
// state from a prior session (e.g. an interrupted picotool run). Recommended
// as the first call after opening the device.
func (c *Client) IfReset() error {
	setup := usbx.ControlSetup{
		BmRequestType: 0x41, // dir=OUT, type=vendor, recipient=interface
		BRequest:      usbx.IfRequestReset,
		WValue:        0,
		Dir:           usbx.Out,
	}
	_, err := c.dev.Control(setup, nil, c.CmdTimeout)
	if err != nil {
		return fmt.Errorf("rpasmboot: IF_RESET: %w", err)
	}
	return nil
}

// ExclusiveAccess takes (or releases) exclusive control of the device.
// mode is NotExclusive / Exclusive / ExclusiveAndEject (the last ejects the
// MSC drive so the host filesystem stops touching it).
func (c *Client) ExclusiveAccess(mode byte) error {
	args := []byte{mode}
	return c.transact("ExclusiveAccess", CmdExclusiveAccess, 1, args, 0, nil, nil, c.DataTimeout)
}

// ExitXIP: must be called before touching flash, so the QSPI peripheral
// leaves execute-in-place mode and accepts erase/write commands.
func (c *Client) ExitXIP() error {
	return c.transact("ExitXIP", CmdExitXIP, 0, nil, 0, nil, nil, c.DataTimeout)
}

// FlashErase erases [addr, addr+size). Both must be sector-aligned
// (FlashSectorSize = 4096).
func (c *Client) FlashErase(addr, size uint32) error {
	if size == 0 {
		return nil
	}
	if addr%FlashSectorSize != 0 || size%FlashSectorSize != 0 {
		return fmt.Errorf("rpasmboot: FlashErase: addr=0x%x size=%d not sector-aligned", addr, size)
	}
	args := make([]byte, 8)
	binary.LittleEndian.PutUint32(args[0:4], addr)
	binary.LittleEndian.PutUint32(args[4:8], size)
	return c.transact("FlashErase", CmdFlashErase, 8, args, 0, nil, nil, c.EraseTimeout)
}

// Write sends data to addr in the device's address space. Works for both
// flash (page-aligned, sectors previously erased) and SRAM (no alignment
// constraint). The protocol layer doesn't enforce alignment; that's the
// caller's responsibility because flash and SRAM rules differ.
func (c *Client) Write(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	args := make([]byte, 8)
	binary.LittleEndian.PutUint32(args[0:4], addr)
	binary.LittleEndian.PutUint32(args[4:8], uint32(len(data)))
	return c.transact("Write", CmdWrite, 8, args, uint32(len(data)), data, nil, c.DataTimeout)
}

// Read transfers size bytes from the device's address space at addr. Works
// for both RAM and flash. No alignment constraint at the protocol level,
// but flash reads need EXIT_XIP first or the bootrom may return junk.
func (c *Client) Read(addr, size uint32) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	args := make([]byte, 8)
	binary.LittleEndian.PutUint32(args[0:4], addr)
	binary.LittleEndian.PutUint32(args[4:8], size)
	buf := make([]byte, size)
	if err := c.transact("Read", CmdRead, 8, args, size, nil, buf, c.DataTimeout); err != nil {
		return nil, err
	}
	return buf, nil
}

// Reboot2 issues the RP2350 reboot command. Common shortcuts:
//   RebootToApp()      = Reboot2(Reboot2TypeNormal, 0, 0, 0)
//   RebootToBootsel()  = Reboot2(Reboot2TypeBootsel, 0, 0, 0)
func (c *Client) Reboot2(flags, delayMs, p0, p1 uint32) error {
	args := make([]byte, 16)
	binary.LittleEndian.PutUint32(args[0:4], flags)
	binary.LittleEndian.PutUint32(args[4:8], delayMs)
	binary.LittleEndian.PutUint32(args[8:12], p0)
	binary.LittleEndian.PutUint32(args[12:16], p1)
	return c.transact("Reboot2", CmdReboot2, 16, args, 0, nil, nil, c.DataTimeout)
}

// RebootToApp reboots the chip into its normal application boot path. The
// USB device will disappear shortly after this call returns.
func (c *Client) RebootToApp() error {
	return c.Reboot2(Reboot2TypeNormal, 0, 0, 0)
}

// RebootToBootsel reboots the chip back into BOOTSEL. Useful for chaining
// flash operations.
func (c *Client) RebootToBootsel() error {
	return c.Reboot2(Reboot2TypeBootsel, 0, 0, 0)
}
