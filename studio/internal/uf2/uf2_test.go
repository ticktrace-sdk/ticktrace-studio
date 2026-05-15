package uf2

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"runtime"
	"testing"
)

// makeBlock builds one 512-byte UF2 block with the given fields.
func makeBlock(flags, addr uint32, blockNo, numBlocks, familyOrSize uint32, payload []byte) []byte {
	b := make([]byte, BlockSize)
	binary.LittleEndian.PutUint32(b[0:4], MagicStart0)
	binary.LittleEndian.PutUint32(b[4:8], MagicStart1)
	binary.LittleEndian.PutUint32(b[8:12], flags)
	binary.LittleEndian.PutUint32(b[12:16], addr)
	binary.LittleEndian.PutUint32(b[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(b[20:24], blockNo)
	binary.LittleEndian.PutUint32(b[24:28], numBlocks)
	binary.LittleEndian.PutUint32(b[28:32], familyOrSize)
	copy(b[32:], payload)
	binary.LittleEndian.PutUint32(b[508:512], MagicEnd)
	return b
}

func TestParseSingleBlock(t *testing.T) {
	payload := bytes.Repeat([]byte{0xab}, 256)
	raw := makeBlock(FlagFamilyIDPresent, 0x10000000, 0, 1, FamilyRP2350ArmSecure, payload)

	img, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(img.Blocks))
	}
	if len(img.Ranges) != 1 {
		t.Fatalf("ranges = %d", len(img.Ranges))
	}
	r := img.Ranges[0]
	if r.Addr != 0x10000000 || !bytes.Equal(r.Data, payload) {
		t.Errorf("range = {addr 0x%x, len %d}", r.Addr, len(r.Data))
	}
}

func TestMergeContiguousBlocks(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(makeBlock(0, 0x10000000, 0, 3, FamilyRP2350ArmSecure, bytes.Repeat([]byte{1}, 256)))
	buf.Write(makeBlock(0, 0x10000100, 1, 3, FamilyRP2350ArmSecure, bytes.Repeat([]byte{2}, 256)))
	buf.Write(makeBlock(0, 0x10000200, 2, 3, FamilyRP2350ArmSecure, bytes.Repeat([]byte{3}, 256)))

	img, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Ranges) != 1 {
		t.Fatalf("expected merged range, got %d ranges", len(img.Ranges))
	}
	if img.Ranges[0].Addr != 0x10000000 || len(img.Ranges[0].Data) != 768 {
		t.Errorf("merged range = {0x%x, %d}", img.Ranges[0].Addr, len(img.Ranges[0].Data))
	}
	if img.TotalBytes() != 768 {
		t.Errorf("TotalBytes = %d", img.TotalBytes())
	}
}

func TestNonContiguousProducesMultipleRanges(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(makeBlock(0, 0x10000000, 0, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{1}, 256)))
	buf.Write(makeBlock(0, 0x10010000, 1, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{2}, 256)))

	img, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Ranges) != 2 {
		t.Fatalf("ranges = %d", len(img.Ranges))
	}
}

func TestNotMainFlashSkipped(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(makeBlock(0, 0x10000000, 0, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{1}, 256)))
	buf.Write(makeBlock(FlagNotMainFlash, 0x20000000, 1, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{2}, 256)))

	img, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Blocks) != 2 {
		t.Errorf("expected both blocks parsed, got %d", len(img.Blocks))
	}
	if len(img.Ranges) != 1 {
		t.Errorf("FlagNotMainFlash should be filtered from ranges; got %d", len(img.Ranges))
	}
}

func TestSortsOutOfOrderBlocks(t *testing.T) {
	var buf bytes.Buffer
	// Reverse order — final range slice must still be sorted ascending.
	buf.Write(makeBlock(0, 0x10000200, 0, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{3}, 256)))
	buf.Write(makeBlock(0, 0x10000000, 1, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{1}, 256)))
	buf.Write(makeBlock(0, 0x10000100, 1, 2, FamilyRP2350ArmSecure, bytes.Repeat([]byte{2}, 256)))

	img, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Ranges) != 1 || img.Ranges[0].Addr != 0x10000000 {
		t.Errorf("expected one merged range starting at 0x10000000, got %+v", img.Ranges)
	}
}

func TestBadStartMagic(t *testing.T) {
	raw := makeBlock(0, 0x10000000, 0, 1, FamilyRP2350ArmSecure, []byte{0xaa})
	raw[0] = 0
	_, err := Parse(bytes.NewReader(raw))
	if err == nil {
		t.Error("expected error on bad magic")
	}
}

func TestTruncatedBlock(t *testing.T) {
	raw := makeBlock(0, 0x10000000, 0, 1, FamilyRP2350ArmSecure, []byte{0xaa})
	_, err := Parse(bytes.NewReader(raw[:300]))
	if err == nil {
		t.Error("expected error on truncated block")
	}
}

// TestParseRealUF2 smoke-tests against the in-tree blinky.uf2 produced by the
// rpasm build, to catch any drift between our parser and what the toolchain
// actually emits.
func TestParseRealUF2(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/uf2/uf2_test.go -> ../../build/blinky/blinky.uf2
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "build", "blinky", "blinky.uf2")
	img, err := ParseFile(path)
	if err != nil {
		t.Skipf("no real UF2 fixture available (%v) — skipping", err)
		return
	}
	if len(img.Blocks) == 0 {
		t.Fatal("real UF2 had zero blocks")
	}
	if len(img.Ranges) == 0 {
		t.Fatal("real UF2 had zero ranges after merging")
	}
	for _, r := range img.Ranges {
		if r.Addr < 0x10000000 || r.Addr >= 0x20000000 {
			// Not strictly an error — RAM images target SRAM — but flag it
			// for visibility during this v1 bring-up.
			t.Logf("range outside flash: addr=0x%08x len=%d", r.Addr, len(r.Data))
		}
	}
	t.Logf("blinky.uf2: %d blocks, %d ranges, %d bytes total", len(img.Blocks), len(img.Ranges), img.TotalBytes())
}
