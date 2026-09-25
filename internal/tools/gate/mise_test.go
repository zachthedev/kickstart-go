package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pins every mise child carries, whatever it inherited.
var wantPins = []string{
	"MISE_OVERRIDE_CONFIG_FILENAMES=mise.toml",
	"MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES=none",
	"MISE_ENV=",
	"MISE_AUTO_ENV=false",
	`MISE_URL_REPLACEMENTS={"regex:^https://api\\.github\\.com/repos/[^/]+/[^/]+/releases/assets/.*$":"https://url-api-refused.invalid/"}`,
}

// TestMain lets the test binary stand in for the programs the gate starts: a
// copy named after one runs that program's fake instead of the tests, so no
// test starts a real mise, git, taplo, bun, actionlint, gh, go, zizmor or
// ShellCheck.
func TestMain(m *testing.M) {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	switch {
	case strings.HasPrefix(base, "shellcheck"):
		os.Exit(fakeShellCheck(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]))
	case strings.HasPrefix(base, "mise"):
		os.Exit(fakeMise(os.Stdout, os.Args[1:]))
	case strings.HasPrefix(base, "git"):
		os.Exit(fakeGit(os.Stderr))
	case strings.HasPrefix(base, "taplo"):
		os.Exit(fakeTaplo(os.Stderr, os.Args[1:]))
	case strings.HasPrefix(base, "bun"):
		os.Exit(fakeBun(os.Stdout))
	case strings.HasPrefix(base, "actionlint"):
		os.Exit(fakeActionlintProgram(os.Stderr, os.Args[1:]))
	case strings.HasPrefix(base, "gh"):
		os.Exit(fakeGhProgram(os.Stdout, os.Stderr))
	case strings.HasPrefix(base, "go"):
		os.Exit(fakeGoProgram(os.Stdout, os.Args[1:]))
	case strings.HasPrefix(base, "zizmor"):
		os.Exit(fakeZizmorProgram(os.Stdout, os.Stderr, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeMise prints every environment entry it received, the value only for a
// MISE_ name and NO_COLOR, and exits with the code an "exit=<n>" argument
// names.
func fakeMise(out io.Writer, args []string) int {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if upper := strings.ToUpper(name); strings.HasPrefix(upper, "MISE_") || upper == "NO_COLOR" {
			fmt.Fprintf(out, "%s=%s\n", name, value)
		} else {
			fmt.Fprintln(out, name)
		}
	}
	for _, arg := range args {
		if code, ok := strings.CutPrefix(arg, "exit="); ok {
			n, _ := strconv.Atoi(code)
			return n
		}
	}
	return 0
}

// fakeGit answers as git does for a checkout another account owns.
func fakeGit(stderr io.Writer) int {
	fmt.Fprintln(stderr, "fatal: detected dubious ownership in repository at 'checkout'")
	return 128
}

// fakeTaplo logs taplo 0.10.0's found files line for every file after -- that
// exists, as absolute paths with forward slashes, and exits 0 as taplo does
// whatever it found.
func fakeTaplo(stderr io.Writer, args []string) int {
	_, files, _ := cutArgs(args, "--")
	var quoted []string
	for _, name := range files {
		if abs, err := filepath.Abs(name); err == nil {
			if _, err := os.Stat(abs); err == nil {
				quoted = append(quoted, strconv.Quote(filepath.ToSlash(abs)))
			}
		}
	}
	fmt.Fprintf(stderr, " INFO taplo:format_files:collect_files: found files total=%d excluded=0 files=[%s] cwd=\".\"\n", len(quoted), strings.Join(quoted, ", "))
	return 0
}

// fakeBun prints what Prettier's --check --debug-check prints over a tree:
// its opening line, then each regular file in the working directory.
func fakeBun(stdout io.Writer) int {
	fmt.Fprintln(stdout, "Checking formatting...")
	entries, _ := os.ReadDir(".")
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			fmt.Fprintln(stdout, entry.Name())
		}
	}
	return 0
}

// fakeActionlintProgram logs actionlint's -verbose finished line for each file
// after -- and exits 0.
func fakeActionlintProgram(stderr io.Writer, args []string) int {
	_, files, _ := cutArgs(args, "--")
	for _, name := range files {
		fmt.Fprintf(stderr, "verbose: Found total 0 errors in 1 ms for %s\n", name)
	}
	return 0
}

