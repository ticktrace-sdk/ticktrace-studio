// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Copyright (C) 2026 Amken LLC <https://amken.io>
//
// This file is part of the Amken RP2350 Assembly SDK.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see
// <https://www.gnu.org/licenses/>.
//
// A commercial license is available from Amken LLC for use cases that
// cannot comply with the AGPL. See COMMERCIAL-LICENSE.md.

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
