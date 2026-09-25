package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zizmorAnswers are what a stand-in zizmor answers: the audit's completed
// lines, colored when colored is set, and exit code.
type zizmorAnswers struct {
	completes []string
	colored   bool
	code      int
}

// fakeToken is what every stand-in gh answers with. No real token reaches a
// test.
const fakeToken = "fake-gh-token" // #nosec G101 -- a stand-in's fake value, never a credential

// trackedTree is what the zizmor cases track: two workflows and files beside
// them zizmor may complete too.
var trackedTree = []string{".github/workflows/ci.yml", ".github/workflows/cd.yml", ".github/zizmor.yml", "go.mod"}

// fakeZizmorRunner plays zizmor: it checks the command line and environment
// the row builds, for the mode it expects, and answers with a completed line
// for each file, as zizmor 1.30.1 logs them. The row runs zizmor once.
func fakeZizmorRunner(t *testing.T, online bool, answers zizmorAnswers) commandRunner {
	t.Helper()
	runs := 0
	return func(name string, env []string, args ...string) (output, error) {
		runs++
		require.Equal(t, 1, runs, "the row runs zizmor once, with its config")
		assert.Equal(t, "zizmor-path", name)
		assert.Equal(t, []string{"--no-progress", "--strict-collection", "--config", ".github/zizmor.yml"}, args[:4], "the config is named")
		assert.Equal(t, []string{"--collect=all", ".github"}, args[len(args)-2:], "every ignore rule is off")
		if online {
			assert.Equal(t, []string{"RUST_LOG=info", "GH_TOKEN=" + fakeToken}, env, "the token reaches zizmor")
			assert.NotContains(t, args, "--offline")
		} else {
			assert.Equal(t, []string{"RUST_LOG=info"}, env, "no token reaches zizmor offline")
			assert.Contains(t, args, "--offline")
		}
		format := " INFO audit: zizmor: \U0001F308 completed %s\n"
		if answers.colored {
			format = " \x1b[32mINFO\x1b[0m \x1b[2maudit\x1b[0m: zizmor: \U0001F308 \x1b[1mcompleted\x1b[0m %s\n"
		}
		var log strings.Builder
		for _, completed := range answers.completes {
			fmt.Fprintf(&log, format, completed)
		}
		return output{stdout: []byte("No findings to report.\n"), stderr: []byte(log.String()), code: answers.code}, nil
	}
}

// fakeGhRunner plays gh: it checks it was asked for a token with no variable
// but gh's own token names, and answers with out and err.
func fakeGhRunner(t *testing.T, out output, err error) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "gh-path", name)
		assert.Equal(t, []string{"auth", "token"}, args)
		for _, entry := range env {
			variable, _, _ := strings.Cut(entry, "=")
			assert.Contains(t, ghTokenNames, strings.ToUpper(variable), "gh gets its own token names alone")
		}
		return out, err
	}
}

// unasked fails the test when the row starts gh.
func unasked(t *testing.T) commandRunner {
	t.Helper()
	return func(string, []string, ...string) (output, error) {
		t.Error("the row started gh with no gh on PATH")
		return output{}, nil
	}
}

