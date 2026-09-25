//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// systemFolders reads the two folders mise needs on Windows from the system
// itself, never from an inherited variable: SystemRoot, which name resolution
// needs, and LOCALAPPDATA, which holds mise's installs.
func systemFolders() (map[string]string, error) {
	root, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return nil, fmt.Errorf("reading the Windows directory: %w", err)
	}
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return nil, fmt.Errorf("reading the LocalAppData known folder: %w", err)
	}
	return map[string]string{"SystemRoot": root, "LOCALAPPDATA": local}, nil
}
