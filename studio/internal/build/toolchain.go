package build

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Toolchain struct {
	Prefix  string
	As      string
	Ld      string
	Objcopy string
	Size    string // optional — used for per-section memory breakdown
}

func Detect(prefix string) (*Toolchain, error) {
	t := &Toolchain{Prefix: prefix}
	missing := []string{}
	for _, bin := range []struct {
		dst  *string
		name string
	}{
		{&t.As, "as"},
		{&t.Ld, "ld"},
		{&t.Objcopy, "objcopy"},
	} {
		full := prefix + bin.name
		path, err := exec.LookPath(full)
		if err != nil {
			missing = append(missing, full)
			continue
		}
		*bin.dst = path
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("toolchain binaries not found on PATH: %s", strings.Join(missing, ", "))
	}
	// size is optional — old toolchains may lack it. Absence isn't a fatal
	// error; the engine just skips per-section breakdown.
	if path, err := exec.LookPath(prefix + "size"); err == nil {
		t.Size = path
	}
	return t, nil
}

func (t *Toolchain) Version(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "?"
	}
	first, _, _ := bytes.Cut(out, []byte("\n"))
	return string(first)
}