func TestZizmorFindings(t *testing.T) {
	answered := output{stdout: []byte(fakeToken + "\n")}
	both := []string{".github/workflows/ci.yml", ".github/workflows/cd.yml", ".github/zizmor.yml"}
	tests := []struct {
		name         string
		ask          func(t *testing.T) commandRunner
		gh           string
		online       bool
		answers      zizmorAnswers
		wantSummary  string
		wantFindings []string
	}{
		{
			name: "online when gh answers", ask: func(t *testing.T) commandRunner { return fakeGhRunner(t, answered, nil) }, gh: "gh-path", online: true,
			answers:     zizmorAnswers{completes: both},
			wantSummary: "zizmor ran online, since gh auth token answered, and completed 3 files: .github/workflows/ci.yml, .github/workflows/cd.yml, .github/zizmor.yml",
		},
		{
			name: "offline with no gh", ask: unasked, answers: zizmorAnswers{completes: both},
			wantSummary: "zizmor ran offline, since gh auth token did not answer, and completed 3 files",
		},
		{
			name: "offline when gh fails", ask: func(t *testing.T) commandRunner { return fakeGhRunner(t, output{code: 1}, nil) }, gh: "gh-path",
			answers: zizmorAnswers{completes: both}, wantSummary: "zizmor ran offline",
		},
		{
			name: "offline when gh leaves its output held", ask: func(t *testing.T) commandRunner { return fakeGhRunner(t, output{}, exec.ErrWaitDelay) }, gh: "gh-path",
			answers: zizmorAnswers{completes: both}, wantSummary: "zizmor ran offline",
		},
		{
			name: "offline when gh prints nothing but space", ask: func(t *testing.T) commandRunner { return fakeGhRunner(t, output{stdout: []byte(" \r\n")}, nil) }, gh: "gh-path",
			answers: zizmorAnswers{completes: both}, wantSummary: "zizmor ran offline",
		},
		{
			name: "paths with the Windows separator", ask: unasked,
			answers:     zizmorAnswers{completes: []string{`.github\workflows\ci.yml`, `.github\workflows\cd.yml`}},
			wantSummary: "completed 2 files: .github/workflows/ci.yml, .github/workflows/cd.yml",
		},
		{
			name: "completed lines carrying escape sequences, as a runner that forces color prints them", ask: unasked,
			answers:     zizmorAnswers{completes: both, colored: true},
			wantSummary: "zizmor ran offline, since gh auth token did not answer, and completed 3 files: .github/workflows/ci.yml, .github/workflows/cd.yml, .github/zizmor.yml",
		},
		{
			name: "a workflow zizmor never completed", ask: unasked, answers: zizmorAnswers{completes: []string{".github/workflows/ci.yml"}},
			wantFindings: []string{"zizmor logged no completed line for .github/workflows/cd.yml, so it did not audit it"},
		},
		{
			name: "no completed line at all", ask: unasked,
			wantFindings: []string{
				"zizmor logged no completed line for .github/workflows/ci.yml, so it did not audit it",
				"zizmor logged no completed line for .github/workflows/cd.yml, so it did not audit it",
			},
		},
		{
			name: "a finding", ask: unasked, answers: zizmorAnswers{completes: both, code: 13},
			wantFindings: []string{"zizmor exited 13, running offline, since gh auth token did not answer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := zizmorFindings(fakeZizmorRunner(t, tt.online, tt.answers), tt.ask(t), tt.gh, "zizmor-path", trackedTree)
			require.NoError(t, err)
			assert.Equal(t, tt.wantFindings, result.findings)
			assert.Contains(t, result.summary, tt.wantSummary)
			if len(tt.wantFindings) > 0 {
				assert.Contains(t, string(result.relay), "No findings to report", "zizmor's own output reaches the relay")
			}
		})
	}
	t.Run("no tracked workflow", func(t *testing.T) {
		result, err := zizmorFindings(fakeZizmorRunner(t, false, zizmorAnswers{}), unasked(t), "", "zizmor-path", []string{"go.mod"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], "git tracks no file in .github/workflows")
	})
	t.Run("zizmor that cannot start", func(t *testing.T) {
		failing := func(string, []string, ...string) (output, error) { return output{}, os.ErrNotExist }
		_, err := zizmorFindings(failing, unasked(t), "", "zizmor-path", trackedTree)
		assert.ErrorContains(t, err, "running zizmor-path")
	})
}

