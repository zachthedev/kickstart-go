package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invisibleCodePoints are the default-ignorable code points a reason can be
// made of, one per source the property draws from: format characters,
// Other_Default_Ignorable_Code_Point, variation selectors and tag characters.
// Each alone looks like no reason at all, and each must be refused as one.
var invisibleCodePoints = []struct {
	name string
	r    rune
}{
	{name: "a soft hyphen", r: 0x00AD},
	{name: "a combining grapheme joiner", r: 0x034F},
	{name: "an Arabic letter mark", r: 0x061C},
	{name: "a Hangul choseong filler", r: 0x115F},
	{name: "a Hangul jungseong filler", r: 0x1160},
	{name: "a Khmer inherent vowel", r: 0x17B4},
	{name: "a Mongolian vowel separator", r: 0x180E},
	{name: "a Mongolian free variation selector", r: 0x180B},
	{name: "a zero-width space", r: 0x200B},
	{name: "a zero-width non-joiner", r: 0x200C},
	{name: "a zero-width joiner", r: 0x200D},
	{name: "a left-to-right mark", r: 0x200E},
	{name: "a left-to-right embedding", r: 0x202A},
	{name: "a word joiner", r: 0x2060},
	{name: "a left-to-right isolate", r: 0x2066},
	{name: "a Hangul filler", r: 0x3164},
	{name: "a variation selector", r: 0xFE0F},
	{name: "a zero-width no-break space", r: 0xFEFF},
	{name: "a halfwidth Hangul filler", r: 0xFFA0},
	{name: "a musical beam format character", r: 0x1D173},
	{name: "a language tag", r: 0xE0001},
	{name: "a tag space", r: 0xE0020},
	{name: "a supplementary variation selector", r: 0xE0100},
}

// blankCodePoints render as nothing a reader can take for a reason and are not
// default-ignorable, so a reason made of one alone must be refused as well:
// a blank braille cell, a lone combining mark, a private-use point, and a
// Hangul filler, which is a letter and default-ignorable at once.
var blankCodePoints = []struct {
	name string
	r    rune
}{
	{name: "a blank braille cell", r: 0x2800},
	{name: "a lone combining acute accent", r: 0x0301},
	{name: "a private-use point", r: 0xE000},
	{name: "a Hangul filler, a letter the drop removes first", r: 0x3164},
}

