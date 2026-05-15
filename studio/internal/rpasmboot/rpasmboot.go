// Package rpasmboot speaks the RP2350 PICOBOOT wire protocol over a usbx.Device.
// It replaces the picotool subprocess for studio's flash path. RP2350-only; the
// v1 command surface is just what's needed to load a UF2 and reboot:
// EXCLUSIVE_ACCESS, EXIT_XIP, FLASH_ERASE, WRITE, READ, REBOOT2, GET_INFO.
//
// Wire reference: pico-sdk/src/common/boot_picoboot_headers/include/boot/picoboot.h.
package rpasmboot

import (
	"encoding/binary"
	"fmt"
)

// Wire constants from picoboot.h.
const (
	Magic uint32 = 0x431fd10b

	CmdSize       = 32
	StatusSize    = 16
	InCmdBit byte = 0x80 // bCmdId top bit set => device-to-host data phase
)

// Command IDs. Constants prefixed Cmd*, top bit set for IN-direction commands.
const (
	CmdExclusiveAccess byte = 0x01
	CmdReboot          byte = 0x02 // legacy (RP2040); v1 uses Reboot2
	CmdFlashErase      byte = 0x03
	CmdRead            byte = 0x84
	CmdWrite           byte = 0x05
	CmdExitXIP         byte = 0x06
	CmdEnterCmdXIP     byte = 0x07
	CmdExec            byte = 0x08
	CmdVectorizeFlash  byte = 0x09
	CmdReboot2         byte = 0x0a
	CmdGetInfo         byte = 0x8b
	CmdOTPRead         byte = 0x8c
	CmdOTPWrite        byte = 0x0d
)

// Status codes from picoboot.h. Status returned by IF_CMD_STATUS control req.
type Status uint32

const (
	StatusOK                       Status = 0
	StatusUnknownCmd               Status = 1
	StatusInvalidCmdLength         Status = 2
	StatusInvalidTransferLength    Status = 3
	StatusInvalidAddress           Status = 4
	StatusBadAlignment             Status = 5
	StatusInterleavedWrite         Status = 6
	StatusRebooting                Status = 7
	StatusUnknownError             Status = 8
	StatusInvalidState             Status = 9
	StatusNotPermitted             Status = 10
	StatusInvalidArg               Status = 11
	StatusBufferTooSmall           Status = 12
	StatusPreconditionNotMet       Status = 13
	StatusModifiedData             Status = 14
	StatusInvalidData              Status = 15
	StatusNotFound                 Status = 16
	StatusUnsupportedModification  Status = 17
)

func (s Status) String() string {
	names := map[Status]string{
		StatusOK:                      "ok",
		StatusUnknownCmd:              "unknown_cmd",
		StatusInvalidCmdLength:        "invalid_cmd_length",
		StatusInvalidTransferLength:   "invalid_transfer_length",
		StatusInvalidAddress:          "invalid_address",
		StatusBadAlignment:            "bad_alignment",
		StatusInterleavedWrite:        "interleaved_write",
		StatusRebooting:               "rebooting",
		StatusUnknownError:            "unknown_error",
		StatusInvalidState:            "invalid_state",
		StatusNotPermitted:            "not_permitted",
		StatusInvalidArg:              "invalid_arg",
		StatusBufferTooSmall:          "buffer_too_small",
		StatusPreconditionNotMet:      "precondition_not_met",
		StatusModifiedData:            "modified_data",
		StatusInvalidData:             "invalid_data",
		StatusNotFound:                "not_found",
		StatusUnsupportedModification: "unsupported_modification",
	}
	if n, ok := names[s]; ok {
		return n
	}
	return fmt.Sprintf("status(%d)", uint32(s))
}

// Exclusive-access types (PC_EXCLUSIVE_ACCESS arg).
const (
	NotExclusive       byte = 0
	Exclusive          byte = 1
	ExclusiveAndEject  byte = 2
)

// REBOOT2 flag fields (picoboot_constants.h).
const (
	Reboot2TypeMask          uint32 = 0x0f
	Reboot2TypeNormal        uint32 = 0x0
	Reboot2TypeBootsel       uint32 = 0x2
	Reboot2TypeRAMImage      uint32 = 0x3
	Reboot2TypeFlashUpdate   uint32 = 0x4
	Reboot2TypePCSP          uint32 = 0xd
	Reboot2ToArm             uint32 = 0x10
	Reboot2ToRISCV           uint32 = 0x20
	Reboot2NoReturnOnSuccess uint32 = 0x100
)

// GET_INFO request types (picoboot_constants.h).
const (
	GetInfoSys              byte = 1
	GetInfoPartitionTable   byte = 2
	GetInfoUF2TargetPartition byte = 3
	GetInfoUF2Status        byte = 4
)

// RP2350 flash geometry. Erase granularity is the sector; write granularity
// the page. WRITE accepts page-aligned ranges; FLASH_ERASE accepts sector-
// aligned ranges.
const (
	FlashBase      uint32 = 0x10000000
	FlashSectorSize uint32 = 4096
	FlashPageSize   uint32 = 256
)

// CmdStatus is the parsed response from an IF_CMD_STATUS control transfer.
type CmdStatus struct {
	Token      uint32
	Code       Status
	CmdID      byte
	InProgress bool
}

// ParseStatus deserializes a 16-byte picoboot_cmd_status reply.
func ParseStatus(b []byte) (CmdStatus, error) {
	if len(b) < StatusSize {
		return CmdStatus{}, fmt.Errorf("rpasmboot: status reply too short: %d", len(b))
	}
	return CmdStatus{
		Token:      binary.LittleEndian.Uint32(b[0:4]),
		Code:       Status(binary.LittleEndian.Uint32(b[4:8])),
		CmdID:      b[8],
		InProgress: b[9] != 0,
	}, nil
}

// encodeCmd builds a 32-byte picoboot_cmd. argSize is bCmdSize (bytes of args
// actually used; the rest is padding). args may be shorter than 16 bytes;
// remaining argument bytes are zeroed.
func encodeCmd(token uint32, cmdID byte, transferLength uint32, argSize byte, args []byte) []byte {
	if len(args) > 16 {
		panic(fmt.Sprintf("rpasmboot: args too long: %d", len(args)))
	}
	if int(argSize) > len(args) {
		panic(fmt.Sprintf("rpasmboot: argSize %d > args len %d", argSize, len(args)))
	}
	buf := make([]byte, CmdSize)
	binary.LittleEndian.PutUint32(buf[0:4], Magic)
	binary.LittleEndian.PutUint32(buf[4:8], token)
	buf[8] = cmdID
	buf[9] = argSize
	// buf[10:12] reserved
	binary.LittleEndian.PutUint32(buf[12:16], transferLength)
	copy(buf[16:32], args)
	return buf
}

// Error reports a failed PICOBOOT command. If Status was retrieved, it's
// non-OK; if the failure was at the USB layer before status could be read,
// Status is StatusOK and Cause carries the transport error.
type Error struct {
	Op     string
	CmdID  byte
	Status Status
	Cause  error
}

func (e *Error) Error() string {
	if e.Status != StatusOK {
		return fmt.Sprintf("rpasmboot %s (cmd 0x%02x): %s", e.Op, e.CmdID, e.Status)
	}
	if e.Cause != nil {
		return fmt.Sprintf("rpasmboot %s (cmd 0x%02x): %s", e.Op, e.CmdID, e.Cause)
	}
	return fmt.Sprintf("rpasmboot %s (cmd 0x%02x): unknown failure", e.Op, e.CmdID)
}

func (e *Error) Unwrap() error { return e.Cause }
