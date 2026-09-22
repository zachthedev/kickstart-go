// Package generate provides a registry-based code-generation system.
//
// Each generator is an [OutputEntry] declaring the file it produces, the
// input patterns it depends on, and the function that produces its bytes.
// The [Registry] collects these entries and provides list/run helpers used
// by cmd/generate and the pre-commit hook:
//
//   - Entries are declared once (typically in init functions of
//     internal/generate/*.go) and registered to the package-level
//     [Default] registry.
//   - cmd/generate run invokes [Registry.Run] to write every registered
//     output.
//   - cmd/generate list outputs prints every registered path so the
//     pre-commit hook can git-add them after generation.
//   - cmd/generate list inputs prints every registered pattern so the
//     pre-commit hook can skip generation when nothing relevant changed.
//
// Format-specific helpers (see [TOMLConfig]) plug into the same registry
// as Generate functions; add more formats alongside toml.go as needed.
package generate

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// OutputEntry declares a generated file: its output path, the input
// patterns it depends on, and the function that produces its bytes.
type OutputEntry struct {
	// Path is the output file path, relative to the project root.
	Path string
	// Inputs are glob patterns this output depends on. The pre-commit
	// hook uses these to skip generation when no staged file matches.
	Inputs []string
	// Template marks this output as a hand-editable starting point rather
	// than a terminal build artifact. Format-specific generators (see
	// TOMLConfig, JSONSchema) read it to suppress the "Auto-generated, do
	// not edit" banner.
	Template bool
	// Generate produces the file's content bytes. The entry is passed
	// so generators can read envelope policy like Template without
	// requiring duplicate fields on every format config.
	Generate func(OutputEntry) ([]byte, error)
}

// Registry holds a set of [OutputEntry] values.
type Registry struct {
	entries []OutputEntry
}

// ///////////////////////////////////////////////
// Registry methods
// ///////////////////////////////////////////////

// Register appends an entry. Panics on duplicate Path.
func (r *Registry) Register(e OutputEntry) {
	if e.Path == "" {
		panic("generate: OutputEntry.Path must not be empty")
	}
	if e.Generate == nil {
		panic(fmt.Sprintf("generate: OutputEntry.Generate is nil for %q", e.Path))
	}
	for _, existing := range r.entries {
		if existing.Path == e.Path {
			panic(fmt.Sprintf("generate: duplicate OutputEntry path %q", e.Path))
		}
	}
	r.entries = append(r.entries, e)
}

// Entries returns a copy of all registered entries, sorted by path.
func (r *Registry) Entries() []OutputEntry {
	out := make([]OutputEntry, len(r.entries))
	copy(out, r.entries)
	slices.SortFunc(out, func(a, b OutputEntry) int { return cmp.Compare(a.Path, b.Path) })
	return out
}

// Outputs returns every registered output path, sorted.
func (r *Registry) Outputs() []string {
	out := make([]string, len(r.entries))
	for i, e := range r.entries {
		out[i] = filepath.ToSlash(e.Path)
	}
	slices.Sort(out)
	return out
}

// Inputs returns every unique input pattern across all entries, sorted.
func (r *Registry) Inputs() []string {
	seen := map[string]struct{}{}
	var patterns []string
	for _, e := range r.entries {
		for _, p := range e.Inputs {
			norm := filepath.ToSlash(p)
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			patterns = append(patterns, norm)
		}
	}
	slices.Sort(patterns)
	return patterns
}

// InputRegexp returns one anchored alternation matching every path the
// registered input patterns select, or "" when no entry declares an input.
// The pre-commit hook feeds it to grep -E against the staged path list, so
// the output stays inside POSIX extended syntax: capturing groups and
// bracket expressions only.
func (r *Registry) InputRegexp() string {
	patterns := r.Inputs()
	alternatives := make([]string, len(patterns))
	for i, p := range patterns {
		alternatives[i] = globRegexp(p)
	}
	return strings.Join(alternatives, "|")
}

// Run writes registered outputs to disk. With no arguments, Run writes
// every registered entry. With one or more paths, Run writes only the
// entries whose Path matches; an unknown path returns an error.
func (r *Registry) Run(paths ...string) error {
	if len(paths) == 0 {
		for _, e := range r.Entries() {
			if err := r.runEntry(e); err != nil {
				return err
			}
		}
		return nil
	}
	byPath := make(map[string]OutputEntry, len(r.entries))
	for _, e := range r.entries {
		byPath[e.Path] = e
	}
	for _, p := range paths {
		e, ok := byPath[p]
		if !ok {
			return fmt.Errorf("generate: no entry registered for path %q", p)
		}
		if err := r.runEntry(e); err != nil {
			return err
		}
	}
	return nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// globRegexp converts one slash-separated glob into an anchored extended
// regular expression. Three wildcards are recognized and nothing else: a "**"
// segment spans any number of path segments including none, so "**/*.md"
// selects a root-level README.md as well as a nested one; a "*" spans any run
// of characters inside one segment; a "?" spans exactly one. Every other
// character is matched literally, so a bracket expression in a pattern selects
// only a path containing those brackets.
func globRegexp(pattern string) string {
	segments := strings.Split(pattern, "/")
	var b strings.Builder
	b.WriteByte('^')
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case segment == "**" && last:
			b.WriteString(".*")
		case segment == "**":
			b.WriteString("([^/]+/)*")
		default:
			b.WriteString(segmentRegexp(segment))
			if !last {
				b.WriteByte('/')
			}
		}
	}
	b.WriteByte('$')
	return b.String()
}

// segmentRegexp converts the wildcards inside one path segment, quoting
// everything between them. Both wildcards stop at a slash, so a pattern
// naming a directory cannot reach into a subdirectory of it.
func segmentRegexp(segment string) string {
	var b strings.Builder
	literal := 0
	for i := range len(segment) {
		var class string
		switch segment[i] {
		case '*':
			class = "[^/]*"
		case '?':
			class = "[^/]"
		default:
			continue
		}
		b.WriteString(regexp.QuoteMeta(segment[literal:i]))
		b.WriteString(class)
		literal = i + 1
	}
	b.WriteString(regexp.QuoteMeta(segment[literal:]))
	return b.String()
}

// runEntry executes one entry and writes its output to disk.
func (r *Registry) runEntry(e OutputEntry) error {
	data, err := e.Generate(e)
	if err != nil {
		return fmt.Errorf("generate: generating %s: %w", e.Path, err)
	}
	if dir := filepath.Dir(e.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("generate: creating parent dir for %s: %w", e.Path, err)
		}
	}
	if err := os.WriteFile(e.Path, data, 0o644); err != nil { //nolint:gosec // generated files are not secrets
		return fmt.Errorf("generate: writing %s: %w", e.Path, err)
	}
	return nil
}
