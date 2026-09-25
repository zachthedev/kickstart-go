package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"zach.tools/go/kickstart/internal/gittest"
)

// Each case plants one link where mise looks for config files, and the check
// must refuse it by name. The last plants a link elsewhere, which mise never
// reaches.
func TestLinkFindings(t *testing.T) {
	tests := []struct {
		name   string
		link   string
		target string
		wantIn string
	}{
		{name: "a link at the root", link: "mise.toml.link", target: "elsewhere.toml", wantIn: `"mise.toml.link" is a link`},
		{name: "a linked .config", link: ".config", target: "elsewhere", wantIn: `".config" is a link`},
		{name: "a link under .mise", link: ".mise/config.toml", target: "../elsewhere.toml", wantIn: `".mise/config.toml" is a link`},
		{name: "a link under mise", link: "mise/config.toml", target: "../elsewhere.toml", wantIn: `"mise/config.toml" is a link`},
		{name: "a link mise never reaches", link: "docs/elsewhere.md", target: "../elsewhere.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "elsewhere.toml"), nil, 0o600))
			require.NoError(t, os.Mkdir(filepath.Join(dir, "elsewhere"), 0o700))
			link := filepath.Join(dir, filepath.FromSlash(tt.link))
			require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
			if err := os.Symlink(filepath.FromSlash(tt.target), link); err != nil {
				t.Skipf("this machine cannot create a symlink, so the case cannot be planted: %v", err)
			}
			found, err := linkFindings(dir)
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

// TestGitTracked asks the real git what it tracks. The environment cases each
// plant a variable a committed .env could set, and git must list the
// checkout's own index whatever it says.
func TestGitTracked(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	gittest.Isolate(t)
	dir := t.TempDir()
	other := t.TempDir()
	for _, repo := range []string{dir, other} {
		require.NoError(t, exec.Command(git, "-C", repo, "init", "--quiet").Run())
	}
	for _, name := range []string{"Tracked.TXT", "docs/nested.txt", "untracked.txt"} {
		target := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, []byte("PROBE=1\n"), 0o600))
	}
	require.NoError(t, exec.Command(git, "-C", dir, "add", "--force", "Tracked.TXT", "docs/nested.txt").Run())
	require.NoError(t, os.WriteFile(filepath.Join(other, "elsewhere.txt"), nil, 0o600))
	require.NoError(t, exec.Command(git, "-C", other, "add", "elsewhere.txt").Run())

	want := []string{"Tracked.TXT", "docs/nested.txt"}
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "the checkout's own index"},
		{name: "an inherited GIT_INDEX_FILE naming no index", env: map[string]string{"GIT_INDEX_FILE": filepath.Join(dir, ".git", "no-such-index")}},
		{name: "an inherited GIT_DIR naming another repository", env: map[string]string{"GIT_DIR": filepath.Join(other, ".git")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			got, err := gitTracked(t.Context(), dir)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	t.Run("outside a repository is an error that carries git's reason", func(t *testing.T) {
		_, err := gitTracked(t.Context(), t.TempDir())
		assert.ErrorContains(t, err, "git rev-parse")
		assert.ErrorContains(t, err, "not a git repository")
		assert.NotContains(t, err.Error(), "directory's owner", "only an ownership stop names the owner")
	})
	t.Run("an empty .git directory inside another repository is refused", func(t *testing.T) {
		inner := filepath.Join(other, "inner")
		require.NoError(t, os.MkdirAll(filepath.Join(inner, ".git"), 0o700))
		got, err := gitTracked(t.Context(), inner)
		assert.ErrorContains(t, err, "not the checkout at")
		assert.ErrorContains(t, err, "An empty .git directory in the checkout does this")
		assert.Nil(t, got, "the parent repository's elsewhere.txt never reaches a row")
	})
	t.Run("a subdirectory of the checkout is refused", func(t *testing.T) {
		_, err := gitTracked(t.Context(), filepath.Join(dir, "docs"))
		assert.ErrorContains(t, err, "not the checkout at")
	})
}

// TestGitTracked_DubiousOwnership starts the stand-in git, which stops as git
// does in a checkout another account owns, and the error must say that
// git's own safe.directory remedy cannot reach the gate.
func TestGitTracked_DubiousOwnership(t *testing.T) {
	t.Setenv("PATH", fakeProgramDir(t, "git"))
	_, err := gitTracked(t.Context(), t.TempDir())
	assert.ErrorContains(t, err, "detected dubious ownership")
	assert.ErrorContains(t, err, "The gate reads no global git config, so a safe.directory entry cannot reach it. The fix is the directory's owner")
}

func TestGitEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		folders map[string]string
		want    []string
	}{
		{
			name:    "Windows, where the system names SystemRoot",
			folders: map[string]string{"SystemRoot": `C:\Windows`, "LOCALAPPDATA": `C:\Users\probe\AppData\Local`},
			want:    []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", `SystemRoot=C:\Windows`},
		},
		{name: "elsewhere", want: []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, gitEnvironment(tt.folders))
		})
	}
}

