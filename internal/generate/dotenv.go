package generate

import (
	"fmt"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// EnvVar documents one build-time setting for [DotEnv].
//
// The package that owns a schema must not import a generator to describe
// itself, so this type is separate and cmd/generate copies field by field.
type EnvVar struct {
	// Name is the environment variable, spelled as Taskfile.yml reads it.
	Name string
	// Purpose says what the build does with the value.
	Purpose string
	// Example is a value of the right shape, shown in a comment.
	Example string
	// Absent says what a build without the variable does instead.
	Absent string
}

// DotEnv generates a commented `.env` template from a list of build
// settings. Suitable as the Generate function of an [OutputEntry].
type DotEnv struct {
	// ProjectName appears in the generated file's banner.
	ProjectName string
	// Vars are the settings to document, in the order they appear.
	Vars []EnvVar
	// Width is the column prose wraps at. Defaults to 74.
	Width int
}

// ///////////////////////////////////////////////
// DotEnv methods
// ///////////////////////////////////////////////

// Generate returns the template bytes. Every assignment it writes is
// commented out, because the file documents settings rather than supplying
// them: a copy of it taken whole to `.env` must configure nothing, and every
// default must stay in force until a contributor uncomments a line.
func (d DotEnv) Generate(entry OutputEntry) ([]byte, error) {
	width := d.Width
	if width <= 0 {
		width = 74
	}

	var b strings.Builder
	if entry.Template {
		fmt.Fprintf(&b, "# %s build settings. Copy to .env and uncomment what you need.\n", d.ProjectName)
		b.WriteString("# Contributors: update internal/buildenv and run `go tool task generate`.\n")
	} else {
		fmt.Fprintf(&b, "# %s\n", GeneratedByHeader)
	}
	b.WriteString("#\n")
	b.WriteString("# Every assignment below is commented out on purpose. This file documents\n")
	b.WriteString("# the settings; it does not supply them. Copy it whole to .env and nothing\n")
	b.WriteString("# is configured, so every default below stays in force.\n")

	for _, v := range d.Vars {
		b.WriteString("\n")
		fmt.Fprintf(&b, "# ///// %s /////\n\n", v.Name)
		for _, line := range wrapWords(v.Purpose, width) {
			b.WriteString(commentLine(line))
			b.WriteString("\n")
		}
		if v.Absent != "" {
			b.WriteString("#\n")
			for _, line := range wrapWords("Unset: "+v.Absent+".", width) {
				b.WriteString(commentLine(line))
				b.WriteString("\n")
			}
		}
		fmt.Fprintf(&b, "#%s=%s\n", v.Name, v.Example)
	}

	return []byte(b.String()), nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// wrapWords breaks text into lines no longer than width, splitting only at
// spaces. A word longer than width gets its own line rather than being cut,
// because a URL wrapped through the middle cannot be clicked or copied.
func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var (
		lines []string
		line  = words[0]
	)
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(lines, line)
}
