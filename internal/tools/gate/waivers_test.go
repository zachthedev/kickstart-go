package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
}
