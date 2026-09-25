//go:build !windows

package main

import "path/filepath"

// finalPath returns name with every symlink on the way followed.
func finalPath(name string) (string, error) {
	return filepath.EvalSymlinks(name)
}
