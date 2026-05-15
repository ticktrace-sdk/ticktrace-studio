package rpasmboot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/amken3d/rp-asm/studio/internal/usbx"
)

// assertCmdHeader checks the 16-byte fixed header of a picoboot_cmd frame.
// argBytes is the expected bCmdSize value (not the args themselves).
func assertCmdHeader(t *testing.T, frame []byte, cmdID byte, argBytes byte, transferLen uint32) {
	t.Helper()
	if len(frame) != CmdSize {
		t.Fatalf("cmd frame size = %d, want %d", len(frame), CmdSize)
	}
	if got := binary.LittleEndian.Uint32(frame[0:4]); got != Magic {
		t.Errorf("magic = 0x%08x, want 0x%08x", got, Magic)
	}
	if frame[8] != cmdID {
		t.Errorf("cmdID = 0x%02x, want 0x%02x", frame[8], cmdID)
	}
	if frame[9] != argBytes {
		t.Errorf("bCmdSize = %d, want %d", frame[9], argBytes)
	}
	if got := binary.LittleEndian.Uint32(frame[12:16]); got != transferLen {
		t.Errorf("transferLength = %d, want %d", got, transferLen)
	}
}

func TestExclusiveAccessFrame(t *testing.T) {
	dev := usbx.NewMockDevice()
	// ACK on IN (cmd has no IN bit, no data phase).
	dev.QueueBulkIn(nil)

	c := NewClient(dev)
	if err := c.ExclusiveAccess(ExclusiveAndEject); err != nil {
		t.Fatalf("ExclusiveAccess: %v", err)
	}
	if len(dev.SentOut) != 1 {
		t.Fatalf("expected 1 OUT frame, got %d", len(dev.SentOut))
	}
	frame := dev.SentOut[0]
	assertCmdHeader(t, frame, CmdExclusiveAccess, 1, 0)
	if frame[16] != ExclusiveAndEject {
		t.Errorf("exclusive arg = %d, want %d", frame[16], ExclusiveAndEject)
	}
}

func TestExitXIPFrame(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil)
	c := NewClient(dev)
	if err := c.ExitXIP(); err != nil {
		t.Fatalf("ExitXIP: %v", err)
	}
	assertCmdHeader(t, dev.SentOut[0], CmdExitXIP, 0, 0)
}

func TestFlashEraseAlignment(t *testing.T) {
	dev := usbx.NewMockDevice()
	c := NewClient(dev)
	if err := c.FlashErase(0x10000001, FlashSectorSize); err == nil {
		t.Error("expected alignment error for unaligned addr")
	}
	if err := c.FlashErase(0x10000000, 100); err == nil {
		t.Error("expected alignment error for unaligned size")
	}
	if len(dev.SentOut) != 0 {
		t.Errorf("no frames should be sent on alignment failure, got %d", len(dev.SentOut))
	}
}

func TestFlashEraseFrame(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil)
	c := NewClient(dev)
	if err := c.FlashErase(0x10001000, 2*FlashSectorSize); err != nil {
		t.Fatalf("FlashErase: %v", err)
	}
	f := dev.SentOut[0]
	assertCmdHeader(t, f, CmdFlashErase, 8, 0)
	if got := binary.LittleEndian.Uint32(f[16:20]); got != 0x10001000 {
		t.Errorf("addr = 0x%x, want 0x10001000", got)
	}
	if got := binary.LittleEndian.Uint32(f[20:24]); got != 2*FlashSectorSize {
		t.Errorf("size = %d, want %d", got, 2*FlashSectorSize)
	}
}

func TestWriteFrame(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil) // for ACK
	c := NewClient(dev)
	page := make([]byte, FlashPageSize)
	for i := range page {
		page[i] = byte(i)
	}
	if err := c.Write(0x10000000, page); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(dev.SentOut) != 2 {
		t.Fatalf("expected cmd + data frames, got %d", len(dev.SentOut))
	}
	assertCmdHeader(t, dev.SentOut[0], CmdWrite, 8, FlashPageSize)
	if !bytes.Equal(dev.SentOut[1], page) {
		t.Errorf("data frame mismatch")
	}
}

