package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestBunEnvFiles_Gitignored reads the repository's own .gitignore: a
// personal override is refused only when committed, so .gitignore names every
// one, each env file Bun loads and each lefthook local form.
func TestBunEnvFiles_Gitignored(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".gitignore"))
	require.NoError(t, err)
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, name := range append(slices.Clone(bunEnvFiles), "lefthook-local.*", ".lefthook-local.*", ".lefthook-local/") {
		assert.Contains(t, lines, name, ".gitignore names %s on a line of its own", name)
	}
}

// Each case hands the check the tracked paths a pull request could commit,
// with the content of any it reads, and the check must refuse each by name or
// let it pass. Names differ from the refused ones only in case, or in a
// character that folds to theirs, wherever a case-insensitive filesystem opens
// one as the other.
func TestStartupFindings_Tracked(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		files   map[string]string
		wantIn  string
	}{
		{name: "Task's env file", tracked: []string{".env"}, wantIn: `".env" is tracked, and Taskfile.yml loads it as .env into every task`},
		{name: "Task's env file in capitals", tracked: []string{".ENV"}, wantIn: `".ENV" is tracked, and Taskfile.yml loads it as .env into every task`},
		{name: "a Bun env file", tracked: []string{".env.test.local"}, wantIn: `".env.test.local" is tracked, and Bun loads a file of that name`},
		{name: "a Bun env file in mixed case", tracked: []string{".Env.Local"}, wantIn: `".Env.Local" is tracked, and Bun loads a file of that name`},
		{name: "an env file below the root", tracked: []string{"docs/.env"}, wantIn: `"docs/.env" is tracked, and Bun loads a file of that name`},
		{name: "the env template", tracked: []string{".env.template"}},
		{name: "an npmrc", tracked: []string{".NPMRC"}, wantIn: `".NPMRC" is tracked, and bun install fetches from the registry an .npmrc names`},
		{name: "an npmrc below the root", tracked: []string{"docs/.npmrc"}, wantIn: `"docs/.npmrc" is tracked`},
		{name: "a package under node_modules", tracked: []string{"node_modules/prettier/index.mjs"}, wantIn: `"node_modules/prettier/index.mjs" is tracked under node_modules`},
		{name: "node_modules in mixed case", tracked: []string{"Node_Modules/.bin/prettier"}, wantIn: `"Node_Modules/.bin/prettier" is tracked under node_modules`},
		{
			name: "many paths under node_modules",
			tracked: []string{
				"node_modules/a", "node_modules/b", "node_modules/c", "node_modules/d",
				"node_modules/e", "node_modules/f", "node_modules/g",
			},
			wantIn: `"node_modules/e" and 2 more are tracked under node_modules`,
		},
		{name: "node_modules below the root", tracked: []string{"docs/node_modules/x/index.js"}, wantIn: `"docs/node_modules/x/index.js" is tracked under node_modules`},
		{name: "a directory that only starts like node_modules", tracked: []string{"node_modules_notes/a.md"}},
		{name: "a vendor directory", tracked: []string{"vendor/modules.txt"}, wantIn: `"vendor/modules.txt" is tracked under vendor, which go builds from`},
		{name: "vendor in capitals", tracked: []string{"Vendor/x/y.go"}, wantIn: "is tracked under vendor"},
		{name: "a vendor directory below the root, which go never reads", tracked: []string{"docs/vendor/a.md"}},
		{name: "a package.yaml, whose prettier key the row's named config never reads", tracked: []string{"package.yaml"}},
		{
			name:    "a prettier key below the root, which the row's named config never reads",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"name": "docs", "prettier": "./shared.mjs"}`},
		},
		{
			name:    "a prettier key at the root, which the row's named config never reads",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"prettier": {}}`},
		},
		{
			name:    "patchedDependencies at the root",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"patchedDependencies": {"prettier@3.9.8": "patches/p.patch"}}`},
			wantIn:  `"package.json" carries patchedDependencies`,
		},
		{
			name:    "patchedDependencies below the root, even empty",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"patchedDependencies": {}}`},
			wantIn:  `"docs/package.json" carries patchedDependencies, and bun install applies them`,
		},
		{
			name:    "a commitlint key at the root, which commitlint never reads under --config",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"commitlint": {"rules": {}}}`},
		},
		{
			name:    "a cosmiconfig key at the root, which changes nothing under --config",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"cosmiconfig": {"searchPlaces": ["rt.mjs"]}}`},
		},
		{
			name:    "a cosmiconfig key below the root, which commitlint never reads",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"cosmiconfig": {}}`},
		},
		{
			name:    "a commitlint key below the root, which commitlint never reads",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"commitlint": {}}`},
		},
		{
			name:    "a duplicated patchedDependencies, where Bun reads the first and Go the last",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"patchedDependencies": {"prettier@3.9.8": "p.patch"}, "name": "x", "patchedDependencies": null}`},
			wantIn:  `"package.json" is not valid JSON under RFC 7493, which refuses a duplicated key at any depth`,
		},
		{
			name:    "a duplicated key nested deep",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"devDependencies": {"prettier": "3.9.8", "prettier": "3.0.0"}}`},
			wantIn:  "refuses a duplicated key at any depth",
		},
		{
			name:    "a duplicate spelled with an escape",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"prettier": {}, "prettier": null}`},
			wantIn:  "refuses a duplicated key at any depth",
		},
		{
			name:    "a package.json that is not an object",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `["prettier"]`},
			wantIn:  `"docs/package.json" is not a JSON object`,
		},
		{
			name:    "the root package.json as it stands",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"name": "kickstart-go", "devDependencies": {"prettier": "3.9.8"}}`},
		},
		{name: "a tracked package.json the work tree deleted", tracked: []string{"docs/package.json"}},
		{name: "a file whose name only mentions Prettier", tracked: []string{"docs/prettierrc.md", "README.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStartupFiles(t, dir)
			for name, content := range tt.files {
				target := filepath.Join(dir, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
				require.NoError(t, os.WriteFile(target, []byte(content), 0o600))
			}
			found, err := startupFindings(dir, tt.tracked)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
		})
	}
}

