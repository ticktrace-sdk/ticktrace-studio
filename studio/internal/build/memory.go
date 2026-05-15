package build

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MemoryUsage is the per-region and per-section breakdown for a built ELF.
// Regions come from `ld --print-memory-usage`; sections from `<prefix>size -A`.
// Either field may be empty if the tool was unavailable or the output didn't
// parse — callers should treat MemoryUsage as best-effort, not authoritative.
type MemoryUsage struct {
	Regions  []MemoryRegion
	Sections []MemorySection
}

type MemoryRegion struct {
	Name string // FLASH, SRAM, BOOTROM, ...
	Used uint64 // bytes
	Size uint64 // bytes
}

func (r MemoryRegion) Percent() float64 {
	if r.Size == 0 {
		return 0
	}
	return 100.0 * float64(r.Used) / float64(r.Size)
}

type MemorySection struct {
	Name string
	Size uint64
	Addr uint64
}

// BootloaderUsage breaks the firmware UF2 down by stage. Only populated when
// the project has [bootloader] set. Capacities are layout-defined (mirrored
// from include/bootloader.inc); Used is the actual payload size (excludes
// the 256-byte footer for stages that have one).
type BootloaderUsage struct {
	Stages []BootloaderStage
}

type BootloaderStage struct {
	Name     string // "SSBL" | "TSBL-ab" | "Slot A" | "Slot B"
	Base     uint32 // load address
	Used     uint64 // bytes of payload
	Capacity uint64 // bytes the slot can hold (incl. footer for app slots)
}

func (s BootloaderStage) Percent() float64 {
	if s.Capacity == 0 {
		return 0
	}
	return 100.0 * float64(s.Used) / float64(s.Capacity)
}

// ld --print-memory-usage format:
//
//	Memory region         Used Size  Region Size  %age Used
//	           FLASH:        9004 B         2 MB      0.43%
//	            SRAM:        1372 B       512 KB      0.26%
//
// We ignore the percent column and recompute it from used/size; that way the
// caller doesn't have to deal with ld's two-decimal rounding.
var memRegionRE = regexp.MustCompile(
	`^\s*([A-Za-z0-9_]+):\s+(\d+)\s*([KMG]?)B\s+(\d+)\s*([KMG]?)B\s+([\d.]+%)?`,
)

func parseMemoryRegions(s string) []MemoryRegion {
	var out []MemoryRegion
	for _, line := range strings.Split(s, "\n") {
		m := memRegionRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		used, err1 := scaleBytes(m[2], m[3])
		size, err2 := scaleBytes(m[4], m[5])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, MemoryRegion{Name: m[1], Used: used, Size: size})
	}
	return out
}

func scaleBytes(n, unit string) (uint64, error) {
	v, err := strconv.ParseUint(n, 10, 64)
	if err != nil {
		return 0, err
	}
	switch unit {
	case "K":
		v *= 1024
	case "M":
		v *= 1024 * 1024
	case "G":
		v *= 1024 * 1024 * 1024
	}
	return v, nil
}

// `<prefix>size -A` output (Berkeley/SysV mixed; the -A form is SysV):
//
//	build/blinky.elf  :
//	section              size        addr
//	.text                1592   268435456
//	.bss                  272   536870912
//	...
//	Total                1864
//
// We skip the header, the totals line, and any zero-sized section.
var sizeSectionRE = regexp.MustCompile(`^(\.\S+)\s+(\d+)\s+(\d+)\s*$`)

func parseSizeSections(s string) []MemorySection {
	var out []MemorySection
	for _, line := range strings.Split(s, "\n") {
		m := sizeSectionRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sz, err1 := strconv.ParseUint(m[2], 10, 64)
		addr, err2 := strconv.ParseUint(m[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		if sz == 0 {
			continue
		}
		out = append(out, MemorySection{Name: m[1], Size: sz, Addr: addr})
	}
	return out
}

// runSize invokes `<prefix>size -A <elf>` and returns parsed sections. Returns
// nil (no error) if size isn't available or fails — the build artifacts still
// exist regardless.
func runSize(sizeBin, elf, workDir string) []MemorySection {
	if sizeBin == "" {
		return nil
	}
	cmd := exec.Command(sizeBin, "-A", elf)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseSizeSections(string(out))
}
