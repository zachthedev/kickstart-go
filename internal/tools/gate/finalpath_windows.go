//go:build windows

package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// finalPath returns the path Windows opens for name once every symlink,
// junction and mount point on the way is followed. filepath.EvalSymlinks
// leaves a junction in place, so the open handle answers instead.
func finalPath(name string) (string, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", name, err)
	}
	handle, err := windows.CreateFile(path, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", name, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	// Flags 0 asks for the normalized name with a drive letter:
	// FILE_NAME_NORMALIZED and VOLUME_NAME_DOS, which x/sys does not name.
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", name, err)
	}
	if int(n) >= len(buf) {
		return "", fmt.Errorf("resolving %s: the final path is longer than %d characters", name, len(buf))
	}
	final := windows.UTF16ToString(buf[:n])
	if share, ok := strings.CutPrefix(final, `\\?\UNC\`); ok {
		return `\\` + share, nil
	}
	return strings.TrimPrefix(final, `\\?\`), nil
}
