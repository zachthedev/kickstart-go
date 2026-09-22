package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_Usage(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{name: "no subcommand", args: nil, wantIn: "a subcommand is required"},
		{name: "unknown subcommand", args: []string{"nope"}, wantIn: `unknown subcommand "nope"`},
		{name: "canary without paths", args: []string{"canary"}, wantIn: "the actionlint and shellcheck paths are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			got := run(tt.args, &stderr)
			assert.Equal(t, 2, got)
			assert.Contains(t, stderr.String(), tt.wantIn)
		})
	}
}

// TestRun_Pins drives the dispatch through the pins subcommand, which reads
// the two files from the working directory.
func TestRun_Pins(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	t.Run("missing files are an error", func(t *testing.T) {
		var stderr bytes.Buffer
		assert.Equal(t, 2, run([]string{"pins"}, &stderr))
		assert.Contains(t, stderr.String(), "gate pins:")
	})

	require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(intactPins), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), []byte(intactLock()), 0o600))
	t.Run("an intact pair holds", func(t *testing.T) {
		var stderr bytes.Buffer
		assert.Equal(t, 0, run([]string{"pins"}, &stderr))
		assert.Empty(t, stderr.String())
	})

	tampered := []byte(intactLock()[:len(intactLock())-1])
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), bytes.ReplaceAll(tampered, []byte("github-attestations"), []byte("none")), 0o600))
	t.Run("a finding exits 1 and prints it", func(t *testing.T) {
		var stderr bytes.Buffer
		assert.Equal(t, 1, run([]string{"pins"}, &stderr))
		assert.Contains(t, stderr.String(), "attests its releases")
	})
}
