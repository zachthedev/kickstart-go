package generate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleConfig is a minimal struct exercising nested sections, scalar
// fields, and the omitempty interaction that drives injectOmitted.
type sampleConfig struct {
	Name    string         `toml:"name"`
	Timeout int            `toml:"timeout"`
	Server  sampleServer   `toml:"server"`
	Clients map[string]any `toml:"clients,omitempty"`
}

type sampleServer struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

// arrayConfig exercises the array-of-tables path, where one [[items]] header
// introduces the section and later ones repeat it.
type arrayConfig struct {
	Title string      `toml:"title"`
	Items []arrayItem `toml:"items"`
}

type arrayItem struct {
	Name string `toml:"name"`
}

func sampleDocs() map[string]FieldDoc {
	return map[string]FieldDoc{
		"name": {
			Comment: "Human-readable project name.",
		},
		"timeout": {
			Comment:      "Request timeout in seconds.",
			Alternatives: []string{`timeout = 60`},
		},
		"server": {
			Comment: "Server bind settings.",
		},
		"server.host": {
			Comment: "Interface to bind.",
		},
		"server.port": {
			Comment: "Port to listen on.",
		},
		"clients": {
			Comment: "Known client registrations (empty by default).",
		},
	}
}

// ///////////////////////////////////////////////
// TOMLConfig.Generate
// ///////////////////////////////////////////////

func TestTOMLConfig_Generate_ArtifactBanner(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{Name: "x", Server: sampleServer{Host: "0.0.0.0", Port: 80}},
	}
	out, err := cfg.Generate(OutputEntry{}) // Template defaults to false
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		"# " + GeneratedByHeader,
		"# ///////////////////////////////////////////////",
		"# example Configuration",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "edit this copy freely")
}

// Template mode replaces the "do not edit" banner with dual-audience
// wording so the repo-committed copy reads honestly to both
// contributors and operators browsing the repo.
func TestTOMLConfig_Generate_TemplateBanner(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{Name: "x", Server: sampleServer{Host: "0.0.0.0", Port: 80}},
	}
	out, err := cfg.Generate(OutputEntry{Template: true})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		"# example config. Defaults generated; edit this copy freely.",
		"# Contributors: update internal/config/*.go and run `go tool task generate`.",
		"# example Configuration",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, GeneratedByHeader)
}

func TestTOMLConfig_Generate_InjectsFieldComments(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{Name: "x", Timeout: 30, Server: sampleServer{Host: "0.0.0.0", Port: 80}},
		Docs:        sampleDocs(),
	}
	out, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(out)
	// The encoder writes string values as TOML literal strings.
	for _, want := range []string{
		"# Human-readable project name.",
		`name = 'x'`,
		"# Request timeout in seconds.",
		"timeout = 30",
		"# timeout = 60",
		"# Interface to bind.",
		`host = '0.0.0.0'`,
		"# Port to listen on.",
		"port = 80",
	} {
		assert.Contains(t, got, want)
	}
}

// An array of tables gets one banner and one set of docs, keys inside it
// resolve against the array's own dotted path, and the array's doc does not
// reappear after the array it describes.
func TestTOMLConfig_Generate_ArrayOfTables(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults: &arrayConfig{
			Title: "t",
			Items: []arrayItem{{Name: "one"}, {Name: "two"}},
		},
		Docs: map[string]FieldDoc{
			"title":      {Comment: "The title."},
			"items":      {Comment: "The items array."},
			"items.name": {Comment: "An item name."},
		},
	}
	out, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(out)

	assert.Equal(t, 1, strings.Count(got, "# ///// Items /////"))
	assert.Equal(t, 1, strings.Count(got, "# The items array."))
	assert.Equal(t, 2, strings.Count(got, "# An item name."))
	assert.Equal(t, 2, strings.Count(got, "[[items]]"))
	// The array's doc belongs above the array, not appended after it.
	assert.Less(t, strings.LastIndex(got, "# The items array."), strings.Index(got, "[[items]]"),
		"the array doc trails the array it describes")
	assert.Contains(t, got, "# An item name.\nname = 'one'")
}

func TestTOMLConfig_Generate_SectionHeader(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{Server: sampleServer{Host: "h", Port: 1}},
		Docs:        sampleDocs(),
	}
	out, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "# ///// Server /////")
	assert.Contains(t, got, "[server]")
}

func TestTOMLConfig_Generate_InjectsOmittedTopLevelSection(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{}, // Clients is an empty map; encoder omits it
		Docs:        sampleDocs(),
	}
	out, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "# Known client registrations (empty by default).")
	assert.Contains(t, got, "# ///// Clients /////")
}

func TestTOMLConfig_Generate_Deterministic(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    &sampleConfig{Name: "x", Timeout: 1, Server: sampleServer{Host: "h", Port: 2}},
		Docs:        sampleDocs(),
	}
	first, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	for range 5 {
		next, err := cfg.Generate(OutputEntry{})
		require.NoError(t, err)
		assert.Equal(t, string(first), string(next))
	}
}

func TestTOMLConfig_Generate_EncodingError(t *testing.T) {
	cfg := TOMLConfig{
		ProjectName: "example",
		Defaults:    make(chan int), // not marshalable
	}
	_, err := cfg.Generate(OutputEntry{})
	assert.Error(t, err, "Generate error = nil")
}
