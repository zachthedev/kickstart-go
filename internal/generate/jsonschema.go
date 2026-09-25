package generate

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// JSONSchema emits a JSON Schema document reflected from a Go struct type.
// Use this when a project publishes a schema for validation or editor
// autocomplete (e.g. a manifest.schema.json consumed by client tooling).
//
// The struct's JSON tags plus any jsonschema tags (see invopop/jsonschema
// docs) drive the generated schema. Set ID/Title/Description to populate
// the top-level metadata fields.
type JSONSchema struct {
	// Target is a pointer to the struct type to reflect. Example:
	// &manifest.Manifest{}.
	Target any
	// ID is the top-level "$id": a canonical URL identifying the schema.
	ID string
	// Title is the human-readable schema title.
	Title string
	// Description is the longer schema description.
	Description string
	// Indent overrides the default two-space indentation.
	Indent string
}

// ///////////////////////////////////////////////
// JSONSchema methods
// ///////////////////////////////////////////////

// Generate reflects Target into a JSON Schema and returns the marshaled
// bytes with a trailing newline. When entry.Template is true the
// auto-generated $comment field is omitted, so the schema reads as
// belonging to whoever deploys it rather than flagged as build output.
func (s JSONSchema) Generate(entry OutputEntry) ([]byte, error) {
	if s.Target == nil {
		return nil, fmt.Errorf("generate: JSONSchema Target is nil")
	}
	r := &jsonschema.Reflector{}
	schema := r.Reflect(s.Target)
	if !entry.Template {
		schema.Comments = GeneratedByHeader
	}
	if s.ID != "" {
		schema.ID = jsonschema.ID(s.ID)
	}
	if s.Title != "" {
		schema.Title = s.Title
	}
	if s.Description != "" {
		schema.Description = s.Description
	}
	indent := s.Indent
	if indent == "" {
		indent = "  "
	}
	data, err := json.MarshalIndent(schema, "", indent)
	if err != nil {
		return nil, fmt.Errorf("generate: marshaling JSON Schema: %w", err)
	}
	return append(data, '\n'), nil
}
