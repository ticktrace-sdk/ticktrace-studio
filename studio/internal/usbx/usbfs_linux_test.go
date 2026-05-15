//go:build linux

package usbx

import (
	"testing"
	"time"
)

// TestEnumerateNoCrash exercises the sysfs walk without expecting a board.
// On a CI host with no /sys/bus/usb at all, Enumerate returns an error; on a
// typical dev box it returns an empty (or one-element) slice. Either way the
// code path must not panic.
func TestEnumerateNoCrash(t *testing.T) {
	cands, err := Enumerate()
	if err != nil {
		t.Logf("Enumerate returned error (acceptable in sandboxed envs): %v", err)
		return
	}
	t.Logf("found %d BOOTSEL candidate(s)", len(cands))
	for _, c := range cands {
		t.Logf("  %s VID:%04x PID:%04x iface=%d in=0x%02x out=0x%02x",
			c.devNode, c.Info.Vendor, c.Info.Product, c.ifaceNum, c.inEP, c.outEP)
	}
}

func TestToMs(t *testing.T) {
	cases := []struct {
		in   int64 // ns
		want uint32
	}{
		{0, 0},
		{-1, 0},
		{1_000_000, 1},
		{3_000_000_000, 3000},
	}
	for _, c := range cases {
		if got := toMs(time.Duration(c.in)); got != c.want {
			t.Errorf("toMs(%d ns) = %d, want %d", c.in, got, c.want)
		}
	}
}
