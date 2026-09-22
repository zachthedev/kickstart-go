package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ///////////////////////////////////////////////
// Helpers
// ///////////////////////////////////////////////

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	assert.Panics(t, fn)
}

func staticEntry(path, body string) OutputEntry {
	return OutputEntry{
		Path:     path,
		Generate: func(OutputEntry) ([]byte, error) { return []byte(body), nil },
	}
}

// ///////////////////////////////////////////////
// Registry.Register
// ///////////////////////////////////////////////

func TestRegistry_Register_AppendsEntry(t *testing.T) {
	r := &Registry{}
	r.Register(staticEntry("a.txt", "a"))
	require.Len(t, r.entries, 1, "entries count")
}

func TestRegistry_Register_EmptyPathPanics(t *testing.T) {
	r := &Registry{}
	assertPanics(t, func() {
		r.Register(OutputEntry{Generate: func(OutputEntry) ([]byte, error) { return nil, nil }})
	})
}

func TestRegistry_Register_NilGeneratePanics(t *testing.T) {
	r := &Registry{}
	assertPanics(t, func() {
		r.Register(OutputEntry{Path: "a.txt"})
	})
}

func TestRegistry_Register_DuplicatePathPanics(t *testing.T) {
	r := &Registry{}
	r.Register(staticEntry("a.txt", "a"))
	assertPanics(t, func() {
		r.Register(staticEntry("a.txt", "b"))
	})
}

// ///////////////////////////////////////////////
// Registry.Entries
// ///////////////////////////////////////////////

func TestRegistry_Entries_ReturnsSortedCopy(t *testing.T) {
	r := &Registry{}
	r.Register(staticEntry("z.txt", "z"))
	r.Register(staticEntry("a.txt", "a"))
	r.Register(staticEntry("m.txt", "m"))

	entries := r.Entries()
	want := []string{"a.txt", "m.txt", "z.txt"}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Path
	}
	assert.Equal(t, want, got, "Entries did not sort by path")

	entries[0].Path = "mutated"
	assert.NotEqual(t, "mutated", r.entries[0].Path, "Entries() returned a reference; caller mutation leaked")
}

// ///////////////////////////////////////////////
// Registry.Outputs
// ///////////////////////////////////////////////

func TestRegistry_Outputs_SortedForwardSlash(t *testing.T) {
	r := &Registry{}
	r.Register(staticEntry(filepath.Join("sub", "b.txt"), ""))
	r.Register(staticEntry("a.txt", ""))

	want := []string{"a.txt", "sub/b.txt"}
	assert.Equal(t, want, r.Outputs(), "Outputs()")
}

// ///////////////////////////////////////////////
// Registry.Inputs
// ///////////////////////////////////////////////

func TestRegistry_Inputs_DedupsAndSorts(t *testing.T) {
	r := &Registry{}
	r.Register(OutputEntry{
		Path:     "a.txt",
		Inputs:   []string{"src/*.go", "data/*.json"},
		Generate: func(OutputEntry) ([]byte, error) { return nil, nil },
	})
	r.Register(OutputEntry{
		Path:     "b.txt",
		Inputs:   []string{"src/*.go", "docs/*.md"},
		Generate: func(OutputEntry) ([]byte, error) { return nil, nil },
	})

	want := []string{"data/*.json", "docs/*.md", "src/*.go"}
	assert.Equal(t, want, r.Inputs(), "Inputs()")
}

// ///////////////////////////////////////////////
// Registry.InputRegexp
// ///////////////////////////////////////////////

func TestRegistry_InputRegexp_SelectsWhatTheGlobsSelect(t *testing.T) {
	r := &Registry{}
	r.Register(OutputEntry{
		Path:     "a.txt",
		Inputs:   []string{"**/*.md", "internal/buildenv/*.go", "go.mod", "data/?.json"},
		Generate: func(OutputEntry) ([]byte, error) { return nil, nil },
	})
	re, err := regexp.Compile(r.InputRegexp())
	require.NoError(t, err)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "a root-level file under a ** prefix", input: "README.md", want: true},
		{name: "a nested file under a ** prefix", input: "docs/guide.md", want: true},
		{name: "a file in the directory a lone star names", input: "internal/buildenv/env.go", want: true},
		{name: "a file one level below it", input: "internal/buildenv/sub/env.go", want: false},
		{name: "a literal pattern", input: "go.mod", want: true},
		{name: "that literal name in a subdirectory", input: "tools/go.mod", want: false},
		{name: "a name that literal is a substring of", input: "cmd/cargo.modx", want: false},
		{name: "one character under a question mark", input: "data/a.json", want: true},
		{name: "two characters under a question mark", input: "data/ab.json", want: false},
		{name: "a path no pattern selects", input: "LICENSE", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, re.MatchString(tt.input), "InputRegexp() against %q", tt.input)
		})
	}
}

func TestRegistry_InputRegexp_EmptyWhenNoEntryDeclaresAnInput(t *testing.T) {
	r := &Registry{}
	r.Register(staticEntry("a.txt", ""))

	assert.Empty(t, r.InputRegexp(), "InputRegexp()")
}

// ///////////////////////////////////////////////
// Registry.Run
// ///////////////////////////////////////////////

func TestRegistry_Run_WritesEveryOutput(t *testing.T) {
	dir := t.TempDir()
	r := &Registry{}
	r.Register(staticEntry(filepath.Join(dir, "a.txt"), "hello a"))
	r.Register(staticEntry(filepath.Join(dir, "sub", "b.txt"), "hello b"))

	require.NoError(t, r.Run())

	for _, want := range []struct {
		path, body string
	}{
		{filepath.Join(dir, "a.txt"), "hello a"},
		{filepath.Join(dir, "sub", "b.txt"), "hello b"},
	} {
		got, err := os.ReadFile(want.path)
		if !assert.NoError(t, err, "reading %s", want.path) {
			continue
		}
		assert.Equal(t, want.body, string(got))
	}
}

func TestRegistry_Run_GenerateErrorPropagates(t *testing.T) {
	r := &Registry{}
	r.Register(OutputEntry{
		Path:     "boom.txt",
		Generate: func(OutputEntry) ([]byte, error) { return nil, fmt.Errorf("kaboom") },
	})

	err := r.Run()
	require.Error(t, err, "Run error = nil")
	assert.Contains(t, err.Error(), "kaboom", "Run error")
}

func TestRegistry_Run_SubsetWritesOnlyMatchingEntries(t *testing.T) {
	dir := t.TempDir()
	r := &Registry{}
	r.Register(staticEntry(filepath.Join(dir, "a.txt"), "a"))
	r.Register(staticEntry(filepath.Join(dir, "b.txt"), "b"))
	r.Register(staticEntry(filepath.Join(dir, "c.txt"), "c"))

	require.NoError(t, r.Run(filepath.Join(dir, "a.txt"), filepath.Join(dir, "c.txt")))

	assert.FileExists(t, filepath.Join(dir, "a.txt"))
	assert.FileExists(t, filepath.Join(dir, "c.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "b.txt"))
}

func TestRegistry_Run_SubsetUnknownPathErrors(t *testing.T) {
	r := &Registry{}
	err := r.Run("missing.txt")
	assert.Error(t, err, "Run error = nil")
}
