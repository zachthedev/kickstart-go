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
// let it pass. Names differ from the refused ones only in case wherever a
// case-insensitive filesystem opens one as the other. A path the shared
// commits job refuses before a merge passes here: an env file at the root, a
// node_modules path, an .npmrc, a patchedDependencies key.
func TestStartupFindings_Tracked(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		files   map[string]string
		wantIn  string
	}{
		{name: "Task's env file at the root, which the commits job refuses", tracked: []string{".env"}},
		{name: "a Bun env file at the root, which the commits job refuses", tracked: []string{".env.test.local", ".Env.Local"}},
		{name: "an env file below the root", tracked: []string{"docs/.env"}, wantIn: `"docs/.env" is tracked, and Bun loads a file of that name into its environment from the directory it starts in`},
		{name: "a Bun env file below the root in mixed case", tracked: []string{"tools/sub/.Env.Production.Local"}, wantIn: `"tools/sub/.Env.Production.Local" is tracked, and Bun loads`},
		{name: "the env template", tracked: []string{".env.template", "docs/.env.template"}},
		{name: "a name that only starts like an env file", tracked: []string{"docs/.envrc", "docs/.env.example"}},
		{name: "an npmrc, which the commits job refuses", tracked: []string{".NPMRC", "docs/.npmrc"}},
		{name: "a package under node_modules, which the commits job refuses", tracked: []string{"node_modules/prettier/index.mjs", "docs/Node_Modules/x/index.js"}},
		{name: "a vendor directory", tracked: []string{"vendor/modules.txt"}, wantIn: `"vendor/modules.txt" is tracked under vendor, which go builds from`},
		{name: "vendor in capitals", tracked: []string{"Vendor/x/y.go"}, wantIn: "is tracked under vendor"},
		{
			name:    "many paths under vendor",
			tracked: []string{"vendor/a", "vendor/b", "vendor/c", "vendor/d", "vendor/e", "vendor/f", "vendor/g"},
			wantIn:  `"vendor/e" and 2 more are tracked under vendor`,
		},
		{name: "a vendor directory below the root, which go never reads", tracked: []string{"docs/vendor/a.md"}},
		{name: "a directory that only starts like vendor", tracked: []string{"vendored/a.go"}},
		{
			name:    "patchedDependencies at the root",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"patchedDependencies": {"prettier@3.9.8": "patches/p.patch"}}`},
			wantIn:  `"package.json" carries patchedDependencies, and bun install applies them`,
		},
		{
			name:    "patchedDependencies below the root, even empty",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"patchedDependencies": {}}`},
			wantIn:  `"docs/package.json" carries patchedDependencies`,
		},
		{
			name:    "patchedDependencies beside a value nested 300 deep, past the runner's jq and within Bun's reach",
			tracked: []string{"package.json"},
			files: map[string]string{"package.json": `{"patchedDependencies": {"left-pad@1.3.0": "patches/p.patch"}, "deep": ` +
				strings.Repeat("[", 300) + strings.Repeat("]", 300) + `}`},
			wantIn: `"package.json" carries patchedDependencies`,
		},
		{
			name:    "a duplicated patchedDependencies, where Bun reads the first and jq and Go the last",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"patchedDependencies": {"prettier@3.9.8": "p.patch"}, "name": "x", "patchedDependencies": null}`},
			wantIn:  `"package.json" is not valid JSON under RFC 7493, which refuses a duplicated key at any depth`,
		},
		{
			name:    "a duplicated key nested deep, below the root, in capitals",
			tracked: []string{"docs/PACKAGE.JSON"},
			files:   map[string]string{"docs/PACKAGE.JSON": `{"devDependencies": {"prettier": "3.9.8", "prettier": "3.0.0"}}`},
			wantIn:  `"docs/PACKAGE.JSON" is not valid JSON under RFC 7493`,
		},
		{
			name:    "a duplicate spelled with an escape",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"prettier": {}, "prettier": null}`},
			wantIn:  "refuses a duplicated key at any depth",
		},
		{
			name:    "a package.json that does not parse",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"name": `},
			wantIn:  `"package.json" is not valid JSON under RFC 7493`,
		},
		{
			name:    "a cosmiconfig key at the root, whose $import runs a module inside commitlint",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"name": "x", "cosmiconfig": {"$import": ["./probe.mjs", "./absent.json"]}}`},
			wantIn:  `"package.json" carries a cosmiconfig key, which cosmiconfig reads as commitlint's meta config whatever config commitlint names`,
		},
		{
			name:    "an empty cosmiconfig key at the root",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"cosmiconfig": {}}`},
			wantIn:  `"package.json" carries a cosmiconfig key`,
		},
		{
			name:    "a cosmiconfig key below the root, which commitlint never reads",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `{"cosmiconfig": {"$import": ["./probe.mjs"]}}`},
		},
		{
			name:    "a key that only names cosmiconfig in its value",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"description": "cosmiconfig"}`},
		},
		{
			name:    "a package.json that is not an object, which jq's has fails on",
			tracked: []string{"docs/package.json"},
			files:   map[string]string{"docs/package.json": `["patchedDependencies"]`},
			wantIn:  `"docs/package.json" is not a JSON object`,
		},
		{
			name:    "the root package.json as it stands",
			tracked: []string{"package.json"},
			files:   map[string]string{"package.json": `{"name": "kickstart-go", "scripts": {"audit": "bun audit --audit-level=high"}, "devDependencies": {"prettier": "3.9.8"}}`},
		},
		{name: "a tracked package.json the work tree deleted", tracked: []string{"docs/package.json"}},
		{name: "a file whose name only mentions a package", tracked: []string{"docs/package.json.md", "README.md"}},
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

	t.Run("a tracked package.json that is a directory is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs", "package.json"), 0o700))
		_, err := startupFindings(dir, []string{"docs/package.json"})
		assert.ErrorContains(t, err, "reading docs/package.json")
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