// Each case is a spelling a filesystem can open as the name beside it in
// another case, which fold must key alike, or a different name, which it must
// keep apart. A default-ignorable code point stays in the key: the one
// filesystem that dropped them is HFS+, which current Macs do not use.
func TestFold(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "ASCII case", a: ".ENV", b: ".env", want: true},
		{name: "mixed case", a: "Prettier.Config.MJS", b: "prettier.config.mjs", want: true},
		{name: "a long s", a: ".prettierrc.j\U0000017Fon", b: ".prettierrc.json", want: true},
		{name: "the Kelvin sign", a: "pac\U0000212Aage.json", b: "package.json", want: true},
		{name: "a dotless i", a: "prett\U00000131er.config.js", b: "prettier.config.js", want: true},
		{name: "a sharp s, under the full mapping", a: "cla\U000000DF.toml", b: "class.toml", want: true},
		{name: "a zero-width space, which the key keeps", a: ".e\U0000200Bnv", b: ".env", want: false},
		{name: "a soft hyphen, which the key keeps", a: "go.\U000000ADwork", b: "go.work", want: false},
		{name: "a byte order mark, which the key keeps", a: "\U0000FEFF.config", b: ".config", want: false},
		{name: "another name", a: ".env.local", b: ".env", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fold(tt.a) == fold(tt.b))
		})
	}
}

// Each case plants one entry at the root, and the check must refuse .config,
// vendor and package.yaml in any form and any case, and pass everything else.
// A lone single quote is the shared workflows job's to refuse.
func TestRootFindings(t *testing.T) {
	tests := []struct {
		name   string
		plant  func(t *testing.T, dir string)
		wantIn string
	}{
		{name: "a .config directory", plant: func(t *testing.T, dir string) { mkdir(t, dir, ".config") }, wantIn: `".config" sits at the root, and mise`},
		{name: "a .config file in capitals", plant: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".CONFIG"), nil, 0o600))
		}, wantIn: `".CONFIG" sits at the root`},
		{name: "a vendor directory", plant: func(t *testing.T, dir string) { mkdir(t, dir, "Vendor") }, wantIn: `"Vendor" sits at the root, and go builds from it`},
		{name: "a config directory with no dot", plant: func(t *testing.T, dir string) { mkdir(t, dir, "config") }},
		{name: "a .config below the root", plant: func(t *testing.T, dir string) { mkdir(t, dir, "docs/.config") }},
		{name: "a package.yaml", plant: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "package.yaml"), []byte("cosmiconfig:\n  $import: [./probe.mjs]\n"), 0o600))
		}, wantIn: `"package.yaml" sits at the root, and cosmiconfig reads a cosmiconfig key in it as commitlint's meta config`},
		{name: "a package.yaml in capitals, empty", plant: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "Package.YAML"), nil, 0o600))
		}, wantIn: `"Package.YAML" sits at the root`},
		{name: "a package.yaml below the root", plant: func(t *testing.T, dir string) { mkdir(t, dir, "docs/package.yaml") }},
		{name: "a package.yml, which cosmiconfig never reads", plant: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "package.yml"), nil, 0o600))
		}},
		{name: "a directory named by a single quote", plant: func(t *testing.T, dir string) { mkdir(t, dir, "'/tmp") }},
		{name: "a file named by a single quote", plant: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "'"), nil, 0o600))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.plant(t, dir)
			found, err := rootFindings(dir)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
			assert.True(t, strings.HasSuffix(found[0], "Remove it, committed or not"))
		})
	}
	t.Run("an unreadable root is an error", func(t *testing.T) {
		_, err := rootFindings(filepath.Join(t.TempDir(), "absent"))
		assert.ErrorContains(t, err, "listing")
	})
}

// mkdir makes the directory name under dir.
func mkdir(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.FromSlash(name)), 0o700))
}

// TestTreeFindings_Lister carries the lister's error out, with what failed.
func TestTreeFindings_Lister(t *testing.T) {
	failing := func(string) ([]string, error) { return nil, os.ErrPermission }
	_, err := treeFindings(t.TempDir(), failing)
	assert.ErrorContains(t, err, "listing tracked files")
	assert.True(t, strings.Contains(err.Error(), os.ErrPermission.Error()))
}
