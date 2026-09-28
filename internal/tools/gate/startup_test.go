package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bunfig is the file that holds Bun's install cooldown, which the fixtures
// carry beside go.mod as the repository does.
const bunfig = "bunfig.toml"

// intactBunfig is a bunfig.toml holding the install cooldown alone.
const intactBunfig = "[install]\nminimumReleaseAge = 259200 # 3 days\n"

// intactGoMod is a go.mod carrying no directive the gate refuses.
const intactGoMod = "module example.com/probe\n\ngo 1.27.0\n"

// writeStartupFiles writes the intact bunfig.toml and a go.mod into dir.
func writeStartupFiles(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, bunfig), []byte(intactBunfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, goModule), []byte(intactGoMod), 0o600))
}

// TestStartupFindings_GitignoreNamesPersonalFiles reads the repository's own
// .gitignore. A personal file is refused only when committed: an env file Bun
// loads by the shared commits job, and a lefthook local override by the gate.
// So .gitignore names every one, and an untracked one stays out of a commit.
func TestStartupFindings_GitignoreNamesPersonalFiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".gitignore"))
	require.NoError(t, err)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	personal := []string{
		".env", ".env.local",
		".env.development", ".env.development.local",
		".env.production", ".env.production.local",
		".env.test", ".env.test.local",
		"lefthook-local.*", ".lefthook-local.*", ".lefthook-local/",
	}
	for _, name := range personal {
		assert.Contains(t, lines, name, ".gitignore names %s on a line of its own", name)
	}
}

// Each case hands the check the tracked paths a pull request could commit, and
// the check must refuse each by name or let it pass. Names differ from the
// refused ones only in case wherever a case-insensitive filesystem opens one
// as the other. What the shared commits job refuses before a merge passes
// here, since the check reads no file: an env file, a node_modules path and a
// package.json.
func TestStartupFindings_Tracked(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		wantIn  string
	}{
		{name: "a vendor directory", tracked: []string{"vendor/modules.txt"}, wantIn: `"vendor/modules.txt" is tracked under vendor, which go builds from`},
		{name: "vendor in capitals", tracked: []string{"Vendor/x/y.go"}, wantIn: "is tracked under vendor"},
		{
			name:    "many paths under vendor",
			tracked: []string{"vendor/a", "vendor/b", "vendor/c", "vendor/d", "vendor/e", "vendor/f", "vendor/g"},
			wantIn:  `"vendor/e" and 2 more are tracked under vendor`,
		},
		{name: "a vendor directory below the root, which go never reads", tracked: []string{"docs/vendor/a.md"}},
		{name: "a directory that only starts like vendor", tracked: []string{"vendored/a.go"}},
		{name: "an env file below the root, which the commits job refuses", tracked: []string{"docs/.env", "tools/sub/.Env.Production.Local"}},
		{name: "a package.json, which the commits job reads", tracked: []string{"package.json", "docs/PACKAGE.JSON"}},
		{name: "a package under node_modules, which the commits job refuses", tracked: []string{"node_modules/prettier/index.mjs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := startupFindings(tt.tracked)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
		})
	}
}

func TestExcerpt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "short text, quoted whole", input: "a\x1bb", want: `"a\x1bb"`},
		{name: "long text, cut", input: strings.Repeat("x", excerptBytes+1), want: `"` + strings.Repeat("x", excerptBytes) + `"...`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, excerpt([]byte(tt.input)))
		})
	}
}
