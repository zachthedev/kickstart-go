package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The answers a real actionlint gives each canary with the stand-in wired in
// front of ShellCheck.
var (
	sc2086Answer  = output{stdout: []byte("canary.yml:7:14: shellcheck reported issue in this script: SC2086:info:1:6: Double quote to prevent globbing [shellcheck]\n"), code: 1}
	refusalAnswer = output{stdout: []byte("directive.yml:8:9: shellcheck reported issue in this script: SC0:error:1:1: the gate refuses a ShellCheck directive in a workflow script [shellcheck]\n"), code: 1}
)

// fakeActionlint plays actionlint: it checks the command line and environment
// the check builds, reads the canary and the config the check wrote, records
// the canaries it was handed, and answers with the output answers keys by the
// canary's file name. No linter starts under the test.
func fakeActionlint(t *testing.T, answers map[string]output, err error, handed *[]string) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "actionlint-path", name)
		assert.Empty(t, env, "the runner withholds SHELLCHECK_OPTS, so the check adds nothing")
		require.Len(t, args, 5)
		assert.Equal(t, []string{"-shellcheck=stand-in-command", "-pyflakes=", "-config-file"}, args[:3])
		config, readErr := os.ReadFile(args[3]) // #nosec G703 -- the path is the config the check wrote
		require.NoError(t, readErr)
		assert.Empty(t, config, "the named config is empty, so no project config applies")
		data, readErr := os.ReadFile(args[4]) // #nosec G703 -- the path is the canary the check wrote
		require.NoError(t, readErr)
		assert.Contains(t, string(data), "echo $GITHUB_REF", "each canary carries the unquoted expansion")
		*handed = append(*handed, filepath.Base(args[4]))
		return answers[filepath.Base(args[4])], err
	}
}

func TestCanaryFindings(t *testing.T) {
	tests := []struct {
		name    string
		answers map[string]output
		wantIn  []string
	}{
		{
			name:    "ShellCheck ran behind the stand-in, which refused the directive",
			answers: map[string]output{"canary.yml": sc2086Answer, "directive.yml": refusalAnswer},
		},
		{
			name:    "ShellCheck absent or failing to start",
			answers: map[string]output{"directive.yml": refusalAnswer},
			wantIn:  []string{"actionlint found no SC2086 in a script that carries one, so ShellCheck never ran behind the stand-in. Check that stand-in-command starts"},
		},
		{
			name: "the pinned ShellCheck named without the stand-in, which honors the directive",
			answers: map[string]output{
				"canary.yml":    sc2086Answer,
				"directive.yml": {},
			},
			wantIn: []string{"actionlint reported no refusal of a ShellCheck directive, so the stand-in never ran in front of ShellCheck"},
		},
		{
			name:    "both missing",
			answers: map[string]output{},
			wantIn:  []string{"ShellCheck never ran behind the stand-in", "the stand-in never ran in front of ShellCheck"},
		},
		{
			name: "each answer split by escape sequences, as a runner that forces color prints it",
			answers: map[string]output{
				"canary.yml":    {stdout: []byte("\x1b[1mcanary.yml:7:14:\x1b[0m shellcheck reported issue in this script: \x1b[33mSC20\x1b[0m86:info:1:6: Double quote\n"), code: 1},
				"directive.yml": {stdout: []byte("\x1b[1mdirective.yml:8:9:\x1b[0m shellcheck reported issue in this script: SC0:error:1:1: the gate \x1b[31mrefuses\x1b[0m a ShellCheck directive in a workflow script\n"), code: 1},
			},
		},
		{
			name: "actionlint that fails on a canary, reported as its exit and not as a guess about ShellCheck",
			answers: map[string]output{
				"canary.yml":    {stderr: []byte("panic: planted\n"), code: 3},
				"directive.yml": refusalAnswer,
			},
			wantIn: []string{"actionlint exited 3 over canary.yml, so the canary proves nothing. actionlint printed: panic: planted"},
		},
		{
			name: "findings on stderr alone, which is not where actionlint prints them",
			answers: map[string]output{
				"canary.yml":    {stderr: sc2086Answer.stdout, code: 1},
				"directive.yml": {stderr: refusalAnswer.stdout, code: 1},
			},
			wantIn: []string{"ShellCheck never ran behind the stand-in", "the stand-in never ran in front of ShellCheck"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handed []string
			found, err := canaryFindings(fakeActionlint(t, tt.answers, nil, &handed), "actionlint-path", "stand-in-command")
			require.NoError(t, err)
			assert.Equal(t, []string{"canary.yml", "directive.yml"}, handed, "both canaries run, each on its own")
			require.Len(t, found, len(tt.wantIn), "findings: %q", found)
			for i, want := range tt.wantIn {
				assert.Contains(t, found[i], want)
			}
		})
	}
	t.Run("actionlint that fails to start", func(t *testing.T) {
		var handed []string
		_, err := canaryFindings(fakeActionlint(t, nil, os.ErrNotExist, &handed), "actionlint-path", "stand-in-command")
		assert.ErrorContains(t, err, "running actionlint-path")
	})
}

func TestEmptyActionlintConfig(t *testing.T) {
	dir := t.TempDir()
	config, err := emptyActionlintConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, filepath.Dir(config))
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	assert.Empty(t, data)

	_, err = emptyActionlintConfig(filepath.Join(dir, "absent"))
	assert.Error(t, err)
}
