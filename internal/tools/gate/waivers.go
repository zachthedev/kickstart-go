package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// gosecDisable is the second spelling of gosec's waiver. gosec 2.28.0 reads
	// it where a // comment opens with it, alone or before a space, under the
	// same rule and reason settings as #nosec (analyzer.go, findNoSecDirective).
	gosecDisable = "gosec:disable"
	// nosec is gosec's waiver, whose reason follows reasonMarker.
	nosec = "#nosec"
	// reasonMarker opens the reason of a #nosec waiver.
	reasonMarker = "--"
	// invisibleReasonWhy is what every refusal of an invisible reason says.
	invisibleReasonWhy = "whose reason is empty once its invisible code points are dropped, and the linter reads those as a reason. Write the reason in visible text"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// nolintDirective matches what golangci-lint 2.13.2's nolint filter reads as
// a directive once it strips every leading slash and space from a comment
// (pkg/result/processors/nolint_filter.go).
var nolintDirective = regexp.MustCompile(`^nolint( |:|$)`)

// ///////////////////////////////////////////////
// The check
// ///////////////////////////////////////////////

// goWaiverFindings refuses every inline waiver in a tracked Go file that no
// linter checks, and every gosec waiver outside its one form. nolintlint holds
// a //nolint to named linters and a reason, and gosec holds a #nosec to named
// rules and a reason, but golangci-lint also honors forms nolintlint never
// reads, and gosec reads a second spelling of its own. A file that does not
// parse passes, since the build fails on it first, and so does a tracked file
// missing from the work tree.
func goWaiverFindings(root string, tracked []string) ([]string, error) {
	var found []string
	for _, name := range tracked {
		if fold(path.Ext(name)) != fold(".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, name, data, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if why := waiverRefusal(comment.Text); why != "" {
					found = append(found, fmt.Sprintf("%s:%d carries %q, %s", name, files.Position(comment.Pos()).Line, comment.Text, why))
				}
			}
			if why, waiver := nosecRefusal(group.Text()); why != "" {
				found = append(found, fmt.Sprintf("%s:%d carries %q, %s", name, nosecLine(files, group), waiver, why))
			}
		}
	}
	return found, nil
}

// waiverRefusal says why the gate refuses one comment, given with its
// markers, or returns "" for a comment it does not refuse:
//
//   - a nolint directive behind extra slashes or a space, which the filter
//     honors and nolintlint never reads
//   - a directive the filter reads as waiving every linter: a bare nolint, one
//     followed by a space, and a list naming all anywhere, in any case or
//     spacing, where nolintlint catches only a leading lowercase all
//   - a list naming nolintlint, which silences nolintlint's own finding on the
//     line
//   - a list naming gosec, which waives every gosec rule on the line, where
//     `// #nosec G<nnn> -- reason` names the one it waives
//   - //lint:ignore and //lint:file-ignore, staticcheck's directives, which the
//     unused linter honors with no reason
//   - gosec:disable behind any comment marker, in any case or spacing, so a
//     gosec waiver reads // #nosec G<nnn> -- reason and nothing else. gosec
//     honors one spelling of it, and the rest look like a waiver and waive
//     nothing
//   - a nolint whose reason is invisible, which nolintlint's
//     require-explanation accepts when it is made of default-ignorable code
//     points alone, such as U+200B or U+00AD
func waiverRefusal(text string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimLeft(text, "/* \t\r\n")), gosecDisable) {
		return "which spells gosec:disable, and a gosec waiver takes one form. Write // #nosec G<nnn> -- reason"
	}
	if !strings.HasPrefix(text, "//") {
		return ""
	}
	bare := strings.ToLower(strings.TrimLeft(text, "/ \t"))
	if strings.HasPrefix(bare, "lint:ignore") || strings.HasPrefix(bare, "lint:file-ignore") {
		return "a staticcheck directive the unused linter honors with no reason. Fix the finding, or waive it with //nolint:<linter> // reason"
	}
	body := strings.TrimLeft(text, "/ ")
	if !nolintDirective.MatchString(body) {
		return ""
	}
	if !strings.HasPrefix(text, "//nolint") {
		return "which golangci-lint honors and nolintlint never reads, because of what stands before nolint. Write //nolint:<linter> // reason"
	}
	linters, every := nolintLinters(body)
	switch {
	case every:
		return "which golangci-lint reads as waiving every linter. Name the one it waives: //nolint:<linter> // reason"
	case slices.Contains(linters, "nolintlint"):
		return "which silences nolintlint's own finding on the line, so nothing checks the waiver. Take nolintlint out of the list"
	case slices.Contains(linters, "gosec"):
		return "which waives every gosec rule on the line. Name the rule instead: // #nosec G<nnn> -- reason"
	}
	if _, reason, found := strings.Cut(body, "//"); found && invisibleReason(reason) {
		return invisibleReasonWhy
	}
	return ""
}