// Each case is one comment as the parser hands it over, markers included. The
// check must refuse every waiver golangci-lint 2.13.2 honors that nolintlint
// never reads or cannot hold to a named rule, and pass every waiver the
// linters check and every comment that is no waiver at all.
func TestWaiverRefusal(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		wantIn string
	}{
		{name: "a named, reasoned nolint", text: "//nolint:errcheck // the close error is the write's"},
		{name: "two linters named with a reason", text: "//nolint:errcheck,unconvert // why"},
		{name: "a nolint with no reason, which nolintlint refuses", text: "//nolint:errcheck"},
		{name: "a leading space, which nolintlint refuses and the gate refuses too", text: "// nolint:errcheck // why", wantIn: "which golangci-lint honors and nolintlint never reads"},
		{name: "an extra slash", text: "///nolint", wantIn: "which golangci-lint honors and nolintlint never reads"},
		{name: "an extra slash naming a linter", text: "///nolint:errcheck // why", wantIn: "nolintlint never reads"},
		{name: "slashes and spaces", text: "// / nolint:errcheck // why", wantIn: "nolintlint never reads"},
		{name: "a bare nolint", text: "//nolint", wantIn: "which golangci-lint reads as waiving every linter"},
		{name: "a bare nolint with a reason", text: "//nolint // why", wantIn: "waiving every linter"},
		{name: "a list after a space in place of the colon", text: "//nolint gosec // why", wantIn: "waiving every linter"},
		{name: "all", text: "//nolint:all // why", wantIn: "waiving every linter"},
		{name: "all behind a space", text: "//nolint: all // why", wantIn: "waiving every linter"},
		{name: "all in capitals", text: "//nolint:ALL // why", wantIn: "waiving every linter"},
		{name: "all after a named linter", text: "//nolint:errcheck,all // why", wantIn: "waiving every linter"},
		{name: "all in mixed case after a space", text: "//nolint:errcheck, All // why", wantIn: "waiving every linter"},
		{name: "a list opening with all", text: "//nolint:allx // why", wantIn: "waiving every linter"},
		{name: "nolintlint in the list", text: "//nolint:errcheck,nolintlint", wantIn: "which silences nolintlint's own finding on the line"},
		{name: "nolintlint in capitals", text: "//nolint:NoLintLint // why", wantIn: "silences nolintlint's own finding"},
		{name: "gosec", text: "//nolint:gosec // why", wantIn: "which waives every gosec rule on the line. Name the rule instead: // #nosec G<nnn> -- reason"},
		{name: "gosec in capitals behind a space", text: "//nolint:errcheck, GoSec // why", wantIn: "waives every gosec rule"},
		{name: "gosec after the reason marker, which the filter never reads as a linter", text: "//nolint:errcheck // gosec"},
		{name: "a rule-named nosec with a reason", text: "// #nosec G306 -- generated files are not secrets"},
		{name: "lint:ignore", text: "//lint:ignore U1000 kept for the next release", wantIn: "a staticcheck directive the unused linter honors with no reason"},
		{name: "lint:file-ignore", text: "//lint:file-ignore SA1019 the old API", wantIn: "a staticcheck directive"},
		{name: "lint:ignore behind a space, in capitals", text: "// LINT:IGNORE U1000", wantIn: "a staticcheck directive"},
		{name: "a block comment, which the filter never reads", text: "/* nolint:gosec */"},
		{name: "prose mentioning nolint", text: "// The nolint filter strips every leading slash."},
		{name: "a word starting with nolint", text: "//nolintish"},
		{name: "a tab before nolint, which the filter never reads", text: "//\tnolint:gosec"},
		{name: "gosec:disable with a rule and a reason, which gosec honors", text: "//gosec:disable G204 -- the caller names the program", wantIn: "which spells gosec:disable, and a gosec waiver takes one form. Write // #nosec G<nnn> -- reason"},
		{name: "a bare gosec:disable", text: "//gosec:disable", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable behind a space", text: "// gosec:disable G204 -- why", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable in capitals", text: "//GoSec:DISABLE G204 -- why", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable behind an extra slash and a tab", text: "///\tgosec:disable G204 -- why", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable with a word run on", text: "//gosec:disabled G204 -- why", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable in a block comment", text: "/* gosec:disable G204 -- why */", wantIn: "which spells gosec:disable"},
		{name: "gosec:disable on a block comment's second line", text: "/*\r\n\t * gosec:disable G204 -- why\n */", wantIn: "which spells gosec:disable"},
		{name: "prose naming gosec:disable mid-sentence", text: "// The gate refuses gosec:disable in every spelling."},
		{name: "a comment opening with gosec", text: "// gosec reads a second spelling of its waiver."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waiverRefusal(tt.text)
			if tt.wantIn == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tt.wantIn)
		})
	}
}

