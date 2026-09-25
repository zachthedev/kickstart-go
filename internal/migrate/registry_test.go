package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExample_IsInitialized(t *testing.T) {
	require.NotNil(t, Example, "Example registry is nil")
	assert.GreaterOrEqual(t, Example.CurrentVersion, 1, "Example.CurrentVersion")
}

// TestExample_CurrentVersion enforces the sequential-version convention:
// initial schema is version 1, each registered migration adds 1. Catches
// forgotten CurrentVersion bumps when a new migration is added.
func TestExample_CurrentVersion(t *testing.T) {
	want := len(Example.Migrations) + 1
	assert.Equal(t, want, Example.CurrentVersion, "Example.CurrentVersion")
}
