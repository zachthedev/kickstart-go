//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFinalPath follows a directory symlink to its target.
func TestFinalPath(t *testing.T) {
	target, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	file := filepath.Join(target, "tool")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(target, alias))

	got, err := finalPath(filepath.Join(alias, "tool"))
	require.NoError(t, err)
	assert.Equal(t, file, got)

	_, err = finalPath(filepath.Join(alias, "absent"))
	assert.Error(t, err)
}