func TestGoWaiverFindings(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"internal/rt/rt.go":  "package rt\n\nimport \"os\"\n\n// Write writes.\nfunc Write() error {\n\treturn os.WriteFile(\"x\", nil, 0o644) //nolint:gosec // why\n}\n",
		"internal/rt/ok.go":  "package rt\n\n// the text \"//nolint:all\" inside a string is no comment\nconst s = \"//nolint:all\"\n",
		"internal/rt/bad.go": "package rt\n\n//lint:ignore U1000 later\nfunc unused() {}\n\nfunc used() {\n\t_ = 1 ///nolint\n}\n",
		"broken.go":          "package\n//nolint:all\n",
		"docs/notes.md":      "//nolint:all\n",
		"UPPER.GO":           "package upper\n\nvar x = 1 //nolint:ALL // why\n",
		"internal/rt/run.go": "package rt\n\nimport \"os/exec\"\n\n// Run runs name.\nfunc Run(name string) error {\n\treturn exec.Command(name).Run() //gosec:disable G204 -- the caller names it\n}\n",
	}
	for name, content := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, []byte(content), 0o600))
	}
	tracked := []string{"internal/rt/rt.go", "internal/rt/ok.go", "internal/rt/bad.go", "broken.go", "docs/notes.md", "UPPER.GO", "deleted.go", "internal/rt/run.go"}
	found, err := goWaiverFindings(root, tracked)
	require.NoError(t, err)
	require.Len(t, found, 5, "findings: %q", found)
	assert.Contains(t, found[0], `internal/rt/rt.go:7 carries "//nolint:gosec // why", which waives every gosec rule`)
	assert.Contains(t, found[1], `internal/rt/bad.go:3 carries "//lint:ignore U1000 later", a staticcheck directive`)
	assert.Contains(t, found[2], `internal/rt/bad.go:7 carries "///nolint", which golangci-lint honors and nolintlint never reads`)
	assert.Contains(t, found[3], `UPPER.GO:3 carries "//nolint:ALL // why", which golangci-lint reads as waiving every linter`)
	assert.Contains(t, found[4], `internal/rt/run.go:7 carries "//gosec:disable G204 -- the caller names it", which spells gosec:disable, and a gosec waiver takes one form`)

	t.Run("a tracked path that is a directory is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "dir.go"), 0o700))
		_, err := goWaiverFindings(dir, []string{"dir.go"})
		assert.ErrorContains(t, err, "reading dir.go")
	})
	t.Run("an invisible reason on each waiver form, found where the file carries it", func(t *testing.T) {
		dir := t.TempDir()
		content := "package rt\n\nimport \"os\"\n\n// Write writes.\nfunc Write() error {\n" +
			"\t_ = os.Remove(\"x\") //nolint:errcheck // \U0000200B\n" +
			"\t// #nosec G306 -- \U000000AD\n" +
			"\treturn os.WriteFile(\"x\", nil, 0o644)\n}\n\n" +
			"// Read reads, with its reason on the line after the waiver.\nfunc Read() error {\n" +
			"\t// #nosec G304 --\n\t// the path is the caller's own\n" +
			"\t_, err := os.ReadFile(\"x\")\n\treturn err\n}\n\n" +
			"// Remove removes the file, its waiver on the doc comment's second line.\n" +
			"// #nosec G304 -- \U00002060\n" +
			"func Remove() error {\n\treturn os.Remove(\"x\")\n}\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "rt.go"), []byte(content), 0o600))
		found, err := goWaiverFindings(dir, []string{"rt.go"})
		require.NoError(t, err)
		require.Len(t, found, 3, "findings: %q", found)
		assert.Contains(t, found[2], fmt.Sprintf("rt.go:21 carries %q, whose reason holds no letter", "#nosec G304 -- "+string(rune(0x2060))+"\n"))
		assert.Contains(t, found[0], `rt.go:7 carries "//nolint:errcheck // \u200b", whose reason holds no letter or number once its invisible code points are dropped`)
		assert.Contains(t, found[1], `rt.go:8 carries "#nosec G306 -- \u00ad\n", whose reason holds no letter`)
	})
}

func TestDefaultIgnorable(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want bool
	}{
		{name: "a letter", r: 'a'},
		{name: "a space", r: ' '},
		{name: "a no-break space, which is white space", r: 0x00A0},
		{name: "an ogham space mark, which is white space", r: 0x1680},
		{name: "an interlinear annotation anchor, which the property leaves out", r: 0xFFF9},
		{name: "an Egyptian hieroglyph format character, which the property leaves out", r: 0x13430},
		{name: "a prepended concatenation mark, which the property leaves out", r: 0x0600},
	}
	for _, point := range invisibleCodePoints {
		tests = append(tests, struct {
			name string
			r    rune
			want bool
		}{name: point.name, r: point.r, want: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, defaultIgnorable(tt.r), "U+%04X", tt.r)
		})
	}
}

