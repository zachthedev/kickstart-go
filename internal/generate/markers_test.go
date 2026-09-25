package generate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zach.tools/go/kickstart/internal/gittest"
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
// it did not earn, and it reads the files the scan reads. It runs from the top
// of the work tree, it reads untracked and ignored files, its binary test
// ignores .gitattributes, and only git grep's exit 1 reads as absorbed. rc
// carries that status to the test under errexit, and the subshell stops a
// calling script with errexit on any other status.
func TestMarkerTable_Generate_PrintsARunnableCheck(t *testing.T) {
	root := markerTree(t, map[string]string{"a.go": "// " + markers.TODOMarker + "\n"})
	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		"( top=$(git rev-parse --show-toplevel) && empty=$(git hash-object -t tree --stdin </dev/null) && ",
		`{ rc=0; GIT_ATTR_NOSYSTEM=1 git -C "$top" --attr-source="$empty" -c core.attributesFile= `,
		"grep -q -I --untracked --no-exclude-standard -F '" + markers.TODOMarker + "'",
		`|| rc=$?; test "$rc" -eq 1; } )`,
	} {
		assert.Contains(t, got, want)
	}
	// grep -r reads the marker out of a built binary, grep -c never returns
	// a bare 0, a negated git grep turns exit 128 into success, and a bare
	// test $? never runs under errexit once git grep exits 1.
	for _, unwanted := range []string{"grep -rq", "grep -c", "! git grep", "test $? -eq 1"} {
		assert.NotContains(t, got, unwanted)
	}
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
		assert.Contains(t, got, skipDirPathspec(dir), "SkipDirs entry %q is missing from the check", dir)
	}
}

func TestExcludePathspecs_SortsAndDeduplicates(t *testing.T) {
	got := excludePathspecs("MARKERS.md", []string{"MARKERS.md", "", "docs/x.md"})

	assert.NotContains(t, got, "':!'", "an empty path is not a pathspec")
	assert.Equal(t, 1, strings.Count(got, "':!MARKERS.md'"), "a repeated path appears once")
	assert.Contains(t, got, "':!docs/x.md'")

	fields := strings.Fields(got)
	assert.True(t, slices.IsSorted(fields), "pathspecs are sorted, so the output is stable: %v", fields)
}

// The scan skips a bare SkipDirs name at any depth and a path with a slash at
// that path alone, so the pathspec has to say the same thing to git grep.
func TestSkipDirPathspec(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"node_modules", "':(exclude,glob)**/node_modules/**'"},
		{".git", "':(exclude,glob)**/.git/**'"},
		{".claude/worktrees", "':!.claude/worktrees'"},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			assert.Equal(t, tt.want, skipDirPathspec(tt.dir))
		})
	}
}

// printedCheck returns the command a generated inventory prints. It depends on
// the inventory's path and the skip list alone, so one tree with a marker in it
// yields the command for every case.
func printedCheck(t *testing.T) string {
	t.Helper()
	root := markerTree(t, map[string]string{"a.go": "// " + markers.TODOMarker + "\n"})
	out, err := MarkerTable{Root: root, ProjectName: "example"}.Generate(OutputEntry{Path: "MARKERS.md"})
	require.NoError(t, err)
	fenced := strings.Split(string(out), "```\n")
	require.Len(t, fenced, 3, "one fenced block holds the command")
	return strings.TrimSpace(fenced[1])
}

// The printed command runs in a real work tree, the way a clone runs it: sh
// from the top, a subdirectory or .git, the user's git configuration masked.
// Its exit status is the thing under test, so the process is real. Each case
// is a file class the scan reads or skips, or a shell running with errexit,
// and the command has to answer as the inventory does.
func TestMarkerTable_Generate_PrintedCheckRuns(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is not on PATH: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	gittest.Isolate(t)
	// The printed command passes --attr-source, which git 2.41 added. An older
	// git refuses the option, so the skip names that cause and no case reads
	// red for it.
	empty, err := exec.Command("git", "hash-object", "-t", "tree", "--stdin").Output()
	require.NoError(t, err, "hashing the empty tree")
	if out, err := exec.Command("git", "--attr-source="+strings.TrimSpace(string(empty)), "version").CombinedOutput(); err != nil { // #nosec G204 -- the argument is git's own hash
		t.Skipf("the printed check needs git 2.41 or newer for --attr-source: %v\n%s", err, out)
	}
	check := printedCheck(t)
	marker := "// " + markers.TODOMarker + "\n"
	done := "// done\n"

	tests := []struct {
		name    string
		files   map[string]string
		dir     string
		noRepo  bool
		errexit bool
		want    int
	}{
		{"a live marker, from the top", map[string]string{"a.go": marker}, ".", false, false, 1},
		{"a live marker, from a subdirectory holding none", map[string]string{"a.go": marker}, "sub", false, false, 1},
		{"a live marker, from inside .git", map[string]string{"a.go": marker}, ".git", false, false, 128},
		{"outside a work tree", map[string]string{"a.go": marker}, ".", true, false, 128},
		{"absorbed, from the top", map[string]string{"a.go": done}, ".", false, false, 0},
		{"absorbed, from a subdirectory", map[string]string{"a.go": done}, "sub", false, false, 0},
		{"absorbed, under errexit", map[string]string{"a.go": done}, ".", false, true, 0},
		{"a live marker, under errexit", map[string]string{"a.go": marker}, ".", false, true, 1},
		{"from inside .git, under errexit", map[string]string{"a.go": done}, ".git", false, true, 128},
		{"outside a work tree, under errexit", map[string]string{"a.go": done}, ".", true, true, 128},
		{"a marker behind a -diff attribute", map[string]string{"a.go": marker, ".gitattributes": "* -diff\n"}, ".", false, false, 1},
		{"a marker in an ignored file", map[string]string{".gitignore": "*.local.md\n", "notes.local.md": marker}, ".", false, false, 1},
		{"a marker in a binary file alone", map[string]string{"built": marker + "\x00"}, ".", false, false, 0},
		{"a marker under a nested node_modules", map[string]string{"web/node_modules/m/a.js": marker}, ".", false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"sub/b.go": "// nothing\n"}
			for rel, body := range tt.files {
				files[rel] = body
			}
			// Each subtest has its own temporary root, so each isolates again:
			// the ceiling then sits above this case's tree, and the case with
			// no repository finds none.
			gittest.Isolate(t)
			root := markerTree(t, files)

			if !tt.noRepo {
				out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput()
				require.NoError(t, err, "git init: %s", out)
			}

			script := check
			if tt.errexit {
				script = "set -e; " + check + "; echo reached"
			}
			run := exec.Command(sh, "-c", script)
			run.Dir = filepath.Join(root, tt.dir)
			out, err := run.CombinedOutput()
			code := 0
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				code = exitErr.ExitCode()
			} else {
				require.NoError(t, err, "running the check: %s", out)
			}
			assert.Equal(t, tt.want, code, "exit status of the printed check: %s", out)
			if tt.errexit {
				// The next line runs on an absorbed tree alone.
				assert.Equal(t, tt.want == 0, strings.Contains(string(out), "reached"),
					"whether the line after the check ran: %s", out)
			}
		})
	}
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
