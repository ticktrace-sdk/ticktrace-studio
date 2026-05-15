package build

import (
	"encoding/binary"
	"os"
)

const (
	uf2MagicStart0 = 0x0A324655
	uf2MagicStart1 = 0x9E5D5157
	uf2MagicEnd    = 0x0AB16F30
	uf2FlagFamily  = 0x00002000
	uf2Payload     = 256
	uf2BlockSize   = 512

	// Canonical UF2 family IDs from the microsoft/uf2 registry. Mirrored
	// from tools/internal/uf2 so studio can pick the right ID by load
	// address. The bootrom rejects SRAM-resident images unless they carry
	// the absolute family ID — fix landed in tools/uf2.py (917d6f6).
	uf2FamilyRP2350ArmS  uint32 = 0xE48BFF59
	uf2FamilyAbsolute    uint32 = 0xE48BFF57
)

// uf2FamilyFor picks the family ID from the load address. Caller's familyID
// argument is used as a fallback when the address is in the flash window
// (so a target TOML can still override the flash family for, e.g., RISC-V).
func uf2FamilyFor(addr, fallback uint32) uint32 {
	switch {
	case addr >= 0x20000000 && addr < 0x21000000:
		return uf2FamilyAbsolute
	case addr >= 0x10000000 && addr < 0x15000000:
		return fallback
	default:
		return fallback
	}
}

func PackUF2(binPath, uf2Path string, baseAddr, familyID uint32) error {
	familyID = uf2FamilyFor(baseAddr, familyID)
	data, err := os.ReadFile(binPath)
	if err != nil {
		return err
	}
	if pad := (uf2Payload - len(data)%uf2Payload) % uf2Payload; pad != 0 {
		data = append(data, make([]byte, pad)...)
	}
	nblocks := uint32(len(data) / uf2Payload)

	out, err := os.Create(uf2Path)
	if err != nil {
		return err
	}
	defer out.Close()

	block := make([]byte, uf2BlockSize)
	for i := range nblocks {
		clear(block)
		binary.LittleEndian.PutUint32(block[0:], uf2MagicStart0)
		binary.LittleEndian.PutUint32(block[4:], uf2MagicStart1)
		binary.LittleEndian.PutUint32(block[8:], uf2FlagFamily)
		binary.LittleEndian.PutUint32(block[12:], baseAddr+i*uf2Payload)
		binary.LittleEndian.PutUint32(block[16:], uf2Payload)
		binary.LittleEndian.PutUint32(block[20:], i)
		binary.LittleEndian.PutUint32(block[24:], nblocks)
		binary.LittleEndian.PutUint32(block[28:], familyID)
		copy(block[32:32+uf2Payload], data[i*uf2Payload:(i+1)*uf2Payload])
		binary.LittleEndian.PutUint32(block[uf2BlockSize-4:], uf2MagicEnd)
		if _, err := out.Write(block); err != nil {
			return err
		}
	}
	return nil
}