// Each case is a reason as a waiver carries it, and the check must read it as
// empty exactly when nothing visible is left once the default-ignorable code
// points and the white space around them are gone.
func TestInvisibleReason(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "a reason", input: " the close error is the write's"},
		{name: "nothing", input: "", want: true},
		{name: "spaces", input: " \t ", want: true},
		{name: "a no-break space, which the linters trim too", input: " \U000000A0", want: true},
		{name: "invisible code points around a visible one", input: "\U0000200Bx\U000000AD"},
		{name: "a dash alone, which holds no letter or number", input: "-", want: true},
		{name: "a number alone", input: "7"},
		{name: "several invisible code points", input: " \U0000200B\U0000200C\U00002060\U0000FEFF ", want: true},
	}
	for _, point := range slices.Concat(invisibleCodePoints, blankCodePoints) {
		tests = append(tests, struct {
			name  string
			input string
			want  bool
		}{name: point.name + " alone", input: " " + string(point.r), want: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, invisibleReason(tt.input))
		})
	}
}

// Each case is a comment as the parser hands it over, and waiverRefusal must
// refuse a nolint whose reason is invisible, one case per code point, and pass
// one whose reason reads.
func TestWaiverRefusal_InvisibleReason(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		wantIn string
	}{
		{name: "a reason that reads", text: "//nolint:errcheck // the close error is the write's"},
		{name: "a reason with an invisible code point inside it", text: "//nolint:errcheck // the close\U0000200B error"},
		{name: "a reason marker with nothing after it", text: "//nolint:errcheck //", wantIn: invisibleReasonWhy},
		{name: "no reason marker, which nolintlint refuses itself", text: "//nolint:errcheck"},
	}
	for _, point := range slices.Concat(invisibleCodePoints, blankCodePoints) {
		tests = append(tests, struct {
			name   string
			text   string
			wantIn string
		}{name: point.name, text: "//nolint:errcheck // " + string(point.r), wantIn: invisibleReasonWhy})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waiverRefusal(tt.text)
			if tt.wantIn == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.wantIn, got)
		})
	}
}

// Each case is a comment group's text, markers dropped, as gosec reads it. The
// check must refuse a #nosec whose reason after -- is invisible, one case per
// code point, read through the extra dashes gosec trims and across the group's
// lines, and pass one whose reason reads or that gives none.
func TestNosecRefusal(t *testing.T) {
	tests := []struct {
		name   string
		group  string
		refuse bool
	}{
		{name: "a reason that reads", group: "#nosec G306 -- generated files are not secrets\n"},
		{name: "a reason on the next line of the group", group: "#nosec G304 --\nthe path is the caller's own\n"},
		{name: "no reason marker, which gosec refuses itself", group: "#nosec G306\n"},
		{name: "no nosec at all", group: "the prose names -- and \U0000200B\n"},
		{name: "a reason marker with nothing after it", group: "#nosec G306 --\n", refuse: true},
		{name: "extra dashes before an invisible reason", group: "#nosec G306 --- \U0000200B\n", refuse: true},
		{name: "a nosec after prose on its own line", group: "The path is fixed.\n#nosec G304 -- \U00002060\n", refuse: true},
	}
	for _, point := range slices.Concat(invisibleCodePoints, blankCodePoints) {
		tests = append(tests, struct {
			name   string
			group  string
			refuse bool
		}{name: point.name, group: "#nosec G306 -- " + string(point.r) + "\n", refuse: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, waiver := nosecRefusal(tt.group)
			if !tt.refuse {
				assert.Empty(t, got)
				assert.Empty(t, waiver)
				return
			}
			assert.Equal(t, invisibleReasonWhy, got)
			assert.True(t, strings.HasPrefix(waiver, nosec), "the waiver opens at #nosec: %q", waiver)
		})
	}
}
