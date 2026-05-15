package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// pickFile opens a native OS file-selection dialog and returns the chosen
// absolute path. Cancelling returns "" with no error; missing-tool returns
// ("", err) so the caller can surface a useful message.
//
// startDir is a hint for where to anchor the dialog. titleArgs is a single
// dialog title.
func pickFile(title, startDir string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return pickLinux(title, startDir)
	case "darwin":
		return pickDarwin(title)
	case "windows":
		return pickWindows(title, startDir)
	}
	return "", fmt.Errorf("file picker not supported on %s", runtime.GOOS)
}

func pickLinux(title, startDir string) (string, error) {
	if path, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection", "--title=" + title}
		if startDir != "" {
			args = append(args, "--filename="+startDir+"/")
		}
		out, err := exec.Command(path, args...).Output()
		if err != nil {
			// Zenity exits non-zero when the user cancels. Treat that as
			// "no selection" rather than an error worth surfacing.
			if _, ok := err.(*exec.ExitError); ok {
				return "", nil
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	if path, err := exec.LookPath("kdialog"); err == nil {
		args := []string{"--getopenfilename"}
		if startDir != "" {
			args = append(args, startDir)
		}
		out, err := exec.Command(path, args...).Output()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				return "", nil
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", fmt.Errorf("install zenity or kdialog to use the file picker")
}

func pickDarwin(title string) (string, error) {
	script := fmt.Sprintf(`set f to choose file with prompt %q
POSIX path of f`, title)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pickWindows(title, startDir string) (string, error) {
	// PowerShell's OpenFileDialog. The escape gymnastics keep paths with
	// spaces from breaking the cmdline.
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms;
$d = New-Object System.Windows.Forms.OpenFileDialog;
$d.Title = %q;
$d.InitialDirectory = %q;
if ($d.ShowDialog() -eq 'OK') { Write-Output $d.FileName }`, title, startDir)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
