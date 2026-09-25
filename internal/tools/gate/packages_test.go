package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoPairs is the coverage most cases read: the release pair CI never hosts,
// and a host pair.
var twoPairs = lintCoverage{
	platforms: []platform{{goos: "linux", goarch: "arm64"}, {goos: "windows", goarch: "amd64"}},
	tagSets:   []string{""},
	release:   []platform{{goos: "linux", goarch: "arm64"}},
}

// fakeGoRunner plays go: it checks the run inherits what the package rows
// inherit and asks for go list ./... alone, and answers with out.
func fakeGoRunner(t *testing.T, out output) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "go-path", name)
		assert.Empty(t, env, "go inherits what the package rows inherit")
		assert.Equal(t, []string{"list", "./..."}, args)
		return out, nil
	}
}

func TestPackagesFindings(t *testing.T) {
	three := output{stdout: []byte("example.com/a\nexample.com/b\r\nexample.com/c\n")}
	tests := []struct {
		name         string
		out          output
		wantSummary  string
		wantFindings []string
		wantRelay    string
	}{
		{
			name:        "packages",
			out:         three,
			wantSummary: "go list ./... matched 3 packages, and the rows read linux/arm64 windows/amd64, each with no build tags",
		},
		{
			name:        "one package",
			out:         output{stdout: []byte("example.com/a\n")},
			wantSummary: "go list ./... matched 1 package,",
		},
		{
			name:         "no package",
			out:          output{stderr: []byte(`go: warning: "./..." matched no packages` + "\n")},
			wantSummary:  "matched 0 packages",
			wantFindings: []string{"go list ./... matched no package, so vet, lint, deadcode, testpair and build check nothing"},
			wantRelay:    "matched no packages",
		},
		{
			name:         "a load error",
			out:          output{stdout: []byte("example.com/a\n"), stderr: []byte("go: cannot load\n"), code: 1},
			wantFindings: []string{"go list exited 1"}, wantRelay: "cannot load",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := packagesFindings(fakeGoRunner(t, tt.out), "go-path", t.TempDir(), nil, twoPairs, nil)
			require.NoError(t, err)
			require.Len(t, result.findings, len(tt.wantFindings), "findings: %q", result.findings)
			for i, want := range tt.wantFindings {
				assert.Contains(t, result.findings[i], want)
			}
			assert.Contains(t, result.summary, tt.wantSummary)
			assert.Contains(t, string(result.relay), tt.wantRelay)
		})
	}
	t.Run("the summary names each tag set", func(t *testing.T) {
		cover := twoPairs
		cover.tagSets = []string{"", "dev"}
		result, err := packagesFindings(fakeGoRunner(t, three), "go-path", t.TempDir(), nil, cover, nil)
		require.NoError(t, err)
		assert.Empty(t, result.findings)
		assert.Contains(t, result.summary, "each with no build tags and with -tags dev")
	})
	t.Run("go that cannot start", func(t *testing.T) {
		failing := func(string, []string, ...string) (output, error) { return output{}, os.ErrNotExist }
		_, err := packagesFindings(failing, "go-path", t.TempDir(), nil, twoPairs, nil)
		assert.ErrorContains(t, err, "running go-path")
	})
	t.Run("the file checks join the row", func(t *testing.T) {
		root := t.TempDir()
		files := map[string]string{
			"rt.go":                 "// Do not edit this table.\npackage rt\n",
			"internal/_old/old.go":  "package old\n",
			"internal/w/w.go":       "package w\n\nvar x = 1 //nolint:all // why\n",
			"internal/w/w_linux.go": "package w\n",
		}
		for name, content := range files {
			target := filepath.Join(root, filepath.FromSlash(name))
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
			require.NoError(t, os.WriteFile(target, []byte(content), 0o600))
		}
		cover := lintCoverage{platforms: []platform{{goos: "windows", goarch: "amd64"}}, tagSets: []string{""}}
		tracked := []string{"rt.go", "internal/_old/old.go", "internal/w/w.go", "internal/w/w_linux.go"}
		result, err := packagesFindings(fakeGoRunner(t, three), "go-path", root, tracked, cover, nil)
		require.NoError(t, err)
		require.Len(t, result.findings, 3, "findings: %q", result.findings)
		assert.Contains(t, result.findings[0], `"internal/w/w_linux.go" compiles in no build the lint and vet rows read`)
		assert.Contains(t, result.findings[1], `"rt.go" carries "do not edit"`)
		assert.Contains(t, result.findings[2], `internal/w/w.go:3 carries "//nolint:all // why"`)
	})
}

