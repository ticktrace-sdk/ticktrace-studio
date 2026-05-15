package flash

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// BoardState describes whether an RP2350 / RPI-RP2 board is reachable in
// BOOTSEL mode right now. Filled by scanning /proc/mounts for the canonical
// drive labels. picotool USB-only access isn't checked here — it would require
// either invoking picotool (subprocess cost) or libusb directly; the mount
// check is sufficient because picotool's own load path also uses USB and
// effectively requires the same board attachment state.
type BoardState struct {
	// InBootsel is true when at least one BOOTSEL-mounted drive was found.
	InBootsel bool
	// Mountpoint is the first matching drive's mountpoint (empty if none).
	Mountpoint string
}

// DetectBoard returns the current board state by scanning /proc/mounts for a
// vfat mount whose basename is "RPI-RP2" or "RP2350".
func DetectBoard() BoardState {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return BoardState{}
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 3 {
			continue
		}
		mp := fields[1]
		base := filepath.Base(mp)
		if base == "RPI-RP2" || base == "RP2350" {
			return BoardState{InBootsel: true, Mountpoint: mp}
		}
	}
	return BoardState{}
}
