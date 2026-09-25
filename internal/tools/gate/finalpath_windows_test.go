//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFinalPath follows a directory symlink to its target. A portable test
// cannot make a junction or an 8.3 name without spawning, so the symlink
// stands in for them, as TestResolveProgram's link cases do.
func TestFinalPath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	require.NoError(t, os.Mkdir(target, 0o700))
	file := filepath.Join(target, "tool.exe")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}

	got, err := finalPath(filepath.Join(alias, "tool.exe"))
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(got, `\\?\`), "the long-path prefix is stripped: %s", got)
	assert.NotContains(t, got, "alias")
	assert.True(t, strings.EqualFold("target", filepath.Base(filepath.Dir(got))), "the final path runs through the target: %s", got)
	gotInfo, err := os.Stat(got)
	require.NoError(t, err)
	fileInfo, err := os.Stat(file)
	require.NoError(t, err)
	assert.True(t, os.SameFile(gotInfo, fileInfo))

	_, err = finalPath(filepath.Join(alias, "absent.exe"))
	assert.ErrorContains(t, err, "opening")
}