// programName is name as the platform spells an executable.
func programName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// fakeProgramDir copies the test binary into a fresh directory as name and
// returns the directory.
func fakeProgramDir(t *testing.T, name string) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, programName(name)), data, 0o700)) // #nosec G703 -- the test copies its own binary into its own temporary directory
	return dir
}

// fakeMiseDir copies the test binary into a fresh directory as mise and
// returns the directory.
func fakeMiseDir(t *testing.T) string {
	t.Helper()
	return fakeProgramDir(t, "mise")
}

func TestMiseEnvironment(t *testing.T) {
	inherited := []string{
		"=C:=C:\\work",
		"Path=C:\\bin",
		"PATH=/usr/bin",
		"HOME=/home/probe",
		"TMPDIR=/tmp",
		"TEMP=C:\\Temp",
		"TMP=C:\\Tmp",
		"SystemRoot=C:\\planted-root",
		"LOCALAPPDATA=C:\\planted-local",
		"USERPROFILE=C:\\Users\\probe",
		"https_proxy=http://proxy.invalid:3128",
		"ALL_PROXY=http://proxy.invalid:3128",
		"SSL_CERT_FILE=probe-ca.pem",
		"MISE_GLOBAL_CONFIG_FILE=probe-global.toml",
		"mise_url_replacements={}",
		"Mise_Env=ci",
		"MISE_DATA_DIR=probe-data",
		"MISE_TRUSTED_CONFIG_PATHS=/elsewhere",
		"XDG_CONFIG_HOME=probe-xdg",
		"GH_TOKEN=fake-token",
		"GODEBUG=execerrdot=0",
	}
	folders := map[string]string{"SystemRoot": "C:\\Windows", "LOCALAPPDATA": "C:\\Users\\probe\\AppData\\Local"}
	tests := []struct {
		goos    string
		folders map[string]string
		want    []string
	}{
		{goos: "windows", folders: folders, want: []string{
			"TEMP=C:\\Temp", "TMP=C:\\Tmp", "https_proxy=http://proxy.invalid:3128",
			"SystemRoot=C:\\Windows", "LOCALAPPDATA=C:\\Users\\probe\\AppData\\Local",
		}},
		{goos: "linux", want: []string{"HOME=/home/probe", "TMPDIR=/tmp", "https_proxy=http://proxy.invalid:3128"}},
		{goos: "darwin", want: []string{"HOME=/home/probe", "TMPDIR=/tmp", "https_proxy=http://proxy.invalid:3128"}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := miseEnvironment(inherited, tt.goos, "/work", tt.folders)
			want := append(slices.Clone(tt.want), wantPins...)
			assert.ElementsMatch(t, append(want, "MISE_TRUSTED_CONFIG_PATHS=/work"), got)
		})
	}
}

func TestResolveProgram(t *testing.T) {
	fake := fakeMiseDir(t)
	// A checkout holding the stand-in two levels down, in bin/tools.
	checkout := t.TempDir()
	nested := filepath.Join(checkout, "bin", "tools")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	require.NoError(t, os.CopyFS(nested, os.DirFS(fake)))

	t.Run("a program on PATH outside the checkout", func(t *testing.T) {
		t.Setenv("PATH", fake)
		got, err := resolveProgram("mise", t.TempDir())
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))
		assert.Equal(t, fake, filepath.Dir(got))
	})
	t.Run("a program at the checkout root", func(t *testing.T) {
		t.Setenv("PATH", fake)
		_, err := resolveProgram("mise", fake)
		assert.ErrorContains(t, err, "inside the checkout")
	})
	t.Run("a program two directories into the checkout", func(t *testing.T) {
		t.Setenv("PATH", nested)
		_, err := resolveProgram("mise", checkout)
		assert.ErrorContains(t, err, "inside the checkout")
	})
	t.Run("a PATH entry that reaches the checkout through a link", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(checkout, alias); err != nil {
			t.Skipf("this machine cannot create a symlink, so the second spelling cannot be planted: %v", err)
		}
		t.Setenv("PATH", filepath.Join(alias, "bin", "tools"))
		_, err := resolveProgram("mise", checkout)
		assert.ErrorContains(t, err, "inside the checkout")
	})
	t.Run("a PATH entry outside the checkout that links to a directory inside it", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "tools")
		if err := os.Symlink(nested, alias); err != nil {
			t.Skipf("this machine cannot create a symlink, so the link cannot be planted: %v", err)
		}
		t.Setenv("PATH", alias)
		_, err := resolveProgram("mise", checkout)
		assert.ErrorContains(t, err, "inside the checkout")
	})
	t.Run("a program nowhere on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := resolveProgram("mise", t.TempDir())
		assert.ErrorContains(t, err, "finding mise on PATH")
	})
	t.Run("a checkout that does not exist", func(t *testing.T) {
		t.Setenv("PATH", fake)
		_, err := resolveProgram("mise", filepath.Join(t.TempDir(), "absent"))
		assert.ErrorContains(t, err, "inspecting")
	})
}

