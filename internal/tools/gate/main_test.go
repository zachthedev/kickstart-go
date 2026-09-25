package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"zach.tools/go/kickstart/internal/gittest"
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
		{name: "toml without taplo", args: []string{"toml"}, wantIn: "the taplo path is required"},
		{name: "actionlint with one path", args: []string{"actionlint", "actionlint-path"}, wantIn: "the actionlint and shellcheck paths are required"},
		{name: "zizmor without zizmor", args: []string{"zizmor"}, wantIn: "the zizmor path is required"},
		{name: "scripts without shellcheck", args: []string{"scripts"}, wantIn: "the shellcheck path is required"},
		{name: "packages without platforms", args: []string{"packages", "-release", "linux/amd64"}, wantIn: "gate packages: -platforms names no os/arch pair. Taskfile.yml passes -platforms, -tags and -release"},
		{name: "packages with a platform that is no pair", args: []string{"packages", "-platforms", "linux"}, wantIn: `"linux" is not an os/arch pair`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run(t.Context(), tt.args, strings.NewReader(""), &stdout, &stderr)
			assert.Equal(t, 2, got)
			assert.Contains(t, stderr.String(), tt.wantIn)
			assert.Empty(t, stdout.String())
		})
	}
}

// intactCheckout makes a git repository holding the intact mise pair and
// startup files, none of them tracked, makes it the working directory, and
// returns it with the git it found.
func intactCheckout(t *testing.T) (string, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH, and the pins checks ask it what the checkout tracks")
	}
	gittest.Isolate(t)
	dir := t.TempDir()
	require.NoError(t, exec.Command(git, "-C", dir, "init", "--quiet").Run())
	writeStartupFiles(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(intactPins), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), []byte(intactLock()), 0o600))
	t.Chdir(dir)
	return dir, git
}

