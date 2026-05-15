package build_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amken3d/rp-asm/studio/internal/build"
	"github.com/amken3d/rp-asm/studio/internal/catalog"
	"github.com/amken3d/rp-asm/studio/internal/project"
)

// TestGoldenBlinky asserts that rpasm's blinky output is byte-identical to
// the Makefile's. Requires arm-none-eabi-as on PATH and that `make build/blinky.bin`
// has been run from the parent rp-asm directory beforehand (we run it).
func TestGoldenBlinky(t *testing.T) {
	if _, err := exec.LookPath("arm-none-eabi-as"); err != nil {
		t.Skip("arm-none-eabi-as not on PATH")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(root)
	makefile := filepath.Join(parent, "Makefile")
	if _, err := os.Stat(makefile); err != nil {
		t.Skipf("no Makefile at %s: %v", makefile, err)
	}

	mk := exec.Command("make", "-C", parent, "build/blinky.bin", "build/blinky.uf2")
	mk.Stderr = os.Stderr
	if err := mk.Run(); err != nil {
		t.Fatalf("make: %v", err)
	}
	wantBin := filepath.Join(parent, "build", "blinky.bin")
	wantUf2 := filepath.Join(parent, "build", "blinky.uf2")
	wantBinBytes, err := os.ReadFile(wantBin)
	if err != nil {
		t.Fatal(err)
	}
	wantUf2Bytes, err := os.ReadFile(wantUf2)
	if err != nil {
		t.Fatal(err)
	}

	cat, err := catalog.Load(filepath.Join(root, "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.Load(filepath.Join(root, "testdata", "blinky.rpasm.toml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := project.Resolve(proj, cat)
	if err != nil {
		t.Fatal(err)
	}
	tc, err := build.Detect(res.Target.ToolchainPrefix)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	result, err := build.Build(&build.Options{
		Resolved:  res,
		Root:      root,
		OutDir:    outDir,
		Toolchain: tc,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	gotBin, err := os.ReadFile(result.Bin)
	if err != nil {
		t.Fatal(err)
	}
	gotUf2, err := os.ReadFile(result.Uf2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(gotBin, wantBinBytes) {
		t.Errorf(".bin differs: got %d bytes, want %d bytes", len(gotBin), len(wantBinBytes))
	}
	if !bytes.Equal(gotUf2, wantUf2Bytes) {
		t.Errorf(".uf2 differs: got %d bytes, want %d bytes", len(gotUf2), len(wantUf2Bytes))
	}
}
