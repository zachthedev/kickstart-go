package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each case hands the check tracked paths a pull request could commit, and
// the check must refuse each name a program the gate starts would read as its
// configuration, and let every other name pass. Case variants are the
// spellings a case-insensitive filesystem opens as the refused name.
func TestSearchFindings(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		wantIn  string
	}{
		{name: "an actionlint config", tracked: []string{".github/actionlint.yaml"}, wantIn: `".github/actionlint.yaml" is an actionlint config, and actionlint reads it`},
		{name: "the other actionlint config spelling, in capitals", tracked: []string{".GitHub/ActionLint.YML"}, wantIn: "is an actionlint config"},
		{name: "a lefthook local override", tracked: []string{"lefthook-local.yml"}, wantIn: `"lefthook-local.yml" is a lefthook local override, and lefthook merges it over lefthook.yml`},
		{name: "a hidden lefthook local override in capitals", tracked: []string{".Lefthook-Local.JSONC"}, wantIn: "is a lefthook local override"},
		{name: "a second lefthook main config", tracked: []string{".lefthook.jsonc"}, wantIn: "is a lefthook config"},
		{name: "a lowercase Taskfile", tracked: []string{"taskfile.yml"}, wantIn: `"taskfile.yml" is a Task config`},
		{name: "a .taskrc", tracked: []string{".taskrc.yml"}, wantIn: `".taskrc.yml" is a Task config`},
		{name: "a nested go.mod", tracked: []string{"internal/extra/go.mod"}, wantIn: `"internal/extra/go.mod" is a Go module file below the root, and every ./... row skips the module it starts`},
		{name: "a go.work", tracked: []string{"go.work"}, wantIn: `"go.work" is a Go workspace file`},
		{name: "a go.work.sum below the root", tracked: []string{"tools/go.work.sum"}, wantIn: "is a Go workspace file"},
		{name: "a tsconfig", tracked: []string{"tsconfig.json"}, wantIn: `"tsconfig.json" is a TypeScript or JavaScript project config, and Bun applies`},
		{name: "a jsconfig below the root, in capitals", tracked: []string{"docs/JSCONFIG.JSON"}, wantIn: "is a TypeScript or JavaScript project config"},
		{
			name: "every file the gate names, in its own spelling",
			tracked: []string{
				".prettierrc", ".golangci.yml", ".taplo.toml", ".github/zizmor.yml", "commitlint.config.js",
				"lefthook.yml", "Taskfile.yml", "go.mod",
			},
		},
		{name: "a root-only name below the root", tracked: []string{"docs/lefthook-local.yml", "docs/Taskfile.yaml", "docs/.github/actionlint.yaml"}},
		{
			name: "a config the row names, which stops every other read",
			tracked: []string{
				"docs/prettier.config.mjs", ".prettierrc.json", ".golangci.json", ".Golangci.INI", "taplo.toml",
				"zizmor.yml", ".github/zizmor.yaml", ".commitlintrc.json", "commitlint.config.ts",
			},
		},
		{name: "a workflow named like a zizmor config", tracked: []string{".github/workflows/zizmor.yml"}},
		{name: "a file whose name only mentions a tool", tracked: []string{"docs/prettierrc.md", "docs/golangci.md", "README.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := searchFindings(t.TempDir(), tt.tracked)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
			assert.True(t, strings.HasSuffix(found[0], "Remove it"), "the finding says what to do: %s", found[0])
		})
	}
}

// Each case plants files in the work tree, some of them tracked, and the
// check must refuse a config on disk whether or not it is committed, where the
// program that reads it looks: never a personal override untracked, nothing
// under node_modules or a worktree, a go.mod only where ./... reaches, and a
// tsconfig or a go.work at the root alone. A tracked file is refused once.
func TestSearchFindings_OnDisk(t *testing.T) {
	tests := []struct {
		name    string
		disk    []string
		tracked []string
		want    []string
	}{
		{name: "an untracked actionlint config", disk: []string{".github/actionlint.yaml"}, want: []string{"is an actionlint config"}},
		{name: "an untracked second Taskfile", disk: []string{"Taskfile.yaml"}, want: []string{`"Taskfile.yaml" is a Task config`}},
		{name: "an untracked go.mod where ./... reaches", disk: []string{"internal/extra/go.mod"}, want: []string{"is a Go module file below the root"}},
		{name: "an untracked go.work at the root", disk: []string{"go.work"}, want: []string{`"go.work" is a Go workspace file`}},
		{name: "an untracked tsconfig at the root, in capitals", disk: []string{"TSConfig.json"}, want: []string{"is a TypeScript or JavaScript project config"}},
		{name: "a tracked config is refused once", disk: []string{"taskfile.yml"}, tracked: []string{"taskfile.yml"}, want: []string{`"taskfile.yml" is a Task config`}},
		{name: "an untracked config the row names, which stops every other read", disk: []string{".golangci.json", "docs/.prettierrc.json", "taplo.toml", "zizmor.yml", ".commitlintrc.json"}},
		{name: "an untracked lefthook local override is the contributor's own", disk: []string{"lefthook-local.yml", ".lefthook-local.toml"}},
		{name: "a tracked lefthook local override", disk: []string{"lefthook-local.yml"}, tracked: []string{"lefthook-local.yml"}, want: []string{"is a lefthook local override"}},
		{
			name: "an install and a worktree",
			disk: []string{
				"node_modules/pkg/tsconfig.json", "node_modules/pkg/go.mod", "NODE_MODULES/other/go.mod",
				"docs/node_modules/pkg/go.mod", ".claude/worktrees/wt/go.mod",
				".Claude/WorkTrees/other/Taskfile.yaml",
			},
		},
		{name: "a go.mod ./... never reaches", disk: []string{"_scratch/go.mod", "internal/testdata/mod/go.mod", ".hidden/go.mod"}},
		{name: "a tsconfig and a go.work below the root", disk: []string{"docs/tsconfig.json", "tools/go.work", "tools/go.work.sum"}},
		{name: "a go.mod inside a version control directory", disk: []string{".git/go.mod", "docs/.hg/go.mod"}},
		{name: "every file the gate names, on disk", disk: []string{".prettierrc", ".golangci.yml", ".taplo.toml", ".github/zizmor.yml", "commitlint.config.js", "lefthook.yml", "Taskfile.yml", "go.mod"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.disk {
				target := filepath.Join(dir, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
				require.NoError(t, os.WriteFile(target, nil, 0o600))
			}
			found, err := searchFindings(dir, tt.tracked)
			require.NoError(t, err)
			require.Len(t, found, len(tt.want), "findings: %q", found)
			for i, want := range tt.want {
				assert.Contains(t, found[i], want)
			}
		})
	}
}

func TestSearchFindings_Unreadable(t *testing.T) {
	_, err := searchFindings(filepath.Join(t.TempDir(), "absent"), nil)
	assert.ErrorContains(t, err, "walking the work tree")
}

func TestWithExtensions(t *testing.T) {
	got := withExtensions([]string{"a", ".b"}, ".x", "")
	assert.Equal(t, []string{"a.x", "a", ".b.x", ".b"}, got)
}
