//go:build !windows

package main

// systemFolders has nothing to add outside Windows: mise finds its
// directories under HOME there.
func systemFolders() (map[string]string, error) {
	return nil, nil
}
