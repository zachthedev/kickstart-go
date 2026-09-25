package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingReader fails every read, as a closed or broken stdin does.
type failingReader struct{}

// fakeShellCheck plays ShellCheck. Asked for checkstyle, as the scripts row
// asks, it names every file after -- clean. Behind the stand-in, it writes the
// script it read on stdin back to stdout, its arguments and whether
// SHELLCHECK_OPTS reached it to stderr, and exits with the code an
// "exit=<n>" argument names.
func fakeShellCheck(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	if slices.Contains(args, "--format=checkstyle") {
		_, files, _ := cutArgs(args, "--")
		_, _ = stdout.Write(checkstyleFor(files, false))
		return 0
	}
	script, _ := io.ReadAll(stdin)
	_, _ = stdout.Write(script)
	_, opts := os.LookupEnv("SHELLCHECK_OPTS")
	fmt.Fprintf(stderr, "args=%s opts=%t\n", strings.Join(args, " "), opts)
	for _, arg := range args {
		if code, ok := strings.CutPrefix(arg, "exit="); ok {
			n, _ := strconv.Atoi(code)
			return n
		}
	}
	return 0
}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestStandIn(t *testing.T) {
	shellcheck := filepath.Join(fakeProgramDir(t, "shellcheck"), programName("shellcheck"))
	// actionlint's own arguments, which the stand-in hands ShellCheck as they
	// came.
	actionlintArgs := []string{"--norc", "-f", "json", "-x", "--shell", "bash", "-"}
	t.Setenv("SHELLCHECK_OPTS", "--exclude=SC2086")

	tests := []struct {
		name       string
		args       []string
		stdin      io.Reader
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "a clean script reaches ShellCheck byte for byte without SHELLCHECK_OPTS",
			args:       append([]string{shellcheck}, actionlintArgs...),
			stdin:      strings.NewReader("set -eo pipefail\necho \"$X\"\n"),
			wantStdout: "set -eo pipefail\necho \"$X\"\n",
			wantStderr: "args=--norc -f json -x --shell bash - opts=false",
		},
		{
			name:       "ShellCheck's exit code passes through",
			args:       []string{shellcheck, "exit=1", "-"},
			stdin:      strings.NewReader("echo $X\n"),
			wantCode:   1,
			wantStdout: "echo $X\n",
			wantStderr: "args=exit=1 - opts=false",
		},
		{
			name:       "ShellCheck that cannot start fails closed, with nothing on stdout",
			args:       []string{filepath.Join(t.TempDir(), "absent"), "-"},
			stdin:      strings.NewReader("echo hi\n"),
			wantCode:   2,
			wantStderr: "gate: running ",
		},
		{
			name:       "no ShellCheck path",
			stdin:      strings.NewReader("echo hi\n"),
			wantCode:   2,
			wantStderr: "the ShellCheck stand-in needs the ShellCheck path",
		},
		{
			name:       "a script that cannot be read",
			args:       []string{shellcheck, "-"},
			stdin:      failingReader{},
			wantCode:   2,
			wantStderr: "gate: reading the script: broken pipe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := standIn(t.Context(), tt.args, tt.stdin, &stdout, &stderr)
			assert.Equal(t, tt.wantCode, code, "stderr: %s", stderr.String())
			assert.Equal(t, tt.wantStdout, stdout.String())
			assert.Contains(t, stderr.String(), tt.wantStderr)
		})
	}

	t.Run("a directive comes back as ShellCheck's JSON, and ShellCheck never starts", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		script := "set -eo pipefail\necho hi\n# shellcheck disable=SC2086\necho $X\n"
		code := standIn(t.Context(), append([]string{shellcheck}, actionlintArgs...), strings.NewReader(script), &stdout, &stderr)
		assert.Equal(t, 1, code)
		assert.Empty(t, stderr.String(), "ShellCheck never ran, so nothing reports its arguments")
		var reports []shellCheckReport
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &reports), "stdout: %s", stdout.String())
		require.Len(t, reports, 1)
		assert.Equal(t, 3, reports[0].Line)
		assert.Equal(t, 1, reports[0].Column)
		assert.Equal(t, "error", reports[0].Level)
		assert.Equal(t, 0, reports[0].Code)
		assert.Contains(t, reports[0].Message, `the gate refuses a ShellCheck directive in a workflow script, since it silences ShellCheck for the lines after it: "# shellcheck disable=SC2086"`)
	})
}