// Each case writes bunfig.toml as a pull request could, and the check must
// refuse any key but [install] minimumReleaseAge, whatever its value, and
// pass a missing file.
func TestStartupFindings_Bunfig(t *testing.T) {
	tests := []struct {
		name   string
		bunfig *string
		wantIn string
	}{
		{name: "intact"},
		{name: "a top-level preload", bunfig: new("preload = [\"./x.mjs\"]\n" + intactBunfig), wantIn: `bunfig.toml carries "preload"`},
		{name: "a test preload", bunfig: new(intactBunfig + "[test]\npreload = [\"./x.mjs\"]\n"), wantIn: `bunfig.toml carries "test"`},
		{name: "a define table", bunfig: new(intactBunfig + "[define]\n\"process.env.X\" = \"'y'\"\n"), wantIn: `bunfig.toml carries "define"`},
		{name: "another install key", bunfig: new(intactBunfig + "registry = \"https://registry.invalid/\"\n"), wantIn: `bunfig.toml [install] carries "registry"`},
		{name: "another cooldown, a content change for the code owner", bunfig: new("[install]\nminimumReleaseAge = 0\n")},
		{name: "no install table", bunfig: new("# nothing\n")},
		{name: "bunfig.toml missing", bunfig: new("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStartupFiles(t, dir)
			switch {
			case tt.bunfig == nil:
			case *tt.bunfig == "":
				require.NoError(t, os.Remove(filepath.Join(dir, bunfig)))
			default:
				require.NoError(t, os.WriteFile(filepath.Join(dir, bunfig), []byte(*tt.bunfig), 0o600))
			}
			found, err := startupFindings(dir, nil)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
		})
	}

	t.Run("a bunfig.toml that does not parse is an error", func(t *testing.T) {
		dir := t.TempDir()
		writeStartupFiles(t, dir)
		require.NoError(t, os.WriteFile(filepath.Join(dir, bunfig), []byte("[install\n"), 0o600))
		_, err := startupFindings(dir, nil)
		assert.ErrorContains(t, err, "parsing")
	})
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
