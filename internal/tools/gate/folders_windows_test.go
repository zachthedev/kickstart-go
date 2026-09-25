//go:build windows

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemFolders reads the folders from the system with the inherited
// variables pointing elsewhere, so a planted value cannot stand in for them.
func TestSystemFolders(t *testing.T) {
	t.Setenv("SystemRoot", `C:\planted-root`)
	t.Setenv("LOCALAPPDATA", `C:\planted-local`)
	got, err := systemFolders()
	require.NoError(t, err)
	require.Len(t, got, 2)
	for name, value := range got {
		assert.True(t, filepath.IsAbs(value), "%s is %q", name, value)
		assert.NotContains(t, value, "planted", "%s came from the environment", name)
	}
	assert.Contains(t, got, "SystemRoot")
	assert.Contains(t, got, "LOCALAPPDATA")
}