func TestParseCoverage(t *testing.T) {
	t.Run("the three lists and the generated files", func(t *testing.T) {
		cover, generated, err := parseCoverage([]string{
			"-platforms", "linux/amd64  linux/arm64 windows/amd64", "-tags", "dev dev,trace", "-release", "linux/arm64",
			".env.template", "MARKERS.md",
		})
		require.NoError(t, err)
		assert.Equal(t, []platform{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}}, cover.platforms)
		assert.Equal(t, []string{"", "dev", "dev,trace"}, cover.tagSets, "the empty set comes first")
		assert.Equal(t, []platform{{"linux", "arm64"}}, cover.release)
		assert.Equal(t, []string{".env.template", "MARKERS.md"}, generated)
	})
	t.Run("no tag sets and no release", func(t *testing.T) {
		cover, generated, err := parseCoverage([]string{"-platforms", "linux/amd64", "-tags", "", "-release", ""})
		require.NoError(t, err)
		assert.Equal(t, []string{""}, cover.tagSets)
		assert.Empty(t, cover.release)
		assert.Empty(t, generated)
	})
	failures := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{name: "no platforms", args: []string{"-tags", "dev"}, wantIn: "-platforms names no os/arch pair"},
		{name: "a platform with no arch", args: []string{"-platforms", "linux darwin"}, wantIn: `-platforms: "linux" is not an os/arch pair such as linux/amd64`},
		{name: "a platform with an empty half", args: []string{"-platforms", "/amd64"}, wantIn: `"/amd64" is not an os/arch pair`},
		{name: "a platform with a third part", args: []string{"-platforms", "linux/arm/v7"}, wantIn: `"linux/arm/v7" is not an os/arch pair`},
		{name: "a bad release pair", args: []string{"-platforms", "linux/amd64", "-release", "linux"}, wantIn: `-release: "linux" is not an os/arch pair`},
		{name: "an unknown flag", args: []string{"-platform", "linux/amd64"}, wantIn: "flag provided but not defined: -platform"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseCoverage(tt.args)
			assert.ErrorContains(t, err, tt.wantIn)
		})
	}
}

// Each case plants one tracked Go file, and the check must refuse it wherever
// no build the lint and vet rows read compiles it, as go's own file matcher
// decides, and pass it wherever one does.
func TestCoverageFindings(t *testing.T) {
	hosts := lintCoverage{
		platforms: []platform{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}, {"windows", "amd64"}},
		tagSets:   []string{""},
	}
	withDev := hosts
	withDev.tagSets = []string{"", "dev"}
	tests := []struct {
		name    string
		file    string
		content string
		cover   lintCoverage
		wantIn  string
	}{
		{name: "no constraint", file: "internal/a/a.go", content: "package a\n", cover: hosts},
		{name: "a platform suffix the rows read", file: "internal/a/a_windows.go", content: "package a\n", cover: hosts},
		{name: "an arch suffix the rows read", file: "internal/a/a_arm64.go", content: "package a\n", cover: hosts},
		{name: "a unix constraint", file: "internal/a/a.go", content: "//go:build unix\n\npackage a\n", cover: hosts},
		{name: "a release pair CI never hosts", file: "cmd/x/use.go", content: "//go:build linux && arm64\n\npackage main\n", cover: hosts},
		{name: "a platform the rows never read", file: "internal/a/a.go", content: "//go:build freebsd\n\npackage a\n", cover: hosts, wantIn: `"internal/a/a.go" compiles in no build the lint and vet rows read (linux/amd64 linux/arm64 darwin/arm64 windows/amd64, each with no build tags)`},
		{name: "a platform suffix the rows never read", file: "internal/a/a_plan9.go", content: "package a\n", cover: hosts, wantIn: "compiles in no build"},
		{name: "an arch suffix the rows never read", file: "internal/a/a_s390x.go", content: "package a\n", cover: hosts, wantIn: "compiles in no build"},
		{name: "a legacy build line", file: "internal/a/a.go", content: "// +build solaris\n\npackage a\n", cover: hosts, wantIn: "compiles in no build"},
		{name: "a dev tag the rows never read", file: "internal/a/feature.go", content: "//go:build dev\n\npackage a\n", cover: hosts, wantIn: "Name the pair in LINT_PLATFORMS or the tag set in LINT_TAGS"},
		{name: "a dev tag the rows read", file: "internal/a/feature.go", content: "//go:build dev\n\npackage a\n", cover: withDev},
		{name: "a dev test the rows read", file: "internal/a/feature_test.go", content: "//go:build dev\n\npackage a\n", cover: withDev},
		{name: "the stub beside it", file: "internal/a/feature_stub.go", content: "//go:build !dev\n\npackage a\n", cover: withDev},
		{name: "two tags a set names together", file: "internal/a/t.go", content: "//go:build dev && trace\n\npackage a\n", cover: lintCoverage{platforms: hosts.platforms, tagSets: []string{"", "dev,trace"}}},
		{name: "two tags no set names together", file: "internal/a/t.go", content: "//go:build dev && trace\n\npackage a\n", cover: lintCoverage{platforms: hosts.platforms, tagSets: []string{"", "dev", "trace"}}, wantIn: "compiles in no build"},
		{name: "ignore", file: "internal/a/gen.go", content: "//go:build ignore\n\npackage main\n", cover: withDev, wantIn: "compiles in no build"},
		{name: "cgo, on for a native pair", file: "internal/a/c.go", content: "//go:build cgo\n\npackage a\n", cover: hosts},
		{name: "a file go never builds for its name", file: "internal/a/_draft.go", content: "package a\n", cover: hosts, wantIn: "compiles in no build"},
		{name: "a directory starting with _, which review holds", file: "internal/_hidden/h.go", content: "package hidden\n", cover: hosts},
		{name: "a directory starting with _ holding a file no build compiles", file: "_tools/t.go", content: "//go:build freebsd\n\npackage tools\n", cover: hosts, wantIn: `"_tools/t.go" compiles in no build`},
		{name: "a fixture under testdata", file: "internal/a/testdata/f.go", content: "//go:build ignore\n\npackage f\n", cover: hosts},
		{name: "an extension go never reads", file: "internal/a/A.GO", content: "//go:build freebsd\n\npackage a\n", cover: hosts},
		{name: "a file that is not Go", file: "docs/a.md", content: "//go:build freebsd\n", cover: hosts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, filepath.FromSlash(tt.file))
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
			require.NoError(t, os.WriteFile(target, []byte(tt.content), 0o600))
			found, err := coverageFindings(root, []string{tt.file, "internal/a/deleted_freebsd.go"}, tt.cover)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1, "findings: %q", found)
			assert.Contains(t, found[0], tt.wantIn)
		})
	}
	t.Run("a release pair the rows never read", func(t *testing.T) {
		cover := lintCoverage{
			platforms: []platform{{"linux", "amd64"}, {"windows", "amd64"}},
			tagSets:   []string{""},
			release:   []platform{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}},
		}
		found, err := coverageFindings(t.TempDir(), nil, cover)
		require.NoError(t, err)
		require.Len(t, found, 2)
		assert.Contains(t, found[0], "RELEASE_TARGETS ships linux/arm64, which LINT_PLATFORMS does not name, so no lint or vet run reads the code that build compiles")
		assert.Contains(t, found[1], "RELEASE_TARGETS ships darwin/arm64")
	})
	t.Run("a tracked Go path that is a directory is an error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "dir.go"), 0o700))
		_, err := coverageFindings(root, []string{"dir.go"}, hosts)
		assert.ErrorContains(t, err, "reading dir.go")
	})
}