// TestRun_Pins drives the dispatch through the pins subcommand, which reads
// the files from the working directory and asks git what it tracks there.
// The env cases plant what a committed env file could set to hide itself, and
// each case variant names a file a case-insensitive filesystem opens as the
// refused one. A tracked env file at the root is the shared commits job's to
// refuse, so the gate refuses the one below it.
func TestRun_Pins(t *testing.T) {
	dir, git := intactCheckout(t)
	gitRun := func(args ...string) {
		t.Helper()
		require.NoError(t, exec.Command(git, append([]string{"-C", dir}, args...)...).Run()) // #nosec G204 -- the test runs the git PATH names in its own temporary repository
	}
	pins := func(t *testing.T, wantCode int, wantIn string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		assert.Equal(t, wantCode, run(t.Context(), []string{"pins"}, strings.NewReader(""), &stdout, &stderr))
		if wantIn == "" {
			assert.Empty(t, stderr.String())
			return
		}
		assert.Contains(t, stderr.String(), wantIn)
	}

	t.Run("an intact checkout holds", func(t *testing.T) { pins(t, 0, "") })

	nested := filepath.Join(dir, "docs", ".env")
	require.NoError(t, os.MkdirAll(filepath.Dir(nested), 0o700))
	require.NoError(t, os.WriteFile(nested, []byte("GIT_INDEX_FILE=.git/no-such-index\n"), 0o600))
	t.Run("an untracked env file is the contributor's own", func(t *testing.T) { pins(t, 0, "") })

	gitRun("add", "--force", "docs/.env")
	t.Run("a tracked env file below the root exits 1 and names it", func(t *testing.T) { pins(t, 1, `"docs/.env" is tracked`) })
	t.Run("a tracked env file that sets GIT_INDEX_FILE still exits 1", func(t *testing.T) {
		t.Setenv("GIT_INDEX_FILE", filepath.Join(dir, ".git", "no-such-index"))
		pins(t, 1, `"docs/.env" is tracked`)
	})
	gitRun("rm", "--cached", "--quiet", "docs/.env")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "docs")))

	upper := filepath.Join(dir, "docs", ".ENV.LOCAL")
	require.NoError(t, os.MkdirAll(filepath.Dir(upper), 0o700))
	require.NoError(t, os.WriteFile(upper, []byte("PROBE=1\n"), 0o600))
	gitRun("add", "--force", "docs/.ENV.LOCAL")
	t.Run("a tracked case variant of an env file exits 1 and names it", func(t *testing.T) {
		pins(t, 1, `"docs/.ENV.LOCAL" is tracked, and Bun loads a file of that name`)
	})
	gitRun("rm", "--cached", "--quiet", "docs/.ENV.LOCAL")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "docs")))

	config := filepath.Join(dir, ".github", "ActionLint.YAML")
	require.NoError(t, os.MkdirAll(filepath.Dir(config), 0o700))
	require.NoError(t, os.WriteFile(config, []byte("paths: {}\n"), 0o600))
	gitRun("add", "--force", ".github/ActionLint.YAML")
	t.Run("a tracked actionlint config exits 1 and names it", func(t *testing.T) {
		pins(t, 1, `".github/ActionLint.YAML" is an actionlint config`)
	})
	gitRun("rm", "--cached", "--quiet", ".github/ActionLint.YAML")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".github")))

	t.Run("missing files are an error", func(t *testing.T) {
		require.NoError(t, os.Rename(filepath.Join(dir, lockPath), filepath.Join(dir, lockPath+".aside")))
		t.Cleanup(func() {
			require.NoError(t, os.Rename(filepath.Join(dir, lockPath+".aside"), filepath.Join(dir, lockPath)))
		})
		pins(t, 2, "gate pins:")
	})

	secondTaskfile := filepath.Join(dir, "Taskfile.yaml")
	require.NoError(t, os.WriteFile(secondTaskfile, []byte("version: '3'\n"), 0o600))
	gitRun("add", "--force", "Taskfile.yaml")
	t.Run("a tracked config the gate names no tool to read exits 1 and names it", func(t *testing.T) {
		pins(t, 1, `"Taskfile.yaml" is a Task config`)
	})
	gitRun("rm", "--cached", "--quiet", "Taskfile.yaml")
	require.NoError(t, os.Remove(secondTaskfile))

	workflow := filepath.Join(dir, ".github", "workflows", "ci.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(workflow), 0o700))
	require.NoError(t, os.WriteFile(workflow, []byte("jobs: {} # zizmor: ignore[excessive-permissions]\n"), 0o600))
	gitRun("add", "--force", ".github/workflows/ci.yaml")
	t.Run("a workflow a row would skip exits 1 and names both reasons", func(t *testing.T) {
		pins(t, 1, `".github/workflows/ci.yaml" is a workflow named .yaml`)
		pins(t, 1, `".github/workflows/ci.yaml" carries an inline zizmor ignore comment`)
	})
	gitRun("rm", "--cached", "--quiet", ".github/workflows/ci.yaml")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".github")))

	tampered := []byte(intactLock()[:len(intactLock())-1])
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), bytes.ReplaceAll(tampered, []byte("github-attestations"), []byte("none")), 0o600))
	t.Run("a finding exits 1 and prints it", func(t *testing.T) { pins(t, 1, "attests its releases") })
}

