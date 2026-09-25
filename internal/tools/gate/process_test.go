package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandWithin starts the test binary as the stand-in mise, which prints
// the names in its environment and exits with the code its arguments name.
func TestCommandWithin(t *testing.T) {
	program := filepath.Join(fakeProgramDir(t, "mise"), programName("mise"))
	run := commandWithin(t.Context(), 0)

	t.Run("an exit code is output, not an error", func(t *testing.T) {
		out, err := run(program, []string{"GATE_PROBE=1"}, "exit=3")
		require.NoError(t, err)
		assert.Equal(t, 3, out.code)
		assert.Contains(t, string(out.stdout), "GATE_PROBE", "env reaches the program beside what the gate inherited")
		assert.Contains(t, strings.ToUpper(string(out.stdout)), "PATH", "the inherited environment reaches it too")
	})
	t.Run("no env keeps the inherited environment alone", func(t *testing.T) {
		out, err := run(program, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, out.code)
		assert.NotContains(t, string(out.stdout), "GATE_PROBE")
	})
	t.Run("every withheld name stays out", func(t *testing.T) {
		for _, name := range withheldNames {
			t.Setenv(name, "planted-by-the-test")
		}
		out, err := run(program, nil)
		require.NoError(t, err)
		for line := range strings.Lines(string(out.stdout)) {
			name := strings.TrimSpace(line)
			assert.NotContains(t, withheldNames, strings.ToUpper(name), "the program inherited %s", name)
		}
	})
	t.Run("NO_COLOR=1 reaches every program, over an inherited one", func(t *testing.T) {
		t.Setenv("NO_COLOR", "inherited")
		out, err := run(program, nil)
		require.NoError(t, err)
		var values []string
		for line := range strings.Lines(string(out.stdout)) {
			if value, ok := strings.CutPrefix(strings.TrimSpace(line), "NO_COLOR="); ok {
				values = append(values, value)
			}
		}
		assert.Equal(t, []string{"1"}, values)
	})
	t.Run("an absent program is an error", func(t *testing.T) {
		_, err := run(filepath.Join(t.TempDir(), "absent"), nil)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "exit status", "an absent program is not an exit status")
	})
	t.Run("an interrupt ends the program", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := commandWithin(ctx, 0)(program, nil)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestInheritedEnvironment(t *testing.T) {
	environ := []string{
		"PATH=/bin", "SHELLCHECK_OPTS=--exclude=SC2086", "BUN_OPTIONS=--preload=./planted.ts",
		"BUN_INSPECT_PRELOAD=./planted.ts", "Bun_Inspect_Preload=./planted.ts", "BUN_INSPECT=1", "BUN_INSPECT_CONNECT_TO=ws://127.0.0.1:1",
		"Gh_Token=fake", "GH_HOST=elsewhere.invalid", "KEEP=1",
	}
	tests := []struct {
		goos string
		want []string
	}{
		{goos: "windows", want: []string{"PATH=/bin", "KEEP=1"}},
		{goos: "linux", want: []string{"PATH=/bin", "Bun_Inspect_Preload=./planted.ts", "Gh_Token=fake", "KEEP=1"}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			assert.Equal(t, tt.want, inheritedEnvironment(environ, tt.goos))
		})
	}
}

// Each case is output a tool can print on a runner that forces color, and
// withoutEscapes must hand back the plain text a row matches, leaving every
// byte outside an escape sequence alone.
func TestWithoutEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text", in: "Found total 0 errors in 1 ms for ci.yml\n", want: "Found total 0 errors in 1 ms for ci.yml\n"},
		{name: "a color and its reset", in: "\x1b[31mred\x1b[0m\n", want: "red\n"},
		{name: "a color with parameters", in: "\x1b[1;38;5;208mbold\x1b[22;39m", want: "bold"},
		{name: "an erase and a cursor move", in: "\x1b[2K\x1b[1Gdone", want: "done"},
		{name: "a private-mode sequence", in: "\x1b[?25lhidden\x1b[?25h", want: "hidden"},
		{name: "a link ended by BEL", in: "\x1b]8;;https://example.invalid\x07ci.yml\x1b]8;;\x07", want: "ci.yml"},
		{name: "a link ended by ESC backslash", in: "\x1b]8;;file:///ci.yml\x1b\\ci.yml\x1b]8;;\x1b\\", want: "ci.yml"},
		{name: "an escape that opens no sequence stays", in: "a\x1bb", want: "a\x1bb"},
		{name: "a bracket with no escape stays", in: "[31m", want: "[31m"},
		{name: "a line ending in CRLF", in: "\x1b[32mok\x1b[0m\r\n", want: "ok\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(withoutEscapes([]byte(tt.in))))
		})
	}
}
