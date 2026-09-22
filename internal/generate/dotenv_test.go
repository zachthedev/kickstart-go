package generate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleEnvVars covers the shapes the renderer has to handle: prose long
// enough to wrap, a single unbreakable token, and an omitted Absent.
func sampleEnvVars() []EnvVar {
	return []EnvVar{
		{
			Name:    "EXAMPLE_CLIENT_ID",
			Purpose: "An application id the build stamps into the binary, registered at https://example.test/apps/new for each machine.",
			Example: "abcdefghijklmnopqrstuvwxyz0123",
			Absent:  "the feature it enables is unavailable, and nothing else changes",
		},
		{
			Name:    "SHORT",
			Purpose: "Brief.",
			Example: "1",
		},
	}
}

// ///////////////////////////////////////////////
// DotEnv.Generate
// ///////////////////////////////////////////////

// The template documents settings rather than supplying them, so a copy of
// it taken whole to .env must configure nothing.
func TestDotEnv_Generate_CommentsOutEveryAssignment(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: sampleEnvVars()}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	for line := range strings.SplitSeq(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		assert.NotContains(t, trimmed, "=")
	}
}

func TestDotEnv_Generate_DocumentsEveryVariable(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: sampleEnvVars()}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		"# ///// EXAMPLE_CLIENT_ID /////",
		"#EXAMPLE_CLIENT_ID=abcdefghijklmnopqrstuvwxyz0123",
		"# Unset: the feature it enables is unavailable",
		"# ///// SHORT /////",
		"#SHORT=1",
	} {
		assert.Contains(t, got, want)
	}
}

// A variable with no Absent gets no Unset paragraph rather than an empty one.
func TestDotEnv_Generate_OmitsAbsentWhenUnset(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: []EnvVar{
		{Name: "SHORT", Purpose: "Brief.", Example: "1"},
	}}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "Unset:")
}

// A Purpose carries the URL where an application is registered, and a wrap
// through the middle leaves an address that cannot be clicked or copied.
func TestDotEnv_Generate_NeverWrapsMidWord(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: sampleEnvVars(), Width: 40}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	assert.Contains(t, string(out), "https://example.test/apps/new")
}

func TestDotEnv_Generate_TemplateBanner(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: sampleEnvVars()}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "# example build settings. Copy to .env")
	assert.NotContains(t, got, GeneratedByHeader)
}

func TestDotEnv_Generate_ArtifactBanner(t *testing.T) {
	out, err := DotEnv{ProjectName: "example", Vars: sampleEnvVars()}.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, GeneratedByHeader)
	assert.NotContains(t, got, "Copy to .env")
}

func TestDotEnv_Generate_NoVars(t *testing.T) {
	out, err := DotEnv{ProjectName: "example"}.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "/////")
}

// ///////////////////////////////////////////////
// wrapWords
// ///////////////////////////////////////////////

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{name: "empty", text: "", width: 10, want: nil},
		{name: "fits", text: "one two", width: 10, want: []string{"one two"}},
		{name: "wraps at a space", text: "one two three", width: 8, want: []string{"one two", "three"}},
		{
			name:  "keeps an oversized word whole",
			text:  "a supercalifragilistic b",
			width: 5,
			want:  []string{"a", "supercalifragilistic", "b"},
		},
		{name: "collapses runs of whitespace", text: "one   two", width: 20, want: []string{"one two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, wrapWords(tt.text, tt.width))
		})
	}
}