// Each case plants one tracked Go file, and the check must refuse a lax
// generated marker wherever golangci-lint 2.13.2 reads one, however it is
// disguised, pass one it never reads, and pass a file cmd/generate writes.
func TestLaxMarkerFindings(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		content   string
		generated []string
		wantIn    string
	}{
		{name: "prose above the package clause", file: "internal/rt/rt.go", content: "// The table here mirrors README.md. Do not edit one without the other.\npackage rt\n", wantIn: `"internal/rt/rt.go" carries "do not edit"`},
		{name: "a doc comment after the package clause", file: "internal/rt/rt.go", content: "package rt\n\n// Header is the canonical \"DO NOT EDIT\" marker.\nconst Header = \"x\"\n", wantIn: `carries "do not edit"`},
		{name: "code generated in a block comment", file: "rt.go", content: "/*\n Code Generated here\n*/\npackage rt\n", wantIn: `carries "code generated"`},
		{name: "an autogenerated file note", file: "rt.go", content: "// This is an AutoGenerated File.\npackage rt\n", wantIn: `carries "autogenerated file"`},
		{name: "the swagger codegen marker", file: "rt.go", content: "/*\n * Generated by: Swagger Codegen (https://example.com)\n */\npackage rt\n", wantIn: `carries "* generated by: swagger codegen "`},
		{name: "an extension in capitals", file: "RT.GO", content: "// do not edit\npackage rt\n", wantIn: `"RT.GO" carries`},
		{name: "a marker past the first declaration", file: "rt.go", content: "package rt\n\nconst x = 1\n\n// do not edit\nvar y = 2\n"},
		{name: "a marker inside a function", file: "rt.go", content: "package rt\n\nfunc f() {\n\t// do not edit\n}\n"},
		{name: "a file cmd/generate writes", file: "internal/gen/table.go", content: "// Auto-generated by cmd/generate. Do not edit.\npackage gen\n", generated: []string{"internal/gen/table.go"}},
		{name: "a file that does not parse", file: "rt.go", content: "// do not edit\npackage\n"},
		{name: "a file that is not Go", file: "docs/notes.md", content: "Do not edit.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, filepath.FromSlash(tt.file))
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
			require.NoError(t, os.WriteFile(target, []byte(tt.content), 0o600))
			found, err := laxMarkerFindings(root, []string{tt.file, "deleted.go"}, tt.generated)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
			assert.Contains(t, found[0], "golangci-lint reads as a generated file's mark")
		})
	}
	t.Run("an unreadable tracked file is an error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "dir.go"), 0o700))
		_, err := laxMarkerFindings(root, []string{"dir.go"}, nil)
		assert.ErrorContains(t, err, "reading dir.go")
	})
}

// fakeGoProgram answers go list ./... with two packages.
func fakeGoProgram(stdout io.Writer) int {
	fmt.Fprintln(stdout, "example.com/fake/a")
	fmt.Fprintln(stdout, "example.com/fake/b")
	return 0
}
