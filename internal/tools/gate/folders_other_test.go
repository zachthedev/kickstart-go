//go:build !windows

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemFolders(t *testing.T) {
	got, err := systemFolders()
	require.NoError(t, err)
	assert.Empty(t, got)
}
