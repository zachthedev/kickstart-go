package markers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree lays out a sandbox with the given relative paths and contents.
func writeTree(t *testing.T, files map[string]string) string {
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
// Scan
// ///////////////////////////////////////////////

func TestScan_CountsBothMarkers(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":      "// " + TODOMarker + " one\n// " + TODOMarker + " two\n",
		"b.md":      "text " + NOTEMarker + " here\n",
		"c.go":      "nothing to see\n",
		"sub/d.yml": TODOMarker + " x\n" + NOTEMarker + " y\n",
		"unrelated": "TODO: not scoped\nNOTE: not scoped\n",
	})

	got, err := Scan(root)
	require.NoError(t, err)

	want := []File{
		{Path: "a.go", TODO: 2, NOTE: 0},
		{Path: "b.md", TODO: 0, NOTE: 1},
		{Path: "sub/d.yml", TODO: 1, NOTE: 1},
	}
	assert.Equal(t, want, got)
}

// A file with no marker never appears, so an absorbed clone gets an empty
// inventory rather than a page of zeroes.
func TestScan_OmitsFilesWithNoMarkers(t *testing.T) {
	root := writeTree(t, map[string]string{"clean.go": "package main\n"})
	got, err := Scan(root)
	require.NoError(t, err)
	assert.Empty(t, got, "Scan on a clean tree")
}

func TestScan_SkipsSkipDirs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"keep.go":                 TODOMarker + "\n",
		"dist/built.go":           TODOMarker + "\n",
		"vendor/dep/dep.go":       TODOMarker + "\n",
		"node_modules/m/index.js": TODOMarker + "\n",
		".claude/settings.json":   TODOMarker + "\n",
	})
	got, err := Scan(root)
	require.NoError(t, err)
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{".claude/settings.json", "keep.go"}, paths, "Scan paths")
}

func TestScan_SkipsNamedPaths(t *testing.T) {
	root := writeTree(t, map[string]string{
		"keep.go":    TODOMarker + "\n",
		"MARKERS.md": TODOMarker + "\n",
	})
	got, err := Scan(root, "MARKERS.md")
	require.NoError(t, err)
	require.Len(t, got, 1, "Scan with a skip")
	assert.Equal(t, "keep.go", got[0].Path)
}

func TestScan_SkipsSymbolicLinks(t *testing.T) {
	root := writeTree(t, map[string]string{"target.md": TODOMarker + "\n"})
	if err := os.Symlink("target.md", filepath.Join(root, "link.md")); err != nil {
		t.Skipf("this host cannot create a symbolic link: %v", err)
	}
	got, err := Scan(root)
	require.NoError(t, err)
	require.Len(t, got, 1, "the target counts once and the link not at all")
	assert.Equal(t, "target.md", got[0].Path)
}

func TestScan_SkipsBinaryFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"text.go":  TODOMarker + "\n",
		"blob.bin": TODOMarker + "\x00 padding\n",
	})
	got, err := Scan(root)
	require.NoError(t, err)
	require.Len(t, got, 1, "only the text file carries a marker")
	assert.Equal(t, "text.go", got[0].Path)
}

func TestScan_SortsByPath(t *testing.T) {
	root := writeTree(t, map[string]string{
		"z.go":     TODOMarker + "\n",
		"a.go":     TODOMarker + "\n",
		"m/mid.go": TODOMarker + "\n",
	})
	got, err := Scan(root)
	require.NoError(t, err)
	for i := 1; i < len(got); i++ {
		assert.Less(t, got[i-1].Path, got[i].Path, "Scan is not sorted")
	}
}

func TestScan_MissingRoot(t *testing.T) {
	_, err := Scan(filepath.Join(t.TempDir(), "absent"))
	assert.Error(t, err, "Scan of a missing root returned nil error")
}

// ///////////////////////////////////////////////
// Totals
// ///////////////////////////////////////////////

func TestTotals(t *testing.T) {
	files := []File{
		{Path: "a", TODO: 2, NOTE: 1},
		{Path: "b", TODO: 3, NOTE: 0},
	}
	todo, note := Totals(files)
	assert.Equal(t, 5, todo, "Totals todo")
	assert.Equal(t, 1, note, "Totals note")
}

func TestTotals_Empty(t *testing.T) {
	todo, note := Totals(nil)
	assert.Zero(t, todo, "Totals(nil) todo")
	assert.Zero(t, note, "Totals(nil) note")
}

// ///////////////////////////////////////////////
// Markers
// ///////////////////////////////////////////////

// The two markers share one scope so a single grep finds both.
func TestTODOMarker_SharesScopeWithNote(t *testing.T) {
	// The scope is spelled in two pieces so this assertion does not become a
	// marker the scanner counts.
	scope := "(kick" + "start):"
	assert.Equal(t, "TODO"+scope, TODOMarker)
	assert.Equal(t, "NOTE"+scope, NOTEMarker)
	require.True(t, strings.HasPrefix(TODOMarker, "TODO"), "TODOMarker = %q", TODOMarker)
	require.True(t, strings.HasPrefix(NOTEMarker, "NOTE"), "NOTEMarker = %q", NOTEMarker)
	assert.Equal(t, strings.TrimPrefix(NOTEMarker, "NOTE"), strings.TrimPrefix(TODOMarker, "TODO"))
}