// Each case is one decoded line, as actionlint hands a run: script to
// ShellCheck once the YAML is read: the spellings that silence SC2086 under
// actionlint, whatever escape or fold produced them, must each come back
// refused, and lines ShellCheck never reads as a directive must pass.
func TestDirectiveReports(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		refused bool
	}{
		{name: "a disable", line: "# shellcheck disable=SC2086", refused: true},
		{name: "a disable of every code", line: "# shellcheck disable=all", refused: true},
		{name: "no space after the hash", line: "#shellcheck disable=SC2086", refused: true},
		{name: "a tab between the words", line: "#\tshellcheck\tdisable=SC2086", refused: true},
		{name: "a no-break space from an escape", line: "#\u00a0shellcheck\u00a0disable=SC2086", refused: true},
		{name: "a thin space", line: "# shellcheck\u2009disable=SC2086", refused: true},
		{name: "a narrow no-break space", line: "#\u202fshellcheck disable=SC2086", refused: true},
		{name: "a zero-width space", line: "#\u200bshellcheck disable=SC2086", refused: true},
		{name: "a source key before the disable", line: "# shellcheck source=/dev/null disable=SC2086", refused: true},
		{name: "a disable after a quoted value", line: "# shellcheck source='x'disable=SC2086", refused: true},
		{name: "a source directive alone", line: "# shellcheck source=scripts/lib.sh", refused: true},
		{name: "a shell directive", line: "# shellcheck shell=sh", refused: true},
		{name: "a plain scalar folded after a command", line: "true;# shellcheck disable=SC2086", refused: true},
		{name: "a directive trailing a command", line: "echo $X # shellcheck disable=SC2086", refused: true},
		{name: "indented", line: "    # shellcheck disable=SC2086", refused: true},
		{name: "a carriage return before the key", line: "# shellcheck\rdisable=SC2086", refused: true},
		{name: "capitals, which ShellCheck ignores and the gate refuses anyway", line: "# ShellCheck disable=SC2086", refused: true},
		{name: "the word in a message", line: `echo "run shellcheck first"`},
		{name: "a link", line: "# see https://www.shellcheck.net/wiki/SC2086"},
		{name: "the word alone at the end of the line", line: "# shellcheck"},
		{name: "a longer word", line: "# shellchecked by hand"},
		{name: "a hyphen after the word", line: "# shellcheck-disable=SC2086"},
		{name: "a quoted word", line: "echo '#shellcheck'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := "set -eo pipefail\n" + tt.line + "\necho done\n"
			reports := directiveReports([]byte(script))
			if !tt.refused {
				assert.Empty(t, reports)
				return
			}
			require.Len(t, reports, 1)
			assert.Equal(t, 2, reports[0].Line)
		})
	}
	t.Run("every directive line is reported, numbered from 1", func(t *testing.T) {
		reports := directiveReports([]byte("# shellcheck disable=SC1\necho\n# shellcheck disable=SC2\n"))
		require.Len(t, reports, 2)
		assert.Equal(t, 1, reports[0].Line)
		assert.Equal(t, 3, reports[1].Line)
	})
}

func TestStandInCommand(t *testing.T) {
	tests := []struct {
		name       string
		self       string
		shellcheck string
		want       string
		wantErr    string
	}{
		{
			name: "Windows paths turn to forward slashes, each word single-quoted", self: `C:\Users\a b\go-build\gate.exe`, shellcheck: `C:\mise\shellcheck.exe`,
			want: `'C:/Users/a b/go-build/gate.exe' 'shellcheck-stand-in' 'C:/mise/shellcheck.exe'`,
		},
		{name: "Unix paths", self: "/tmp/go-build/gate", shellcheck: "/opt/mise/shellcheck", want: `'/tmp/go-build/gate' 'shellcheck-stand-in' '/opt/mise/shellcheck'`},
		{name: "a single quote in the gate's path", self: "/tmp/it's/gate", shellcheck: "/opt/shellcheck", wantErr: "/tmp/it's/gate holds a single quote"},
		{name: "a single quote in ShellCheck's path", self: "/tmp/gate", shellcheck: "/opt/o'neil/shellcheck", wantErr: "holds a single quote, which the -shellcheck value cannot carry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if filepath.Separator != '\\' && strings.Contains(tt.self, `\`) {
				t.Skip("filepath.ToSlash turns backslashes to slashes on Windows alone")
			}
			got, err := standInCommand(tt.self, tt.shellcheck)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
