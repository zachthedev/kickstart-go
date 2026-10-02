package gittest

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain lets this package's own tests run the stand-in.
func TestMain(m *testing.M) {
	StandInMain()
	os.Exit(m.Run())
}

// ///////////////////////////////////////////////
// NewStandIn
// ///////////////////////////////////////////////

// A stand-in run as git exits 0 once its child runs, and the child holds the
// output until the release ends it.
func TestNewStandIn_ChildHoldsOutputUntilRelease(t *testing.T) {
	s := NewStandIn(t)
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { reader.Close() })

	cmd := exec.Command(filepath.Join(s.Dir, standInName()), "remote", "get-url", "origin") // #nosec G204 -- the test runs the stand-in it built
	cmd.Stdout = writer
	require.NoError(t, cmd.Run(), "the stand-in exits 0 once its child runs")
	require.NoError(t, writer.Close())

	require.True(t, s.Started(), "the stand-in never started the child that holds its output")
	assert.NoFileExists(t, filepath.Join(s.Dir, standInExited), "the child ended before its release")

	read := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(reader)
		read <- err
	}()
	s.release(t)
	assert.FileExists(t, filepath.Join(s.Dir, standInExited), "the child did not end at its release")
	select {
	case err := <-read:
		assert.NoError(t, err, "reading the stand-in's output")
	case <-time.After(releaseWait):
		t.Errorf("the stand-in's output stayed open %s after its child ended", releaseWait)
	}
}

// ///////////////////////////////////////////////
// checkStandIn
// ///////////////////////////////////////////////

// A stand-in started by another stand-in, other than the holding child, ends
// at once and names the TestMain call that is missing.
func TestCheckStandIn_EndsANestedStandIn(t *testing.T) {
	s := NewStandIn(t)
	cmd := exec.Command(filepath.Join(s.Dir, standInName()), "remote", "get-url", "origin") // #nosec G204 -- the test runs the stand-in it built
	cmd.Env = append(os.Environ(), standInNested+"=1")

	out, err := cmd.CombinedOutput()

	exit, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "the nested stand-in did not fail: %v\n%s", err, out)
	assert.Equal(t, 125, exit.ExitCode(), "exit code")
	assert.Contains(t, string(out), "gittest.StandInMain", "the refusal names the missing call")
	assert.False(t, s.Started(), "the nested stand-in started a child")
}

// ///////////////////////////////////////////////
// isStandIn
// ///////////////////////////////////////////////

func TestIsStandIn_Cases(t *testing.T) {
	dir := filepath.Join("tmp", "standin")
	tests := []struct {
		name string
		arg0 string
		dir  string
		want bool
	}{
		{name: "git under a stand-in test", arg0: filepath.Join(dir, "git"), dir: dir, want: true},
		{name: "git.exe under a stand-in test", arg0: filepath.Join(dir, "git.exe"), dir: dir, want: true},
		{name: "upper-case GIT.EXE", arg0: filepath.Join(dir, "GIT.EXE"), dir: dir, want: true},
		{name: "git with no stand-in test", arg0: filepath.Join(dir, "git"), dir: "", want: false},
		{name: "this package's test binary", arg0: filepath.Join(dir, "gittest.test.exe"), dir: dir, want: false},
		{name: "another package's test binary", arg0: filepath.Join(dir, "remote.test"), dir: dir, want: false},
		{name: "a name that starts with git", arg0: filepath.Join(dir, "github"), dir: dir, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStandIn(tt.arg0, tt.dir)
			assert.Equal(t, tt.want, got)
		})
	}
}
