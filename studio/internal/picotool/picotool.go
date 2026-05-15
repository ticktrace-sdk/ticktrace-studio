// Package picotool detects and installs the Raspberry Pi `picotool` utility.
//
// picotool is the canonical way to load images onto an RP2350 over USB without
// physically pressing the BOOTSEL button each time. It's a CMake C++ project
// that depends on the pico-sdk for build helpers and on libusb-1.0 at runtime.
//
// Install steps (mirrored from picotool's BUILDING.md):
//  1. clone raspberrypi/pico-sdk (for the cmake helpers PICO_SDK_PATH points to)
//  2. clone raspberrypi/picotool
//  3. cmake configure
//  4. make
//  5. copy the resulting picotool binary to ~/.local/bin
//
// udev rule installation (60-picotool.rules) requires sudo and is left to the
// user — InstallUdevCommand returns the exact command to run.
package picotool

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	picoSdkRepo  = "https://github.com/raspberrypi/pico-sdk.git"
	picotoolRepo = "https://github.com/raspberrypi/picotool.git"
)

type Status struct {
	Installed   bool
	Path        string // absolute path if installed
	Version     string // e.g. "picotool v2.2.0-a4 (Linux, GNU-13.3.0, Release)"
	UdevRuleSrc string // path to bundled 60-picotool.rules if available
}

// Detect returns the current install state. Path is "" if not on PATH.
func Detect() Status {
	st := Status{}
	path, err := exec.LookPath("picotool")
	if err != nil {
		return st
	}
	st.Installed = true
	st.Path = path
	if out, err := exec.Command(path, "version").Output(); err == nil {
		st.Version = stripTrailingNewline(string(out))
	}
	// Look for the bundled udev rule from our most recent build, so the GUI
	// can surface the exact `sudo cp ...` command.
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "src", "picotool-build", "picotool", "udev", "60-picotool.rules")
		if _, err := os.Stat(candidate); err == nil {
			st.UdevRuleSrc = candidate
		}
	}
	return st
}

// Options controls the install flow. Workdir is where the source trees are
// cloned (default: ~/src/picotool-build). Logf, if set, receives one line per
// progress event ("cloning pico-sdk", "running cmake", ...).
type Options struct {
	Workdir   string
	InstallTo string // default: ~/.local/bin/picotool
	Logf      func(string)
	Stdout    io.Writer // raw subprocess output; nil = discard
	Stderr    io.Writer
}

// Install performs the full build-from-source flow. Long-running — caller
// should run this in a goroutine and stream Logf/Stdout/Stderr to a UI panel.
func Install(opts *Options) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	workdir := opts.Workdir
	if workdir == "" {
		workdir = filepath.Join(home, "src", "picotool-build")
	}
	installTo := opts.InstallTo
	if installTo == "" {
		installTo = filepath.Join(home, ".local", "bin", "picotool")
	}

	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return fmt.Errorf("mkdir workdir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(installTo), 0o755); err != nil {
		return fmt.Errorf("mkdir install dir: %w", err)
	}

	sdkDir := filepath.Join(workdir, "pico-sdk")
	toolDir := filepath.Join(workdir, "picotool")
	buildDir := filepath.Join(toolDir, "build")

	// Step 1+2: clone (skip if already present — supports re-runs).
	if err := cloneIfMissing(sdkDir, picoSdkRepo, logf, stdout, stderr); err != nil {
		return err
	}
	if err := cloneIfMissing(toolDir, picotoolRepo, logf, stdout, stderr); err != nil {
		return err
	}

	// Step 3: cmake.
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("mkdir build: %w", err)
	}
	logf("running cmake (this may take ~30s)")
	cmakeCmd := exec.Command("cmake", "..")
	cmakeCmd.Dir = buildDir
	cmakeCmd.Env = append(os.Environ(), "PICO_SDK_PATH="+sdkDir)
	cmakeCmd.Stdout = stdout
	cmakeCmd.Stderr = stderr
	if err := cmakeCmd.Run(); err != nil {
		return fmt.Errorf("cmake: %w", err)
	}

	// Step 4: make -j.
	jobs := runtime.NumCPU()
	logf(fmt.Sprintf("running make -j%d (this may take a minute)", jobs))
	makeCmd := exec.Command("make", fmt.Sprintf("-j%d", jobs))
	makeCmd.Dir = buildDir
	makeCmd.Stdout = stdout
	makeCmd.Stderr = stderr
	if err := makeCmd.Run(); err != nil {
		return fmt.Errorf("make: %w", err)
	}

	// Step 5: copy binary.
	src := filepath.Join(buildDir, "picotool")
	if err := copyExecutable(src, installTo); err != nil {
		return fmt.Errorf("install %s -> %s: %w", src, installTo, err)
	}
	logf("installed " + installTo)

	if rule := filepath.Join(toolDir, "udev", "60-picotool.rules"); fileExists(rule) {
		logf("udev rule available at " + rule)
		logf("for non-sudo USB access run: " + InstallUdevCommand(rule))
	}
	return nil
}

// Reboot runs `picotool reboot -f -u`, forcing a running board into BOOTSEL
// mode via USB. Useful when a board is running a USB-enabled image (CDC-ACM,
// etc.) and the user wants to reflash without physically pressing BOOTSEL.
//
// Returns nil if the board accepted the reboot. If picotool isn't installed
// or no responsive board is connected, the error from picotool is wrapped.
func Reboot(stdout, stderr io.Writer) error {
	bin, err := exec.LookPath("picotool")
	if err != nil {
		return fmt.Errorf("picotool not installed: %w", err)
	}
	cmd := exec.Command(bin, "reboot", "-f", "-u")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// InstallUdevCommand returns the exact shell command the user should run with
// sudo to install picotool's udev rules system-wide. The studio surfaces this
// in the UI rather than invoking sudo itself.
func InstallUdevCommand(rulePath string) string {
	return fmt.Sprintf(
		"sudo cp %s /etc/udev/rules.d/ && sudo udevadm control --reload && sudo udevadm trigger",
		rulePath,
	)
}

func cloneIfMissing(dir, url string, logf func(string), stdout, stderr io.Writer) error {
	if _, err := os.Stat(dir); err == nil {
		logf("already cloned: " + dir)
		return nil
	}
	logf("cloning " + url)
	cmd := exec.Command("git", "clone", "--depth", "1", url, dir)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func stripTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
