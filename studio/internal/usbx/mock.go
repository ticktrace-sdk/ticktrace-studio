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

package usbx

import (
	"fmt"
	"sync"
	"time"
)

// MockDevice is a Device implementation used by rpasmboot's unit tests. Wire
// it up by queueing the bytes the device should deliver on IN endpoints, then
// run the client code; sent OUT bytes are recorded for assertion.
//
// Not thread-safe; tests run single-goroutine.
type MockDevice struct {
	mu sync.Mutex

	info DeviceInfo

	// SentOut accumulates everything written to the bulk OUT endpoint, one
	// entry per BulkOut call. Tests usually inspect SentOut[0] etc.
	SentOut [][]byte

	// inQueue is consumed by BulkIn. Each entry is one transfer's worth.
	inQueue [][]byte

	// ctrlOutQueue collects payloads sent on control OUT transfers.
	ControlOut []controlRecord

	// ctrlInQueue is consumed by control IN transfers in FIFO order.
	ctrlInQueue [][]byte

	closed bool
}

type controlRecord struct {
	Setup ControlSetup
	Data  []byte
}

func NewMockDevice() *MockDevice {
	return &MockDevice{
		info: DeviceInfo{
			Vendor:  VendorRaspberryPi,
			Product: ProductRP2350Bootsel,
			Serial:  "MOCK",
		},
	}
}

func (m *MockDevice) Info() DeviceInfo { return m.info }

// QueueBulkIn enqueues bytes to be returned by the next BulkIn call.
func (m *MockDevice) QueueBulkIn(b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	m.inQueue = append(m.inQueue, cp)
}

// QueueControlIn enqueues bytes to be returned by the next control IN call.
func (m *MockDevice) QueueControlIn(b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	m.ctrlInQueue = append(m.ctrlInQueue, cp)
}

func (m *MockDevice) Control(setup ControlSetup, data []byte, _ time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, fmt.Errorf("usbx: control on closed mock")
	}
	if setup.Dir == Out {
		cp := make([]byte, len(data))
		copy(cp, data)
		m.ControlOut = append(m.ControlOut, controlRecord{Setup: setup, Data: cp})
		return len(data), nil
	}
	if len(m.ctrlInQueue) == 0 {
		return 0, fmt.Errorf("usbx: mock has no queued control IN data")
	}
	src := m.ctrlInQueue[0]
	m.ctrlInQueue = m.ctrlInQueue[1:]
	n := copy(data, src)
	return n, nil
}

func (m *MockDevice) BulkOut(data []byte, _ time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, fmt.Errorf("usbx: bulk-out on closed mock")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.SentOut = append(m.SentOut, cp)
	return len(data), nil
}

func (m *MockDevice) BulkIn(buf []byte, _ time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, fmt.Errorf("usbx: bulk-in on closed mock")
	}
	if len(m.inQueue) == 0 {
		return 0, fmt.Errorf("usbx: mock has no queued bulk-in data")
	}
	src := m.inQueue[0]
	m.inQueue = m.inQueue[1:]
	n := copy(buf, src)
	return n, nil
}

func (m *MockDevice) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
