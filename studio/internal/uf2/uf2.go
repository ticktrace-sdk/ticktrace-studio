// Package uf2 reads Microsoft UF2 images. Studio uses this to feed the
// rpasmboot flash orchestrator: parse the .uf2 produced by `rpasm build`,
// merge contiguous blocks into write ranges, hand off to the flasher.
//
// Format reference: https://github.com/microsoft/uf2
package uf2

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	BlockSize    = 512
	MagicStart0  = 0x0A324655 // "UF2\n"
	MagicStart1  = 0x9E5D5157
	MagicEnd     = 0x0AB16F30
	MaxPayload   = 476
)

// Flag bits.
const (
	FlagNotMainFlash      uint32 = 0x00000001
	FlagFileContainer     uint32 = 0x00001000
	FlagFamilyIDPresent   uint32 = 0x00002000
	FlagMD5Present        uint32 = 0x00004000
	FlagExtensionTags     uint32 = 0x00008000
)

// RP2350 family IDs (data/uf2.h in pico-sdk).
const (
	FamilyRP2040           uint32 = 0xe48bff56
	FamilyRP2350ArmSecure  uint32 = 0xe48bff59
	FamilyRP2350ArmNonSec  uint32 = 0xe48bff5a
	FamilyRP2350RISCV      uint32 = 0xe48bff5b
	FamilyDataAbsolute     uint32 = 0xe48bff5c
)

// Block is one parsed UF2 record. We keep only the fields studio needs.
type Block struct {
	Flags       uint32
	TargetAddr  uint32
	PayloadSize uint32
	BlockNo     uint32
	NumBlocks   uint32
	FamilyOrSize uint32
	Payload     []byte // sliced from the 512-byte block; copy if you need to keep it
}

// Range is a contiguous region after merging adjacent blocks. Addr is the
// start address; Data is the payload bytes laid out contiguously.
type Range struct {
	Addr uint32
	Data []byte
}

// Image is a parsed UF2 file.
type Image struct {
	Blocks []Block
	// Ranges is Blocks merged into contiguous chunks, sorted by Addr. Blocks
	// flagged NotMainFlash are excluded.
	Ranges []Range
}

// ParseFile reads and parses a UF2 file at path.
func ParseFile(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads UF2 blocks from r until EOF.
func Parse(r io.Reader) (*Image, error) {
	img := &Image{}
	buf := make([]byte, BlockSize)
	for blockIdx := 0; ; blockIdx++ {
		_, err := io.ReadFull(r, buf)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("uf2: truncated block %d", blockIdx)
		}
		if err != nil {
			return nil, err
		}
		blk, err := decodeBlock(buf)
		if err != nil {
			return nil, fmt.Errorf("uf2: block %d: %w", blockIdx, err)
		}
		img.Blocks = append(img.Blocks, blk)
	}
	img.Ranges = mergeRanges(img.Blocks)
	return img, nil
}

func decodeBlock(b []byte) (Block, error) {
	if len(b) != BlockSize {
		return Block{}, fmt.Errorf("block size %d != %d", len(b), BlockSize)
	}
	m0 := binary.LittleEndian.Uint32(b[0:4])
	m1 := binary.LittleEndian.Uint32(b[4:8])
	mEnd := binary.LittleEndian.Uint32(b[508:512])
	if m0 != MagicStart0 || m1 != MagicStart1 {
		return Block{}, fmt.Errorf("bad start magic 0x%08x 0x%08x", m0, m1)
	}
	if mEnd != MagicEnd {
		return Block{}, fmt.Errorf("bad end magic 0x%08x", mEnd)
	}
	payloadSize := binary.LittleEndian.Uint32(b[16:20])
	if payloadSize > MaxPayload {
		return Block{}, fmt.Errorf("payload size %d > %d", payloadSize, MaxPayload)
	}
	return Block{
		Flags:        binary.LittleEndian.Uint32(b[8:12]),
		TargetAddr:   binary.LittleEndian.Uint32(b[12:16]),
		PayloadSize:  payloadSize,
		BlockNo:      binary.LittleEndian.Uint32(b[20:24]),
		NumBlocks:    binary.LittleEndian.Uint32(b[24:28]),
		FamilyOrSize: binary.LittleEndian.Uint32(b[28:32]),
		Payload:      append([]byte(nil), b[32:32+payloadSize]...),
	}, nil
}

func mergeRanges(blocks []Block) []Range {
	// Filter + sort by addr.
	usable := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Flags&FlagNotMainFlash != 0 {
			continue
		}
		if b.PayloadSize == 0 {
			continue
		}
		usable = append(usable, b)
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].TargetAddr < usable[j].TargetAddr })

	var out []Range
	for _, b := range usable {
		if n := len(out); n > 0 {
			last := &out[n-1]
			end := last.Addr + uint32(len(last.Data))
			if b.TargetAddr == end {
				last.Data = append(last.Data, b.Payload...)
				continue
			}
			if b.TargetAddr < end {
				// Overlap — UF2 files shouldn't produce this, but tolerate
				// duplicate-address blocks by keeping the new payload.
				offset := int(end - b.TargetAddr)
				if offset < len(b.Payload) {
					last.Data = append(last.Data, b.Payload[offset:]...)
				}
				continue
			}
		}
		out = append(out, Range{Addr: b.TargetAddr, Data: append([]byte(nil), b.Payload...)})
	}
	return out
}

// TotalBytes returns the sum of all Range payload sizes — i.e. the count of
// bytes that will be written to flash.
func (img *Image) TotalBytes() uint32 {
	var n uint32
	for _, r := range img.Ranges {
		n += uint32(len(r.Data))
	}
	return n
}
