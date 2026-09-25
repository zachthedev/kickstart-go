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
	// The command reads the files the scan reads, with one exception below.
	// The scan reads every file below the root, tracked, untracked or ignored,
	// outside its skip list, SkipDirs and any nested work tree, and passes
	// over a file with a NUL byte in its first 8000 bytes. So git grep runs:
	//
	//   - from the top of the work tree (-C), because it searches the
	//     current directory alone;
	//   - over untracked files, ignored ones included (--untracked
	//     --no-exclude-standard);
	//   - with -I decided by that NUL test alone. --attr-source names the
	//     empty tree, core.attributesFile is cleared, and GIT_ATTR_NOSYSTEM
	//     drops the system file, so no .gitattributes line marks a file
	//     binary. A pattern in .git/info/attributes still applies. A built
	//     cmd/generate carries this marker in its string table, and both
	//     pass over it;
	//   - with the empty tree hashed from stdin. Git for Windows resolves a
	//     /dev/null path argument against the current directory, which
	//     fails below the top;
	//   - with a pathspec per skipped path and one per SkipDirs entry, at
	//     any depth for a bare name. git grep does not enter a nested work
	//     tree.
	//
	// The exception is a stray .git inside a tracked directory, such as a git
	// init run there by hand. The scan skips that directory, and git grep
	// still searches its tracked files. It fails closed: the command reads not
	// absorbed, and the generate row goes red on the smaller inventory.
	//
	// Exit 1 alone reads as absorbed. git grep exits 0 on a match and 128 on
	// an error. rev-parse exits 128 first from inside .git and outside a work
	// tree. Under errexit two things hold. rc carries git grep's status to
	// the test, so an absorbed tree reaches it. The subshell makes the whole
	// command one command, so any nonzero status, rev-parse's included, stops
	// the calling script.
	fmt.Fprintf(&b, "( top=$(git rev-parse --show-toplevel) && empty=$(git hash-object -t tree --stdin </dev/null) && "+
		"{ rc=0; GIT_ATTR_NOSYSTEM=1 git -C \"$top\" --attr-source=\"$empty\" -c core.attributesFile= "+
		"grep -q -I --untracked --no-exclude-standard -F '%s' -- %s || rc=$?; test \"$rc\" -eq 1; } )\n",
		markers.TODOMarker, excludePathspecs(entry.Path, m.Skip))
	b.WriteString("```\n")
	return []byte(b.String()), nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// excludePathspecs renders the scan's exclusions as git pathspecs, sorted and
// deduplicated. The inventory's own path and the caller's skip list are
// relative paths, so each excludes that one path. A [markers.SkipDirs] entry
// is matched the way the scan matches it: a bare name at any depth, a path
// with a slash at that path alone.
func excludePathspecs(own string, skip []string) string {
	seen := make(map[string]struct{}, len(skip)+len(markers.SkipDirs)+1)
	var specs []string
	add := func(spec string) {
		if _, ok := seen[spec]; !ok {
			seen[spec] = struct{}{}
			specs = append(specs, spec)
		}
	}
	for _, p := range slices.Concat([]string{own}, skip) {
		if p != "" {
			add(fmt.Sprintf("':!%s'", p))
		}
	}
	for _, dir := range markers.SkipDirs {
		add(skipDirPathspec(dir))
	}
	slices.Sort(specs)
	return strings.Join(specs, " ")
}

// skipDirPathspec renders one SkipDirs entry as an exclude pathspec.
func skipDirPathspec(dir string) string {
	if strings.Contains(dir, "/") {
		return fmt.Sprintf("':!%s'", dir)
	}
	return fmt.Sprintf("':(exclude,glob)**/%s/**'", dir)
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