func TestWriteUnalignedToSRAM(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil)
	c := NewClient(dev)
	// Unaligned SRAM write — protocol layer should accept; alignment is the
	// caller's responsibility based on the target region.
	if err := c.Write(0x20000003, []byte{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	assertCmdHeader(t, dev.SentOut[0], CmdWrite, 8, 3)
}

func TestReadFrame(t *testing.T) {
	dev := usbx.NewMockDevice()
	// IN cmd: device delivers data on IN, then we send zero-length OUT ack.
	want := bytes.Repeat([]byte{0xaa}, 128)
	dev.QueueBulkIn(want)

	c := NewClient(dev)
	got, err := c.Read(0x10000000, 128)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read data mismatch")
	}
	// Cmd has IN bit -> ACK is zero-length OUT, recorded as a second SentOut entry.
	if len(dev.SentOut) != 2 {
		t.Fatalf("expected cmd + ack-out, got %d", len(dev.SentOut))
	}
	assertCmdHeader(t, dev.SentOut[0], CmdRead, 8, 128)
	if len(dev.SentOut[1]) != 0 {
		t.Errorf("ack-out should be zero-length, got %d", len(dev.SentOut[1]))
	}
}

func TestReboot2Frame(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil)
	c := NewClient(dev)
	if err := c.Reboot2(Reboot2TypeBootsel|Reboot2NoReturnOnSuccess, 250, 0x11, 0x22); err != nil {
		t.Fatalf("Reboot2: %v", err)
	}
	f := dev.SentOut[0]
	assertCmdHeader(t, f, CmdReboot2, 16, 0)
	if got := binary.LittleEndian.Uint32(f[16:20]); got != Reboot2TypeBootsel|Reboot2NoReturnOnSuccess {
		t.Errorf("flags = 0x%x", got)
	}
	if got := binary.LittleEndian.Uint32(f[20:24]); got != 250 {
		t.Errorf("delayMs = %d", got)
	}
	if got := binary.LittleEndian.Uint32(f[24:28]); got != 0x11 {
		t.Errorf("p0 = 0x%x", got)
	}
	if got := binary.LittleEndian.Uint32(f[28:32]); got != 0x22 {
		t.Errorf("p1 = 0x%x", got)
	}
}

func TestParseStatus(t *testing.T) {
	raw := make([]byte, StatusSize)
	binary.LittleEndian.PutUint32(raw[0:4], 42)
	binary.LittleEndian.PutUint32(raw[4:8], uint32(StatusBadAlignment))
	raw[8] = CmdWrite
	raw[9] = 1
	s, err := ParseStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != 42 || s.Code != StatusBadAlignment || s.CmdID != CmdWrite || !s.InProgress {
		t.Errorf("parsed = %+v", s)
	}
	if got := s.Code.String(); got != "bad_alignment" {
		t.Errorf("status string = %q", got)
	}
}

func TestIfResetControlSetup(t *testing.T) {
	dev := usbx.NewMockDevice()
	c := NewClient(dev)
	if err := c.IfReset(); err != nil {
		t.Fatalf("IfReset: %v", err)
	}
	if len(dev.ControlOut) != 1 {
		t.Fatalf("expected 1 control OUT, got %d", len(dev.ControlOut))
	}
	got := dev.ControlOut[0].Setup
	if got.BRequest != usbx.IfRequestReset {
		t.Errorf("bRequest = 0x%02x", got.BRequest)
	}
	if got.BmRequestType != 0x41 {
		t.Errorf("bmRequestType = 0x%02x", got.BmRequestType)
	}
	if got.Dir != usbx.Out {
		t.Errorf("dir = %v", got.Dir)
	}
}

func TestCmdStatusControlIn(t *testing.T) {
	dev := usbx.NewMockDevice()
	raw := make([]byte, StatusSize)
	binary.LittleEndian.PutUint32(raw[0:4], 7)
	binary.LittleEndian.PutUint32(raw[4:8], uint32(StatusOK))
	dev.QueueControlIn(raw)

	c := NewClient(dev)
	s, err := c.CmdStatus()
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != 7 || s.Code != StatusOK {
		t.Errorf("status = %+v", s)
	}
}

func TestErrorWrapsCause(t *testing.T) {
	cause := errors.New("boom")
	err := &Error{Op: "x", CmdID: CmdWrite, Cause: cause}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is should find cause through Unwrap")
	}
}

func TestTokenIncrements(t *testing.T) {
	dev := usbx.NewMockDevice()
	dev.QueueBulkIn(nil)
	dev.QueueBulkIn(nil)
	c := NewClient(dev)
	_ = c.ExitXIP()
	_ = c.ExitXIP()
	t1 := binary.LittleEndian.Uint32(dev.SentOut[0][4:8])
	t2 := binary.LittleEndian.Uint32(dev.SentOut[1][4:8])
	if t2 != t1+1 {
		t.Errorf("token didn't increment: %d -> %d", t1, t2)
	}
}
