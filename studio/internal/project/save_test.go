package project

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestSaveLoadRoundTrip confirms a Project written by Save can be read back
// by Load with all fields intact, including the studio-only fields.
func TestSaveLoadRoundTrip(t *testing.T) {
	p := &Project{
		Name:         "demo",
		Target:       "rp2350-arm",
		Layout:       "flash",
		RpasmVersion: "1.0",
		StudioMode:   "examples",
		ExampleName:  "pio_blink_demo",
		Features:     map[string]bool{"PIO": true, "ADC": false},
		UserSource:   UserSource{Files: []string{"examples/pio_blink_demo.S"}},
	}
	path := filepath.Join(t.TempDir(), "test.rpasm.toml")
	if err := Save(p, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	q, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	q.Path = "" // not part of the on-disk schema
	expected := *p
	expected.Path = ""
	if !reflect.DeepEqual(expected, *q) {
		t.Errorf("round-trip mismatch:\n  saved: %+v\n  loaded: %+v", expected, *q)
	}
}
