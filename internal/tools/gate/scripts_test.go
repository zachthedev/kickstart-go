package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkstyleFor is ShellCheck 0.11.0's checkstyle report naming each file,
// with the SC2016 finding it gives scripts/build.sh when finding is set.
func checkstyleFor(files []string, finding bool) []byte {
	report := "<?xml version='1.0' encoding='UTF-8'?>\n<checkstyle version='4.3'>\n"
	for _, name := range files {
		report += "<file name='" + name + "' >\n"
		if finding && name == "scripts/build.sh" {
			report += "<error line='15' column='16' severity='info' message='Expressions don&#39;t expand in single quotes&#44; use double quotes for that.' source='ShellCheck.SC2016' />\n"
		}
		report += "</file>\n"
	}
	return []byte(report + "</checkstyle>\n")
}

// fakeShellCheckRunner plays ShellCheck over the files after --: it checks the
// command line and environment the row builds, records the files, and answers
// with answer's output for them.
func fakeShellCheckRunner(t *testing.T, handed *[]string, answer func(files []string) output) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "shellcheck-path", name)
		assert.Empty(t, env, "the runner withholds SHELLCHECK_OPTS, so the row adds nothing")
		before, after, found := cutArgs(args, "--")
		require.True(t, found, "the files follow --")
		assert.Equal(t, []string{"--norc", "--format=checkstyle"}, before)
		*handed = after
		return answer(after), nil
	}
}

func TestScriptsFindings(t *testing.T) {
	tracked := []string{"scripts/build.sh", "scripts/RELEASE.SH", "scripts/notes.md", "Taskfile.yml"}
	wantHanded := []string{"scripts/build.sh", "scripts/RELEASE.SH"}
	clean := func(files []string) output { return output{stdout: checkstyleFor(files, false)} }

	tests := []struct {
		name        string
		answer      func(files []string) output
		wantSummary string
		wantIn      []string
		wantRelay   string
	}{
		{name: "ShellCheck checks every script it was handed", answer: clean, wantSummary: "shellcheck checked 2 script files: scripts/build.sh, scripts/RELEASE.SH"},
		{
			name: "a report whose names carry escape sequences, as a runner that forces color prints it",
			answer: func(files []string) output {
				report := strings.ReplaceAll(string(checkstyleFor(files, false)), "<file name='", "<file name='\x1b[1m")
				return output{stdout: []byte("\x1b[0m" + strings.ReplaceAll(report, "' >", "\x1b[22m' >"))}
			},
			wantSummary: "shellcheck checked 2 script files: scripts/build.sh, scripts/RELEASE.SH",
		},
		{
			name:      "a finding",
			answer:    func(files []string) output { return output{stdout: checkstyleFor(files, true), code: 1} },
			wantIn:    []string{"shellcheck exited 1"},
			wantRelay: "scripts/build.sh:15:16: info SC2016: Expressions don't expand in single quotes, use double quotes for that.\n",
		},
		{
			name:   "a script ShellCheck never reports checking",
			answer: func(files []string) output { return output{stdout: checkstyleFor(files[:1], false)} },
			wantIn: []string{"ShellCheck did not report checking scripts/RELEASE.SH, which the scripts row handed it"},
		},
		{
			name:      "a report that is not checkstyle",
			answer:    func([]string) output { return output{stdout: []byte("In scripts/build.sh line 15:\n")} },
			wantIn:    []string{"ShellCheck's checkstyle report cannot be read back", "did not report checking scripts/build.sh", "did not report checking scripts/RELEASE.SH"},
			wantRelay: "In scripts/build.sh line 15:\n",
		},
		{
			name: "a file that does not exist, which ShellCheck reports on stderr",
			answer: func(files []string) output {
				return output{stdout: checkstyleFor(files[:1], false), stderr: []byte("scripts/RELEASE.SH: does not exist\n"), code: 2}
			},
			wantIn:    []string{"did not report checking scripts/RELEASE.SH", "shellcheck exited 2"},
			wantRelay: "scripts/RELEASE.SH: does not exist\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handed []string
			result, err := scriptsFindings(fakeShellCheckRunner(t, &handed, tt.answer), "shellcheck-path", t.TempDir(), tracked)
			require.NoError(t, err)
			assert.Equal(t, wantHanded, handed)
			if tt.wantSummary != "" {
				assert.Equal(t, tt.wantSummary, result.summary)
			}
			require.Len(t, result.findings, len(tt.wantIn), "findings: %q", result.findings)
			for i, want := range tt.wantIn {
				assert.Contains(t, result.findings[i], want)
			}
			assert.Equal(t, tt.wantRelay, string(result.relay))
		})
	}

	t.Run("no tracked script", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) {
			t.Fatal("ShellCheck must not start with nothing to hand it")
			return output{}, nil
		}
		result, err := scriptsFindings(run, "shellcheck-path", t.TempDir(), []string{"README.md"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], "git tracks no .sh file, so the scripts row has nothing to hand ShellCheck and checks nothing")
	})
	t.Run("ShellCheck that does not start", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) { return output{}, errors.New("no such file") }
		_, err := scriptsFindings(run, "shellcheck-path", t.TempDir(), tracked)
		assert.ErrorContains(t, err, "running shellcheck-path")
	})
	t.Run("a refused directive fails the row beside ShellCheck's own pass", func(t *testing.T) {
		root := t.TempDir()
		script := filepath.Join(root, "scripts", "build.sh")
		require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n# shellcheck disable=SC2086\necho $X\n"), 0o600))
		var handed []string
		result, err := scriptsFindings(fakeShellCheckRunner(t, &handed, clean), "shellcheck-path", root, []string{"scripts/build.sh"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], `scripts/build.sh:2 carries "# shellcheck disable=SC2086", and a ShellCheck directive in a script takes one form`)
	})
}

