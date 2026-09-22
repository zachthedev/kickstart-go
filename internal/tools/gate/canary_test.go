package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeActionlint plays actionlint: it checks the command line the check
// builds, reads the canary the check wrote, and answers with output. No
// linter starts under the test.
func fakeActionlint(t *testing.T, output string, err error) commandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		assert.Equal(t, "actionlint-path", name)
		require.Len(t, args, 3)
		assert.Equal(t, "-shellcheck=shellcheck-path", args[0])
		assert.Equal(t, "-pyflakes=", args[1])
		data, readErr := os.ReadFile(args[2]) //nolint:gosec // the path is the canary the check wrote
		require.NoError(t, readErr)
		assert.Contains(t, string(data), "$GITHUB_REF", "the canary carries the unquoted expansion")
		return []byte(output), err
	}
}

func TestCanaryFindings_ShellCheckRan(t *testing.T) {
	finding := "canary.yml:7:14: shellcheck reported issue in this script: SC2086:info:1:6: Double quote to prevent globbing [shellcheck]\n"
	run := fakeActionlint(t, finding, &exec.ExitError{})
	found, err := canaryFindings(run, "actionlint-path", "shellcheck-path")
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestCanaryFindings_ShellCheckAbsent(t *testing.T) {
	run := fakeActionlint(t, "", nil)
	found, err := canaryFindings(run, "actionlint-path", "shellcheck-path")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Contains(t, found[0], "ShellCheck never ran")
}

func TestCanaryFindings_ActionlintFailsToStart(t *testing.T) {
	run := fakeActionlint(t, "", os.ErrNotExist)
	_, err := canaryFindings(run, "actionlint-path", "shellcheck-path")
	assert.ErrorContains(t, err, "running actionlint-path")
}

func TestRunCommand_Absent(t *testing.T) {
	_, err := runCommand(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), "exit status"), "an absent program is not an exit status")
}
