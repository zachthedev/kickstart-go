package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"zach.tools/go/kickstart/internal/generate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRegistry returns a cleanup to restore the package Default.
func resetRegistry(t *testing.T) {
	t.Helper()
	saved := generate.Default
	t.Cleanup(func() { generate.Default = saved })
	generate.Default = &generate.Registry{}
}

// runInDir changes working directory to dir for the test's lifetime and
// writes a placeholder go.mod so ensureProjectRoot treats it as the root.
func runInDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644))
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// ///////////////////////////////////////////////
// dispatch
// ///////////////////////////////////////////////

// Each literal Inputs entry becomes a term in lefthook's pre-commit skip
// regex, which is the list's only consumer. An entry naming a path that is
// not in the tree matches no staged file, so the generate hook stops firing
// for the edits it exists to catch.
func TestInit_LiteralInputsExist(t *testing.T) {
	patterns := generate.Default.Inputs()
	require.NotEmpty(t, patterns, "no input patterns registered, so this test checks nothing")

	for _, pattern := range patterns {
		if strings.ContainsAny(pattern, "*?[") {
			continue // a glob names a set, not one path
		}
		t.Run(pattern, func(t *testing.T) {
			_, err := os.Stat(filepath.Join("..", "..", pattern))
			assert.NoError(t, err, "Inputs entry %q names a path that is not in the tree", pattern)
		})
	}
}

func TestDispatch_NoArgsRuns(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()
	runInDir(t, dir)

	generate.Default.Register(generate.OutputEntry{
		Path:     "out.txt",
		Generate: func(generate.OutputEntry) ([]byte, error) { return []byte("ok"), nil },
	})

	require.NoError(t, dispatch(nil, io.Discard))
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data), "out.txt")
}

func TestDispatch_Run(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()
	runInDir(t, dir)

	generate.Default.Register(generate.OutputEntry{
		Path:     "ran.txt",
		Generate: func(generate.OutputEntry) ([]byte, error) { return []byte("y"), nil },
	})

	require.NoError(t, dispatch([]string{"run"}, io.Discard))
	assert.FileExists(t, filepath.Join(dir, "ran.txt"))
}

func TestDispatch_RunSubsetWritesOnlyListedEntries(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()
	runInDir(t, dir)

	generate.Default.Register(generate.OutputEntry{
		Path:     "wanted.txt",
		Generate: func(generate.OutputEntry) ([]byte, error) { return []byte("w"), nil },
	})
	generate.Default.Register(generate.OutputEntry{
		Path:     "skipped.txt",
		Generate: func(generate.OutputEntry) ([]byte, error) { return []byte("s"), nil },
	})

	require.NoError(t, dispatch([]string{"run", "wanted.txt"}, io.Discard))
	assert.FileExists(t, filepath.Join(dir, "wanted.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "skipped.txt"))
}

func TestDispatch_RunSubsetMultiplePaths(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()
	runInDir(t, dir)

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		generate.Default.Register(generate.OutputEntry{
			Path:     name,
			Generate: func(generate.OutputEntry) ([]byte, error) { return []byte(name), nil },
		})
	}

	require.NoError(t, dispatch([]string{"run", "a.txt", "c.txt"}, io.Discard))
	assert.FileExists(t, filepath.Join(dir, "a.txt"))
	assert.FileExists(t, filepath.Join(dir, "c.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "b.txt"))
}

func TestDispatch_RunSubsetUnknownPathErrors(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()
	runInDir(t, dir)

	assert.Error(t, dispatch([]string{"run", "missing.txt"}, io.Discard), "dispatch run missing.txt error = nil")
}

func TestDispatch_ListOutputs(t *testing.T) {
	resetRegistry(t)
	generate.Default.Register(generate.OutputEntry{
		Path:     "config.toml",
		Generate: func(generate.OutputEntry) ([]byte, error) { return nil, nil },
	})

	var buf bytes.Buffer
	require.NoError(t, dispatch([]string{"list", "outputs"}, &buf))
	got := buf.String()
	assert.Contains(t, got, "config.toml", "output")
}

func TestDispatch_ListInputs(t *testing.T) {
	resetRegistry(t)
	generate.Default.Register(generate.OutputEntry{
		Path:     "out.txt",
		Inputs:   []string{"src/*.go"},
		Generate: func(generate.OutputEntry) ([]byte, error) { return nil, nil },
	})

	var buf bytes.Buffer
	require.NoError(t, dispatch([]string{"list", "inputs"}, &buf))
	got := buf.String()
	assert.Contains(t, got, "src/*.go", "output")
}

func TestDispatch_ListInputRegexp(t *testing.T) {
	resetRegistry(t)
	generate.Default.Register(generate.OutputEntry{
		Path:     "out.txt",
		Inputs:   []string{"**/*.md"},
		Generate: func(generate.OutputEntry) ([]byte, error) { return nil, nil },
	})

	var buf bytes.Buffer
	require.NoError(t, dispatch([]string{"list", "input-regexp"}, &buf))
	re, err := regexp.Compile(strings.TrimSpace(buf.String()))
	require.NoError(t, err)
	assert.True(t, re.MatchString("README.md"), "printed regexp matching a root-level README.md")
}

func TestDispatch_ListRequiresTarget(t *testing.T) {
	resetRegistry(t)
	assert.Error(t, dispatch([]string{"list"}, io.Discard), "dispatch list error = nil")
}

func TestDispatch_ListUnknownTarget(t *testing.T) {
	resetRegistry(t)
	assert.Error(t, dispatch([]string{"list", "bogus"}, io.Discard), "dispatch list bogus error = nil")
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	resetRegistry(t)
	err := dispatch([]string{"bogus"}, io.Discard)
	assert.Error(t, err, "dispatch bogus error = nil")
}

func TestDispatch_Help(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	require.NoError(t, dispatch([]string{"help"}, &buf))
	assert.Contains(t, buf.String(), "Usage:")
}
