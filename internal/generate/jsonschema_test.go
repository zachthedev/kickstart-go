package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sampleManifest struct {
	Name       string   `json:"name"                 jsonschema:"required"`
	Tags       []string `json:"tags,omitempty"`
	MaxWorkers int      `json:"max_workers,omitempty"`
}

// ///////////////////////////////////////////////
// JSONSchema.Generate
// ///////////////////////////////////////////////

func TestJSONSchema_Generate_PopulatesMetadata(t *testing.T) {
	s := JSONSchema{
		Target:      &sampleManifest{},
		ID:          "https://example.com/sample.json",
		Title:       "Sample Manifest",
		Description: "A test manifest for validation.",
	}
	data, err := s.Generate(OutputEntry{})
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "https://example.com/sample.json", parsed["$id"], "$id")
	assert.Equal(t, "Sample Manifest", parsed["title"], "title")
	assert.Equal(t, "A test manifest for validation.", parsed["description"], "description")
}

func TestJSONSchema_Generate_ReflectsFields(t *testing.T) {
	s := JSONSchema{Target: &sampleManifest{}}
	data, err := s.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(data)
	for _, want := range []string{`"name"`, `"tags"`, `"max_workers"`} {
		assert.Contains(t, got, want)
	}
}

func TestJSONSchema_Generate_TrailingNewline(t *testing.T) {
	s := JSONSchema{Target: &sampleManifest{}}
	data, err := s.Generate(OutputEntry{})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "\n"), "output missing trailing newline")
}

func TestJSONSchema_Generate_CustomIndent(t *testing.T) {
	s := JSONSchema{Target: &sampleManifest{}, Indent: "\t"}
	data, err := s.Generate(OutputEntry{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "\t\"")
}

func TestJSONSchema_Generate_NilTargetErrors(t *testing.T) {
	s := JSONSchema{}
	_, err := s.Generate(OutputEntry{})
	assert.Error(t, err, "Generate error = nil")
}

// Template mode omits the $comment field so the on-disk schema reads
// as a hand-editable artifact, not a build output.
func TestJSONSchema_Generate_TemplateOmitsComment(t *testing.T) {
	s := JSONSchema{Target: &sampleManifest{}}

	artifact, err := s.Generate(OutputEntry{})
	require.NoError(t, err)
	tpl, err := s.Generate(OutputEntry{Template: true})
	require.NoError(t, err)

	var artifactParsed, tplParsed map[string]any
	require.NoError(t, json.Unmarshal(artifact, &artifactParsed))
	require.NoError(t, json.Unmarshal(tpl, &tplParsed))
	assert.Equal(t, GeneratedByHeader, artifactParsed["$comment"], "artifact $comment")
	assert.NotContains(t, tplParsed, "$comment", "a Template output carries no do-not-edit comment")
}
