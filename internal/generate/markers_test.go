package generate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zach.tools/go/kickstart/internal/markers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markerTree lays out a sandbox and returns its root.
func markerTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	return root
}

// ///////////////////////////////////////////////
// MarkerTable.Generate
// ///////////////////////////////////////////////

func TestMarkerTable_Generate(t *testing.T) {
	root := markerTree(t, map[string]string{
		"a.go":  "// " + markers.TODOMarker + " one\n// " + markers.TODOMarker + " two\n",
		"b.md":  markers.NOTEMarker + " why\n",
		"c.txt": "nothing\n",
	})

	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	got := string(out)

	for _, want := range []string{
		"<!-- " + GeneratedByHeader + " -->",
		"# example template markers",
		"2 directives",
		"1 note",
		"| `a.go` | 2 | 0 |",
		"| `b.md` | 0 | 1 |",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "c.txt")
}

// The inventory must not count itself, or every regeneration would report a
// marker that only exists because the last regeneration wrote it.
func TestMarkerTable_Generate_ExcludesItsOwnOutput(t *testing.T) {
	root := markerTree(t, map[string]string{
		"a.go":       "// " + markers.TODOMarker + "\n",
		"MARKERS.md": markers.TODOMarker + " left over from a previous run\n",
	})
	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "`MARKERS.md`")
}

// Skip covers anything that documents the convention rather than using it.
func TestMarkerTable_Generate_HonorsSkip(t *testing.T) {
	root := markerTree(t, map[string]string{
		"a.go":          "// " + markers.TODOMarker + "\n",
		"convention.md": markers.TODOMarker + " shown as an example\n",
	})
	out, err := MarkerTable{Root: root, ProjectName: "example", Skip: []string{"convention.md"}}.
		Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	// The backtick form is a table cell. A skipped path still appears bare in
	// the check's pathspecs, which is the point of skipping it.
	assert.NotContains(t, string(out), "`convention.md`", "a skipped path reached the inventory")
	assert.Contains(t, string(out), "`a.go`")
}

func TestMarkerTable_Generate_EmptyTree(t *testing.T) {
	root := markerTree(t, map[string]string{"clean.go": "package main\n"})
	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "No markers remain")
	assert.NotContains(t, got, "| File |")
}

// The printed command must be one that cannot report a clean bill of health
// it did not earn. It searches untracked files, it reads no built binary, and
// it refuses to answer outside a git work tree rather than negating git's
// exit 128 into success.
func TestMarkerTable_Generate_PrintsARunnableCheck(t *testing.T) {
	root := markerTree(t, map[string]string{"a.go": "// " + markers.TODOMarker + "\n"})
	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		"git rev-parse --is-inside-work-tree",
		"! git grep -q --untracked",
	} {
		assert.Contains(t, got, want)
	}
	// grep -r reads the marker out of a built binary, and grep -c never
	// returns a bare 0.
	assert.NotContains(t, got, "grep -rq")
	assert.NotContains(t, got, "grep -c")
}

// TestMarkerTable_Generate_ExcludesEverythingTheScanSkips is what keeps the
// printed check satisfiable. git grep carries no skip list, so without a
// pathspec for each path the scan leaves out, the files documenting the marker
// match their own example and the command cannot exit 0 however many real
// directives a clone clears.
func TestMarkerTable_Generate_ExcludesEverythingTheScanSkips(t *testing.T) {
	root := markerTree(t, map[string]string{"a.go": "// " + markers.TODOMarker + "\n"})
	table := MarkerTable{Root: root, ProjectName: "example", Skip: []string{"docs/convention.md"}}
	out, err := table.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "':!MARKERS.md'", "the inventory quotes the marker, so it excludes itself")
	assert.Contains(t, got, "':!docs/convention.md'", "a caller's own skip reaches the check")
	for _, dir := range markers.SkipDirs {
		assert.Contains(t, got, "':!"+dir+"'", "SkipDirs entry %q is missing from the check", dir)
	}
}

func TestExcludePathspecs_SortsAndDeduplicates(t *testing.T) {
	got := excludePathspecs("MARKERS.md", []string{"vendor", "MARKERS.md", "", "docs/x.md"})

	assert.NotContains(t, got, "':!'", "an empty path is not a pathspec")
	assert.Equal(t, 1, strings.Count(got, "':!MARKERS.md'"), "a repeated path appears once")
	assert.Equal(t, 1, strings.Count(got, "':!vendor'"), "a path already in SkipDirs appears once")

	fields := strings.Fields(got)
	assert.True(t, slices.IsSorted(fields), "pathspecs are sorted, so the output is stable: %v", fields)
}

func TestMarkerTable_Generate_MissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := (MarkerTable{Root: missing}).Generate(OutputEntry{Path: "MARKERS.md"})
	assert.Error(t, err, "Generate on a missing root returned nil error")
}

func TestMarkerTable_Generate_DefaultsRootToCwd(t *testing.T) {
	t.Chdir(markerTree(t, map[string]string{"a.go": "// " + markers.TODOMarker + "\n"}))
	out, err := MarkerTable{ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	assert.Contains(t, string(out), "| `a.go` | 1 | 0 |")
}

// ///////////////////////////////////////////////
// codeCell
// ///////////////////////////////////////////////

func TestCodeCell(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "plain path", text: "docs/a.md", want: "`docs/a.md`"},
		{name: "pipe escaped", text: "docs/a|b.md", want: "`docs/a\\|b.md`"},
		{name: "backtick takes a double span", text: "docs/a`b.md", want: "`` docs/a`b.md ``"},
		{name: "both", text: "a`|b", want: "`` a`\\|b ``"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, codeCell(tt.text))
		})
	}
}

// ///////////////////////////////////////////////
// countPhrase
// ///////////////////////////////////////////////

func TestCountPhrase(t *testing.T) {
	tests := []struct {
		name string
		n    int
		noun string
		want string
	}{
		{name: "zero", n: 0, noun: "note", want: "0 notes"},
		{name: "one", n: 1, noun: "note", want: "1 note"},
		{name: "many", n: 4, noun: "directive", want: "4 directives"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, countPhrase(tt.n, tt.noun))
		})
	}
}
