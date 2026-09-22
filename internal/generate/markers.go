package generate

import (
	"fmt"
	"slices"
	"strings"

	"zach.tools/go/kickstart/internal/markers"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// MarkerTable renders the project's template-marker inventory as Markdown.
// Suitable as the Generate function of an [OutputEntry].
type MarkerTable struct {
	// Root is the directory to scan, relative to the project root.
	Root string
	// ProjectName appears in the generated file's heading.
	ProjectName string
	// Skip lists paths, relative to Root, to leave out of the scan. The
	// entry's own path is always skipped; this covers anything else that
	// documents the convention rather than using it.
	Skip []string
}

// ///////////////////////////////////////////////
// MarkerTable methods
// ///////////////////////////////////////////////

// Generate returns the inventory as a Markdown document. The entry's own
// path is excluded from the scan, so the inventory never counts itself.
func (m MarkerTable) Generate(entry OutputEntry) ([]byte, error) {
	root := m.Root
	if root == "" {
		root = "."
	}
	files, err := markers.Scan(root, append([]string{entry.Path}, m.Skip...)...)
	if err != nil {
		return nil, fmt.Errorf("generate: scanning for markers: %w", err)
	}
	todo, note := markers.Totals(files)

	var b strings.Builder
	fmt.Fprintf(&b, "<!-- %s -->\n\n", GeneratedByHeader)
	fmt.Fprintf(&b, "# %s template markers\n\n", m.ProjectName)
	fmt.Fprintf(&b, "%s a clone must act on, and %s recording a deliberate\n",
		countPhrase(todo, "directive"), countPhrase(note, "note"))
	b.WriteString("divergence from template content.\n\n")

	if len(files) == 0 {
		b.WriteString("No markers remain. The template is fully absorbed.\n")
		return []byte(b.String()), nil
	}

	b.WriteString("| File | To do | Notes |\n")
	b.WriteString("| - | -: | -: |\n")
	for _, f := range files {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", codeCell(f.Path), f.TODO, f.NOTE)
	}

	b.WriteString("\nThe template is absorbed once this command exits 0:\n\n")
	b.WriteString("```\n")
	// Four things have to hold at once. git grep rather than grep -r,
	// because the compiler folds this marker into cmd/generate's string
	// table and grep -r reads it there whatever .gitignore says.
	// --untracked, because a marker in a file not yet added still has to be
	// answered. The rev-parse guard first, because git grep exits 128
	// outside a work tree, which the negation would otherwise turn into a
	// clean bill of health. And the pathspecs, because git grep has no skip
	// list of its own: without them the files documenting the convention
	// match their own example and the command can never exit 0.
	fmt.Fprintf(&b, "git rev-parse --is-inside-work-tree >/dev/null && ! git grep -q --untracked %q -- %s\n",
		markers.TODOMarker, excludePathspecs(entry.Path, m.Skip))
	b.WriteString("```\n")
	return []byte(b.String()), nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// excludePathspecs renders the scan's exclusions as git pathspecs, sorted and
// deduplicated. It reads [markers.SkipDirs] and the caller's own skip list, so
// the printed command and the scan cannot come to disagree about what counts.
func excludePathspecs(own string, skip []string) string {
	seen := make(map[string]struct{}, len(skip)+len(markers.SkipDirs)+1)
	var paths []string
	for _, p := range slices.Concat([]string{own}, skip, markers.SkipDirs) {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	slices.Sort(paths)

	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = fmt.Sprintf("':!%s'", p)
	}
	return strings.Join(quoted, " ")
}

// codeCell renders text as a code span inside a table cell. A pipe is escaped,
// because GFM reads it as a column break even inside a span, and a text
// carrying a backtick takes a double-backtick span, which is the one way to
// hold a backtick in a span.
func codeCell(text string) string {
	text = strings.ReplaceAll(text, "|", `\|`)
	if strings.Contains(text, "`") {
		return "`` " + text + " ``"
	}
	return "`" + text + "`"
}

// countPhrase renders a count with its noun, pluralized.
func countPhrase(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
