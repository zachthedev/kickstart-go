package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault_IsInitialized(t *testing.T) {
	require.NotNil(t, Default, "Default registry is nil")
	assert.NotNil(t, Default.Entries(), "Default registry cannot list its entries")
}
