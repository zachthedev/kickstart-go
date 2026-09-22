package generate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sampleJSON struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Enabled bool   `json:"enabled"`
}

// ///////////////////////////////////////////////
// JSONConfig.Generate
// ///////////////////////////////////////////////

func TestJSONConfig_Generate_DefaultIndent(t *testing.T) {
	cfg := JSONConfig{
		Defaults: sampleJSON{Name: "x", Count: 5, Enabled: true},
	}
	data, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, "  \"name\": \"x\"")
	assert.True(t, strings.HasSuffix(got, "\n"), "output missing trailing newline")
	var round sampleJSON
	require.NoError(t, json.Unmarshal(data, &round))
	assert.Equal(t, 5, round.Count, "round-trip Count")
}

func TestJSONConfig_Generate_CustomIndent(t *testing.T) {
	cfg := JSONConfig{
		Defaults: sampleJSON{Name: "x"},
		Indent:   "\t",
	}
	data, err := cfg.Generate(OutputEntry{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "\t\"name\"")
}

func TestJSONConfig_Generate_EncodingError(t *testing.T) {
	cfg := JSONConfig{
		Defaults: make(chan int), // channels are not JSON-encodable
	}
	_, err := cfg.Generate(OutputEntry{})
	assert.Error(t, err, "Generate error = nil")
}
