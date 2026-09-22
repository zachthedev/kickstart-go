package main

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// semverRE validates that stdout looks like SemVer 2.0.0:
// MAJOR.MINOR.PATCH with optional -prerelease and/or +buildmeta.
var semverRE = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?$`)

func TestMain_OutputShape(t *testing.T) {
	// coverage:ignore (exec wrapper; relies on go toolchain + git on PATH)
	out, err := exec.Command("go", "run", ".").CombinedOutput()
	require.NoError(t, err)
	got := strings.TrimSpace(string(out))
	assert.True(t, semverRE.MatchString(got),
		"version helper printed %q, want SemVer 2.0.0", got)
}

// TestMain_Direct invokes main() in-process so the single fmt.Print call
// is attributed to this package's coverage profile (the go-run subtest
// executes in a subprocess that the parent profile cannot see).
func TestMain_Direct(t *testing.T) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	main()

	require.NoError(t, w.Close())
	raw, err := io.ReadAll(r)
	require.NoError(t, err)
	got := strings.TrimSpace(string(raw))
	assert.True(t, semverRE.MatchString(got),
		"main() printed %q, want SemVer 2.0.0", got)
}