// Each case is one line of a tracked script. The check must pass the one
// directive form the set allows and refuse every other line ShellCheck could
// read as a directive.
func TestDirectiveFindings(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		refused bool
	}{
		{name: "one code and a reason", line: "# shellcheck disable=SC2086 # TAGS is split on purpose."},
		{name: "two codes and a reason", line: "# shellcheck disable=SC2086,SC2046 # both splits are the point"},
		{name: "indented", line: "\t  # shellcheck disable=SC2086 # why"},
		{name: "a carriage return at the end", line: "# shellcheck disable=SC2086 # why\r"},
		{name: "no reason", line: "# shellcheck disable=SC2086", refused: true},
		{name: "an empty reason", line: "# shellcheck disable=SC2086 # ", refused: true},
		{name: "every code", line: "# shellcheck disable=all # why", refused: true},
		{name: "a range", line: "# shellcheck disable=SC2000-SC3000 # why", refused: true},
		{name: "a code with no prefix", line: "# shellcheck disable=2086 # why", refused: true},
		{name: "a second key", line: "# shellcheck disable=SC2086 source=/dev/null # why", refused: true},
		{name: "a key before the disable", line: "# shellcheck source=/dev/null disable=SC2086 # why", refused: true},
		{name: "a key after a quoted value", line: "# shellcheck source='x'disable=SC2086 # why", refused: true},
		{name: "a source directive", line: "# shellcheck source=lib.sh # why", refused: true},
		{name: "a shell directive", line: "# shellcheck shell=bash # why", refused: true},
		{name: "an enable directive", line: "# shellcheck enable=all # why", refused: true},
		{name: "trailing a command", line: "echo $X # shellcheck disable=SC2086 # why", refused: true},
		{name: "no space after the hash", line: "#shellcheck disable=SC2086 # why", refused: true},
		{name: "a tab between the words", line: "#\tshellcheck disable=SC2086 # why", refused: true},
		{name: "a no-break space", line: "# shellcheck\u00a0disable=SC2086 # why", refused: true},
		{name: "capitals", line: "# ShellCheck disable=SC2086 # why", refused: true},
		{name: "two spaces before the reason", line: "# shellcheck disable=SC2086  # why", refused: true},
		{name: "the word in prose", line: "# run shellcheck before pushing"},
		{name: "a shebang", line: "#!/bin/sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "scripts", "x.sh")
			require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+tt.line+"\necho done\n"), 0o600))
			found, err := directiveFindings(root, []string{"scripts/x.sh", "scripts/gone.sh"})
			require.NoError(t, err)
			if !tt.refused {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], "scripts/x.sh:2 carries")
			assert.Contains(t, found[0], "a ShellCheck directive in a script takes one form, as its whole line")
		})
	}
	t.Run("a script path that is a directory is an error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "x.sh"), 0o700))
		_, err := directiveFindings(root, []string{"x.sh"})
		assert.ErrorContains(t, err, "reading x.sh")
	})
}

// Each case is a directive in the one form the set allows, whose reason the
// form's \S accepts. The check must refuse a reason that is empty once its
// default-ignorable code points are dropped, one case per code point, and pass
// one that reads.
func TestDirectiveFindings_InvisibleReason(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		refused bool
	}{
		{name: "a reason that reads", reason: "TAGS is split on purpose."},
		{name: "a reason opening with an invisible code point", reason: "\U0000200BTAGS is split on purpose."},
		{name: "invisible code points and spaces", reason: "\U0000200B \U00002060 ", refused: true},
	}
	for _, point := range invisibleCodePoints {
		tests = append(tests, struct {
			name    string
			reason  string
			refused bool
		}{name: point.name, reason: string(point.r), refused: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "scripts", "x.sh")
			require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n# shellcheck disable=SC2086 # "+tt.reason+"\necho $X\n"), 0o600))
			found, err := directiveFindings(root, []string{"scripts/x.sh"})
			require.NoError(t, err)
			if !tt.refused {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], "scripts/x.sh:2 carries")
			assert.Contains(t, found[0], invisibleReasonWhy)
		})
	}
}