// TestRun_Walk drives the dispatch through the three rows that walk the tree,
// each against the stand-in for its tool, in a checkout that tracks its files
// through real git.
func TestRun_Walk(t *testing.T) {
	dir, git := intactCheckout(t)
	workflow := filepath.Join(dir, ".github", "workflows", "ci.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(workflow), 0o700))
	require.NoError(t, os.WriteFile(workflow, []byte("on: push\n"), 0o600))
	script := filepath.Join(dir, "scripts", "build.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, taploConfig), []byte("include = [\"**/*.toml\"]\n"), 0o600))
	add := []string{"-C", dir, "add", pinsPath, bunfig, taploConfig, ".github/workflows/ci.yml", "scripts/build.sh"}
	require.NoError(t, exec.Command(git, add...).Run())
	walkRow := func(t *testing.T, args []string, wantCode int, wantOut, wantErr string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		assert.Equal(t, wantCode, run(t.Context(), args, strings.NewReader(""), &stdout, &stderr), "stderr: %s", stderr.String())
		assert.Contains(t, stdout.String(), wantOut)
		assert.Contains(t, stderr.String(), wantErr)
	}
	taplo := filepath.Join(fakeProgramDir(t, "taplo"), programName("taplo"))
	actionlint := filepath.Join(fakeProgramDir(t, "actionlint"), programName("actionlint"))
	zizmor := filepath.Join(fakeProgramDir(t, "zizmor"), programName("zizmor"))
	shellcheck := filepath.Join(fakeProgramDir(t, "shellcheck"), programName("shellcheck"))
	packages := []string{"packages", "-platforms", "linux/amd64 windows/amd64", "-tags", "", "-release", "linux/amd64"}

	t.Run("toml prints what taplo checked", func(t *testing.T) {
		walkRow(t, []string{"toml", taplo}, 0, "taplo checked 3 TOML files: .taplo.toml, bunfig.toml, mise.toml", "")
	})
	t.Run("toml fails on a tracked file taplo never saw", func(t *testing.T) {
		aside := filepath.Join(dir, bunfig+".aside")
		require.NoError(t, os.Rename(filepath.Join(dir, bunfig), aside))
		t.Cleanup(func() { require.NoError(t, os.Rename(aside, filepath.Join(dir, bunfig))) })
		walkRow(t, []string{"toml", taplo}, 1, "", "taplo did not check bunfig.toml, which the toml row handed it")
	})
	t.Run("actionlint prints what it linted", func(t *testing.T) {
		walkRow(t, []string{"actionlint", actionlint, "shellcheck-path"}, 0, "actionlint checked 1 workflow file: .github/workflows/ci.yml", "")
	})
	t.Run("scripts prints what ShellCheck checked", func(t *testing.T) {
		walkRow(t, []string{"scripts", shellcheck}, 0, "shellcheck checked 1 script file: scripts/build.sh", "")
	})
	t.Run("format refuses to start bunx before the install holds Prettier", func(t *testing.T) {
		t.Setenv("PATH", fakeProgramDir(t, "bun"))
		walkRow(t, []string{"format"}, 2, "", "gate format: prettier is not installed in this checkout: run bun install --frozen-lockfile")
	})
	t.Run("format prints how many files Prettier checked", func(t *testing.T) {
		installPrettier(t, dir)
		t.Cleanup(func() { require.NoError(t, os.RemoveAll(filepath.Join(dir, "node_modules"))) })
		t.Setenv("PATH", fakeProgramDir(t, "bun"))
		walkRow(t, []string{"format"}, 0, "prettier checked ", "")
	})
	t.Run("format with no bun on PATH is an error", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		walkRow(t, []string{"format"}, 2, "", "gate format: finding bun on PATH")
	})
	t.Run("zizmor runs online when the gh on PATH answers", func(t *testing.T) {
		t.Setenv("PATH", fakeProgramDir(t, "gh")+string(os.PathListSeparator)+filepath.Dir(git))
		t.Setenv("GATE_FAKE_GH", "")
		walkRow(t, []string{"zizmor", zizmor}, 0, "zizmor ran online, since gh auth token answered, and completed 1 file: .github/workflows/ci.yml", "")
	})
	t.Run("zizmor runs offline with no gh on PATH", func(t *testing.T) {
		t.Setenv("PATH", filepath.Dir(git))
		if found, err := exec.LookPath("gh"); err == nil {
			t.Skipf("gh sits beside git at %s, so this case would start it", found)
		}
		walkRow(t, []string{"zizmor", zizmor}, 0, "zizmor ran offline", "")
	})
	t.Run("packages prints the count go list gives", func(t *testing.T) {
		t.Setenv("PATH", fakeProgramDir(t, "go")+string(os.PathListSeparator)+filepath.Dir(git))
		walkRow(t, packages, 0, "go list ./... matched 2 packages, and the rows read linux/amd64 windows/amd64, each with no build tags", "")
	})
	t.Run("packages with no go on PATH is an error", func(t *testing.T) {
		t.Setenv("PATH", filepath.Dir(git))
		if found, err := exec.LookPath("go"); err == nil {
			t.Skipf("go sits beside git at %s, so this case would start it", found)
		}
		walkRow(t, packages, 2, "", "gate packages: finding go on PATH")
	})
	t.Run("a row outside a repository is an error", func(t *testing.T) {
		t.Chdir(t.TempDir())
		walkRow(t, []string{"toml", taplo}, 2, "", "gate toml: listing tracked files")
		walkRow(t, []string{"actionlint", actionlint, "shellcheck-path"}, 2, "", "gate actionlint: listing tracked files")
		walkRow(t, []string{"zizmor", zizmor}, 2, "", "gate zizmor: listing tracked files")
		walkRow(t, []string{"scripts", shellcheck}, 2, "", "gate scripts: listing tracked files")
	})
}
