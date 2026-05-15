// Package flash copies a UF2 image to an RP2350 in BOOTSEL mode.
//
// Two strategies, tried in order:
//  1. picotool load (preferred — works regardless of mount, can also reboot
//     a running device into BOOTSEL via USB).
//  2. Direct file copy to a BOOTSEL-mounted drive (RPI-RP2 / RP2350 label).
package flash

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Method string

const (
	MethodPicotool Method = "picotool"
	MethodDrive    Method = "drive"
)

type Result struct {
	Method Method
	Target string // device path / mount point used
}

type Options struct {
	Uf2Path string
	Prefer  Method // "" = auto
	Log     func(string)
	// Stdout/Stderr override where picotool's subprocess output goes. If nil,
	// falls back to os.Stdout / os.Stderr (CLI default). The GUI passes a
	// writer that funnels lines into the build-log pane.
	Stdout io.Writer
	Stderr io.Writer
}

// Flash performs the chosen strategy. Auto = picotool first, drive fallback.
func Flash(opts *Options) (*Result, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string) {}
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if _, err := os.Stat(opts.Uf2Path); err != nil {
		return nil, fmt.Errorf("uf2 not found: %w", err)
	}

	switch opts.Prefer {
	case MethodPicotool:
		return doPicotool(opts.Uf2Path, logf, stdout, stderr)
	case MethodDrive:
		return doDrive(opts.Uf2Path, logf)
	}

	if _, err := exec.LookPath("picotool"); err == nil {
		r, err := doPicotool(opts.Uf2Path, logf, stdout, stderr)
		if err == nil {
			return r, nil
		}
		logf("picotool failed: " + err.Error())
		logf("falling back to drive copy")
	} else {
		logf("picotool not on PATH; trying drive copy")
	}
	return doDrive(opts.Uf2Path, logf)
}

func doPicotool(uf2 string, logf func(string), stdout, stderr io.Writer) (*Result, error) {
	bin, err := exec.LookPath("picotool")
	if err != nil {
		return nil, fmt.Errorf("picotool not installed: %w", err)
	}
	args := []string{"load", "-u", "-v", "-x", uf2}
	logf(fmt.Sprintf("+ %s %s", bin, strings.Join(args, " ")))
	cmd := exec.Command(bin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &Result{Method: MethodPicotool, Target: bin}, nil
}

func doDrive(uf2 string, logf func(string)) (*Result, error) {
	mp, err := findBootselMount()
	if err != nil {
		return nil, err
	}
	logf("found BOOTSEL drive: " + mp)
	dst := filepath.Join(mp, filepath.Base(uf2))
	if err := copyFile(uf2, dst); err != nil {
		return nil, fmt.Errorf("copy %s -> %s: %w", uf2, dst, err)
	}
	logf("wrote " + dst)
	return &Result{Method: MethodDrive, Target: mp}, nil
}

// findBootselMount walks /proc/mounts for a vfat mount whose basename is
// "RPI-RP2" or "RP2350" (the labels the RP2040/RP2350 bootrom advertises in
// BOOTSEL mode; udisks2 mounts the label as the directory name).
func findBootselMount() (string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", err
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
			if _, err := os.Stat(mp); err == nil {
				return mp, nil
			}
		}
	}
	if err := scan.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no RPI-RP2 / RP2350 BOOTSEL drive mounted (hold BOOTSEL, plug in the board)")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
