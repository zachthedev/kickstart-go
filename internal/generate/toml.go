package generate

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// FieldDoc documents one config field or section. ConfigDocs maps a dotted
// path (e.g., "capture.interface") to a FieldDoc and drives comment
// injection during [TOMLConfig.Generate].
type FieldDoc struct {
	// Comment is a multi-line description placed above the field.
	Comment string
	// Alternatives are extra commented-out example lines emitted below the
	// value (e.g., "# path = \"/alt/location\"").
	Alternatives []string
}

// TOMLConfig configures the TOML-from-struct-with-docs generator.
type TOMLConfig struct {
	// ProjectName appears in the generated file's banner.
	ProjectName string
	// Defaults is the struct (or map) to serialize as TOML. Typically
	// the value returned by the project's DefaultConfig function.
	Defaults any
	// Docs maps dotted field paths to [FieldDoc] entries for comment
	// injection and omitted-field scaffolding.
	Docs map[string]FieldDoc
}

// ///////////////////////////////////////////////
// TOMLConfig methods
// ///////////////////////////////////////////////

// Generate returns the formatted TOML config bytes. Suitable as the
// Generate function of an [OutputEntry]. When entry.Template is true,
// the "Auto-generated, do not edit" banner is swapped for an operator-
// facing dual-audience header.
func (c TOMLConfig) Generate(entry OutputEntry) ([]byte, error) {
	var raw bytes.Buffer
	if err := toml.NewEncoder(&raw).Encode(c.Defaults); err != nil {
		return nil, fmt.Errorf("generate: marshaling TOML: %w", err)
	}

	out := c.bannerLines(entry.Template)
	emitted := map[string]bool{}
	// seenArray records which [[array]] names already carry a banner, so the
	// second and later elements of one array repeat neither it nor the docs.
	seenArray := map[string]bool{}
	var sectionStack []string

	for line := range strings.SplitSeq(raw.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			section := strings.Trim(trimmed, "[] ")
			repeat := strings.HasPrefix(trimmed, "[[") && seenArray[section]
			seenArray[section] = seenArray[section] || strings.HasPrefix(trimmed, "[[")

			if repeat {
				// A further element of the array already introduced. Keys
				// inside it resolve against the same docs, so only the
				// separating blank line is new.
				out = append(out, "")
				out = append(out, trimmed)
				continue
			}

			c.injectOmitted(&out, sectionStack, emitted)
			sectionStack = strings.Split(section, ".")

			out = append(out, "")
			out = append(out, fmt.Sprintf("# ///// %s /////", sectionLabel(section)))
			out = append(out, "")

			if doc, ok := c.Docs[section]; ok {
				emitted[section] = true
				if doc.Comment != "" {
					for cl := range strings.SplitSeq(doc.Comment, "\n") {
						out = append(out, commentLine(cl))
					}
				}
			}
			out = append(out, trimmed)
			continue
		}

		if !strings.Contains(trimmed, "=") || strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
			continue
		}

		key := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		fullPath := key
		if len(sectionStack) > 0 {
			fullPath = strings.Join(sectionStack, ".") + "." + key
		}
		emitted[fullPath] = true

		doc, ok := c.Docs[fullPath]
		if !ok {
			out = append(out, trimmed)
			continue
		}
		if doc.Comment != "" {
			for cl := range strings.SplitSeq(doc.Comment, "\n") {
				out = append(out, commentLine(cl))
			}
		}
		out = append(out, trimmed)
		for _, alt := range doc.Alternatives {
			out = append(out, commentLine(alt))
		}
	}

	c.injectOmitted(&out, sectionStack, emitted)
	c.injectTopLevel(&out, emitted)

	result := strings.Join(out, "\n")
	result = strings.TrimRight(result, "\n") + "\n"
	return []byte(result), nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// bannerLines returns the file-header comment block. When template is
// true, the dual-audience wording replaces the "do not edit" banner
// so the repo-committed copy reads honestly to both contributors and
// operators browsing the repo. At deploy time the full banner should
// be removed with [StripLeadingBanner] so the operator's on-disk
// config has no banner at all.
func (c TOMLConfig) bannerLines(template bool) []string {
	var head []string
	if template {
		head = []string{
			fmt.Sprintf("# %s config. Defaults generated; edit this copy freely.", c.ProjectName),
			"# Contributors: update internal/config/*.go and run `go tool task generate`.",
		}
	} else {
		head = []string{"# " + GeneratedByHeader}
	}
	return append(head,
		"#",
		"# ///////////////////////////////////////////////",
		fmt.Sprintf("# %s Configuration", c.ProjectName),
		"# ///////////////////////////////////////////////",
		"",
	)
}

// injectOmitted appends commented-out entries for Docs keys that belong to
// the current section but were omitted by the encoder (typically omitempty
// fields at their zero value). Keys are sorted for deterministic output.
func (c TOMLConfig) injectOmitted(out *[]string, sectionStack []string, emitted map[string]bool) {
	if len(sectionStack) == 0 {
		return
	}
	prefix := strings.Join(sectionStack, ".") + "."

	var omitted []string
	for path := range c.Docs {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if strings.Contains(rest, ".") {
			continue
		}
		if emitted[path] {
			continue
		}
		omitted = append(omitted, path)
	}
	slices.Sort(omitted)

	for _, path := range omitted {
		doc := c.Docs[path]
		*out = append(*out, "")
		if doc.Comment != "" {
			for cl := range strings.SplitSeq(doc.Comment, "\n") {
				*out = append(*out, commentLine(cl))
			}
		}
		for _, alt := range doc.Alternatives {
			*out = append(*out, commentLine(alt))
		}
		emitted[path] = true
	}
}

// injectTopLevel appends docs for top-level keys never emitted by the
// encoder (e.g. empty maps that the encoder omits entirely).
func (c TOMLConfig) injectTopLevel(out *[]string, emitted map[string]bool) {
	var keys []string
	for path := range c.Docs {
		if strings.Contains(path, ".") || emitted[path] {
			continue
		}
		keys = append(keys, path)
	}
	slices.Sort(keys)

	for _, key := range keys {
		doc := c.Docs[key]
		label := sectionLabel(key)
		*out = append(*out, "")
		*out = append(*out, fmt.Sprintf("# ///// %s /////", label))
		*out = append(*out, "")
		if doc.Comment != "" {
			for cl := range strings.SplitSeq(doc.Comment, "\n") {
				*out = append(*out, commentLine(cl))
			}
		}
		for _, alt := range doc.Alternatives {
			*out = append(*out, commentLine(alt))
		}
		emitted[key] = true
	}
}

// commentLine formats one comment line. Empty input produces a bare "#"
// so blank comment lines don't carry trailing whitespace.
func commentLine(text string) string {
	if text == "" {
		return "#"
	}
	return "# " + text
}

// sectionLabel returns a human-readable section label by extracting the
// last dotted segment and capitalizing its first letter.
func sectionLabel(section string) string {
	parts := strings.Split(section, ".")
	last := parts[len(parts)-1]
	if last == "" {
		return ""
	}
	return strings.ToUpper(last[:1]) + last[1:]
}