// nosecRefusal says why the gate refuses a comment group, as its text reads
// with the markers dropped, and returns the waiver from #nosec to the group's
// end, or "" twice for a group it does not refuse. gosec reads a #nosec from
// the group's text and its reason from after the first --, less any further
// dashes, and nosec-require-justification accepts a reason made of
// default-ignorable code points alone. gosec reads #nosec at the start of a
// line alone, so matching it anywhere refuses more, never less.
func nosecRefusal(group string) (why, waiver string) {
	at := strings.Index(group, nosec)
	if at < 0 {
		return "", ""
	}
	_, reason, found := strings.Cut(group[at+len(nosec):], reasonMarker)
	if found && invisibleReason(strings.TrimLeft(reason, "-")) {
		return invisibleReasonWhy, group[at:]
	}
	return "", ""
}

// nosecLine is the line of the first comment in group that carries #nosec,
// where a reader looks for the waiver, or the group's first line.
func nosecLine(files *token.FileSet, group *ast.CommentGroup) int {
	for _, comment := range group.List {
		if strings.Contains(comment.Text, nosec) {
			return files.Position(comment.Pos()).Line
		}
	}
	return files.Position(group.Pos()).Line
}

// invisibleReason reports whether reason reads empty once every
// default-ignorable code point is dropped and the white space around it is
// trimmed. A reason made of those code points alone looks empty on screen and
// passes every linter's check for a reason.
func invisibleReason(reason string) bool {
	visible := strings.Map(func(r rune) rune {
		if defaultIgnorable(r) {
			return -1
		}
		return r
	}, reason)
	return strings.TrimSpace(visible) == ""
}

// defaultIgnorable reports whether r has Unicode's Default_Ignorable_Code_Point
// property, derived from the property tables the unicode package carries the
// way DerivedCoreProperties.txt derives it: Other_Default_Ignorable_Code_Point,
// format characters and variation selectors, less white space, the
// interlinear annotation and Egyptian hieroglyph format characters, and the
// prepended concatenation marks.
func defaultIgnorable(r rune) bool {
	switch {
	case unicode.Is(unicode.White_Space, r), unicode.Is(unicode.Prepended_Concatenation_Mark, r):
		return false
	case r >= 0xFFF9 && r <= 0xFFFB, r >= 0x13430 && r <= 0x13440:
		return false
	}
	return unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r)
}

// nolintLinters reads a directive's list the way golangci-lint's nolint
// filter does, lowercased and trimmed, and reports whether the filter reads
// it as every linter: a directive that does not open nolint:, one whose list
// opens with all, and one naming all anywhere.
func nolintLinters(body string) ([]string, bool) {
	if strings.HasPrefix(body, "nolint:all") || !strings.HasPrefix(body, "nolint:") {
		return nil, true
	}
	list, _, _ := strings.Cut(strings.TrimPrefix(body, "nolint:"), "//")
	var linters []string
	for item := range strings.SplitSeq(list, ",") {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "all" {
			return nil, true
		}
		linters = append(linters, name)
	}
	return linters, false
}
