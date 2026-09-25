package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each case adds one directive to a go.mod, and the check must refuse
// replace, godebug and ignore in either form, and pass everything else.
func TestGoModFindings(t *testing.T) {
	const base = "module example.com/probe\n\ngo 1.27.0\n\nrequire github.com/pelletier/go-toml/v2 v2.4.3\n"
	tests := []struct {
		name   string
		extra  string
		wantIn []string
	}{
		{name: "no directive the gate refuses", extra: "\ntool golang.org/x/tools/cmd/deadcode\n"},
		{name: "a replace into the checkout", extra: "\nreplace github.com/pelletier/go-toml/v2 => ./rt-toml\n", wantIn: []string{
			"go.mod replaces github.com/pelletier/go-toml/v2 with ./rt-toml, which builds that module, a dependency of the gate's own build among them, from there",
		}},
		{name: "a replace block to another module", extra: "\nreplace (\n\tgithub.com/pelletier/go-toml/v2 v2.4.3 => example.com/fork/toml v2.4.3\n)\n", wantIn: []string{
			"replaces github.com/pelletier/go-toml/v2 with example.com/fork/toml",
		}},
		{name: "a godebug line", extra: "\ngodebug execerrdot=0\n", wantIn: []string{"go.mod sets godebug execerrdot=0"}},
		{name: "a godebug block", extra: "\ngodebug (\n\twinsymlink=0\n\tpanicnil=1\n)\n", wantIn: []string{"godebug winsymlink=0", "godebug panicnil=1"}},
		{name: "an ignore line", extra: "\nignore ./internal/hidden\n", wantIn: []string{"go.mod ignores ./internal/hidden, which every ./... row then skips"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, goModule), []byte(base+tt.extra), 0o600))
			found, err := goModFindings(dir)
			require.NoError(t, err)
			require.Len(t, found, len(tt.wantIn), "findings: %q", found)
			for i, want := range tt.wantIn {
				assert.Contains(t, found[i], want)
			}
		})
	}
	t.Run("a missing go.mod is an error", func(t *testing.T) {
		_, err := goModFindings(t.TempDir())
		assert.ErrorContains(t, err, "reading go.mod")
	})
	t.Run("a go.mod that does not parse is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, goModule), []byte("module (\n"), 0o600))
		_, err := goModFindings(dir)
		assert.ErrorContains(t, err, "parsing go.mod")
	})
}
