package generate

import (
	"encoding/json"
	"fmt"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// JSONConfig emits an indented JSON document from any marshalable Go value.
// JSON has no native comment syntax, so unlike [TOMLConfig] there is no
// Docs field. If a project needs documented JSON, emit a sidecar
// .schema.json (via [JSONSchema]) or a companion .md file alongside.
type JSONConfig struct {
	// ProjectName is accepted for symmetry with [TOMLConfig] but not
	// emitted into the output since JSON cannot carry banner comments.
	// Useful for log lines or error messages only.
	ProjectName string
	// Defaults is the struct or map to serialize.
	Defaults any
	// Indent overrides the default two-space indentation. Use "\t" for
	// tabs, "" for compact single-line output.
	Indent string
}

// ///////////////////////////////////////////////
// JSONConfig methods
// ///////////////////////////////////////////////

// Generate returns the JSON bytes, ending in a single trailing newline.
// JSON has no comment syntax, so the OutputEntry is accepted for
// signature compatibility but the Template flag is ignored.
func (c JSONConfig) Generate(_ OutputEntry) ([]byte, error) {
	indent := c.Indent
	if indent == "" {
		indent = "  "
	}
	data, err := json.MarshalIndent(c.Defaults, "", indent)
	if err != nil {
		return nil, fmt.Errorf("generate: marshaling JSON: %w", err)
	}
	return append(data, '\n'), nil
}