// TestRun_Mise drives the mise subcommand into the stand-in: the child sees
// the allow-list and the pins, never an inherited MISE_ name or token, and the
// subcommand exits with the child's code.
func TestRun_Mise(t *testing.T) {
	_, git := intactCheckout(t)
	t.Setenv("PATH", fakeMiseDir(t)+string(os.PathListSeparator)+filepath.Dir(git))
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", "probe-global.toml")
	t.Setenv("MISE_URL_REPLACEMENTS", "{}")
	t.Setenv("XDG_CONFIG_HOME", "probe-xdg")
	t.Setenv("GH_TOKEN", "fake-token")
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", "elsewhere")
	// The working directory as the runner reads it, which a symlinked
	// temporary root can spell differently from t.TempDir.
	root, err := os.Getwd()
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"mise", "which", "taplo", "exit=3"}, strings.NewReader(""), &stdout, &stderr)
	require.Equal(t, 3, code, "stderr: %s", stderr.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=") {
			continue
		}
		name, _, _ := strings.Cut(line, "=")
		switch {
		case strings.HasPrefix(strings.ToUpper(name), "MISE_"):
			assert.Contains(t, append(slices.Clone(wantPins), "MISE_TRUSTED_CONFIG_PATHS="+root), line)
		default:
			allowed := slices.Concat(miseInherited["unix"], miseInherited["windows"], []string{"SystemRoot", "LOCALAPPDATA"})
			assert.True(t, slices.ContainsFunc(allowed, func(want string) bool { return strings.EqualFold(want, name) }), "the child inherited %s", name)
		}
	}
	for _, pin := range wantPins {
		assert.Contains(t, lines, pin)
	}
	assert.NotContains(t, stdout.String(), "MISE_GLOBAL_CONFIG_FILE")
	assert.NotContains(t, stdout.String(), "XDG_CONFIG_HOME")
	assert.NotContains(t, stdout.String(), "GH_TOKEN")
	assert.Contains(t, lines, "MISE_TRUSTED_CONFIG_PATHS="+root)
}

// TestRun_MiseRefused plants what the pins checks refuse, and mise must never
// start. The stand-in prints at least the pins whenever it runs, so an empty
// stdout means it never ran.
func TestRun_MiseRefused(t *testing.T) {
	tests := []struct {
		name   string
		plant  func(t *testing.T, dir string)
		wantIn string
	}{
		{
			name: "an env table in mise.toml",
			plant: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(intactPins+"\n[env]\nPROBE = \"1\"\n"), 0o600))
			},
			wantIn: `mise.toml carries "env"`,
		},
		{
			name: "a stray mise config",
			plant: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "mise.local.toml"), []byte(intactPins), 0o600))
			},
			wantIn: `"mise.local.toml" is a mise configuration or lock file`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, git := intactCheckout(t)
			t.Setenv("PATH", fakeMiseDir(t)+string(os.PathListSeparator)+filepath.Dir(git))
			tt.plant(t, dir)
			var stdout, stderr bytes.Buffer
			assert.Equal(t, 1, run(t.Context(), []string{"mise", "which", "taplo"}, strings.NewReader(""), &stdout, &stderr))
			assert.Contains(t, stderr.String(), tt.wantIn)
			assert.Empty(t, stdout.String(), "the stand-in mise started")
		})
	}
}

func TestRun_MiseMissing(t *testing.T) {
	_, git := intactCheckout(t)
	t.Setenv("PATH", filepath.Dir(git))
	if found, err := exec.LookPath("mise"); err == nil {
		t.Skipf("mise sits beside git at %s, so this case would start it", found)
	}
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, run(t.Context(), []string{"mise", "which", "taplo"}, strings.NewReader(""), &stdout, &stderr))
	assert.Contains(t, stderr.String(), "finding mise on PATH")
}