// TestGhToken_Environment hands gh the token names the gate withholds from
// every other program, and nothing else it withholds.
func TestGhToken_Environment(t *testing.T) {
	t.Setenv("GH_TOKEN", "fake-inherited")
	t.Setenv("GH_HOST", "elsewhere.invalid")
	var seen []string
	ask := func(_ string, env []string, _ ...string) (output, error) {
		seen = env
		return output{stdout: []byte(fakeToken)}, nil
	}
	assert.Equal(t, fakeToken, ghToken(ask, "gh-path"))
	assert.Contains(t, seen, "GH_TOKEN=fake-inherited")
	for _, entry := range seen {
		assert.False(t, strings.HasPrefix(strings.ToUpper(entry), "GH_HOST="), "gh answers for github.com")
	}
}

// fakeGhProgram answers `gh auth token` with the fake token. GATE_FAKE_GH set
// to hang makes it sleep a minute first, and set to hold makes it answer,
// start a copy of itself that holds its output for longer than pipeGrace, and
// exit.
func fakeGhProgram(stdout, stderr io.Writer) int {
	switch os.Getenv("GATE_FAKE_GH") {
	case "hang":
		time.Sleep(time.Minute)
	case "linger":
		time.Sleep(pipeGrace + 3*time.Second)
	case "hold":
		self, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 99
		}
		cmd := exec.Command(self) // #nosec G204 -- the stand-in starts its own test binary
		cmd.Env = append(os.Environ(), "GATE_FAKE_GH=linger")
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(stderr, err)
			return 99
		}
		_ = cmd.Process.Release()
	}
	fmt.Fprintln(stdout, fakeToken)
	return 0
}

// fakeZizmorProgram logs a completed line for each file in .github/workflows,
// with the platform's separator, as zizmor 1.30.1 does, and exits 0.
func fakeZizmorProgram(stderr io.Writer) int {
	entries, _ := os.ReadDir(filepath.FromSlash(workflowsDir))
	for _, entry := range entries {
		fmt.Fprintf(stderr, " INFO audit: zizmor: \U0001F308 completed %s\n", filepath.Join(filepath.FromSlash(workflowsDir), entry.Name()))
	}
	return 0
}

// TestGhToken_Process starts the stand-in gh through the runner the zizmor
// row asks with: a gh that outlives ghTimeout, or leaves a process holding its
// output past pipeGrace, reads as no token, so the row runs offline rather
// than waiting.
func TestGhToken_Process(t *testing.T) {
	gh := filepath.Join(fakeProgramDir(t, "gh"), programName("gh"))
	ask := commandWithin(t.Context(), ghTimeout)
	t.Run("a gh that answers", func(t *testing.T) {
		t.Setenv("GATE_FAKE_GH", "")
		assert.Equal(t, fakeToken, ghToken(ask, gh))
	})
	t.Run("a gh that hangs", func(t *testing.T) {
		t.Setenv("GATE_FAKE_GH", "hang")
		began := time.Now()
		_, err := ask(gh, nil, "auth", "token")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(began), ghTimeout+pipeGrace+3*time.Second, "gh held the row")
		assert.Empty(t, ghToken(ask, gh))
	})
	t.Run("a gh that leaves its output held", func(t *testing.T) {
		t.Setenv("GATE_FAKE_GH", "hold")
		began := time.Now()
		// The copy gh starts runs from the stand-in's directory, which Windows
		// cannot remove until it exits, so the test outlasts it.
		t.Cleanup(func() { time.Sleep(time.Until(began.Add(pipeGrace + 5*time.Second))) })
		// With no timeout of its own, the runner ends the wait at pipeGrace.
		_, err := commandWithin(t.Context(), 0)(gh, nil, "auth", "token")
		assert.ErrorIs(t, err, exec.ErrWaitDelay)
		assert.Less(t, time.Since(began), pipeGrace+3*time.Second, "the run waited for the process holding the pipe")
	})
	t.Run("an absent gh", func(t *testing.T) {
		assert.Empty(t, ghToken(ask, filepath.Join(t.TempDir(), "absent")))
	})
}
