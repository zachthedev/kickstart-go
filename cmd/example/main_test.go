package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"zach.tools/go/kickstart/internal/paths"
	"zach.tools/go/kickstart/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harness holds the substitutes every case passes to execute. kong writes
// help and version output to these streams and terminates through exit,
// so nothing here reaches the process or the terminal.
type harness struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
	codes  []int
}

// failWriter stands in for a stdout that cannot be written to.
type failWriter struct{}

// reportLines are the labels the demo report must carry, one per internal
// package it exercises.
// errExit unwinds a call that reached the exit function, standing in for
// os.Exit not returning.
var errExit = errors.New("exit")

var reportLines = []struct {
	name    string
	contain string
}{
	{name: "greeting", contain: "hello, world"},
	{name: "config path", contain: "config.toml"},
	{name: "log path", contain: ".log"},
	{name: "cache path", contain: "cache:"},
	{name: "state path", contain: "state:"},
	{name: "version label", contain: "version:"},
	{name: "docker tag label", contain: "docker tag:"},
	{name: "build settings label", contain: "build settings:"},
	{name: "remote label", contain: "remote:"},
	{name: "raw url label", contain: "raw README:"},
	{name: "generators line", contain: "generators:"},
	{name: "banner strip line", contain: "banner strip:"},
	{name: "migrate line", contain: "migrate (bytes/toml/json/sql needed):"},
	{name: "logger line", contain: "logger:"},
}

// ///////////////////////////////////////////////
// run
// ///////////////////////////////////////////////

func TestRun(t *testing.T) {
	got, err := run(t.TempDir())
	require.NoError(t, err)
	for _, tt := range reportLines {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, got, tt.contain, "run()")
		})
	}
}

func TestRun_UsesGivenBaseDir(t *testing.T) {
	base := t.TempDir()
	got, err := run(base)
	require.NoError(t, err)
	want := filepath.Join(base, "config.toml")
	assert.Contains(t, got, want)
}

func TestRun_EmptyBaseDirResolvesDefault(t *testing.T) {
	got, err := run("")
	require.NoError(t, err)
	configDir, err := paths.ConfigDir()
	require.NoError(t, err)
	want := paths.ConfigPath(configDir)
	assert.Contains(t, got, want)
}

// ///////////////////////////////////////////////
// demoCmd.Run
// ///////////////////////////////////////////////

func TestDemoCmd_Run(t *testing.T) {
	base := t.TempDir()
	var out bytes.Buffer
	require.NoError(t, (demoCmd{ConfigDir: base}).Run(&out))
	got := out.String()
	assert.True(t, strings.HasSuffix(got, "\n"), "Run output")
	assert.Contains(t, got, filepath.Join(base, "config.toml"), "Run output")
}

// ///////////////////////////////////////////////
// execute
// ///////////////////////////////////////////////

func TestExecute_DefaultCommand(t *testing.T) {
	h := &harness{}
	require.NoError(t, execute(nil, &h.stdout, &h.stderr, h.exit))
	for _, tt := range reportLines {
		assert.Contains(t, h.stdout.String(), tt.contain)
	}
	assert.Empty(t, h.codes, "the default command must not exit")
}

func TestExecute_ConfigDirFlag(t *testing.T) {
	base := t.TempDir()
	h := &harness{}
	require.NoError(t, execute([]string{"demo", "--config-dir", base}, &h.stdout, &h.stderr, h.exit))
	want := filepath.Join(base, "config.toml")
	assert.Contains(t, h.stdout.String(), want, "execute() stdout")
}

func TestExecute_VersionFlag(t *testing.T) {
	h := &harness{}
	runExpectingExit(t, h, []string{"--version"})
	assert.Contains(t, h.stdout.String(), version.Info(), "execute(--version) stdout")
	assert.Equal(t, []int{0}, h.codes, "the version flag exits once, with 0")
}

// A flag kong treats as terminal must stop the parse. Repeating it exits on
// the first one, so the value is printed once.
func TestExecute_VersionFlagExitsOnTheFirstOne(t *testing.T) {
	h := &harness{}
	runExpectingExit(t, h, []string{"--version", "--version", "--version"})
	assert.Equal(t, []int{0}, h.codes, "a terminal flag must exit exactly once")
	assert.Equal(t, 1, strings.Count(h.stdout.String(), version.Info()),
		"the version must be printed once")
}

func TestExecute_HelpFlag(t *testing.T) {
	h := &harness{}
	runExpectingExit(t, h, []string{"--help"})
	got := h.stdout.String()
	for _, want := range []string{"Usage:", paths.BinaryName, "--version", "demo"} {
		assert.Contains(t, got, want)
	}
	assert.Equal(t, []int{0}, h.codes, "the help flag exits once, with 0")
}

func TestExecute_UnknownFlag(t *testing.T) {
	h := &harness{}
	err := execute([]string{"--nope"}, &h.stdout, &h.stderr, h.exit)
	require.Error(t, err, "execute(--nope) returned nil error")
	assert.Contains(t, err.Error(), "parsing arguments", "execute(--nope) error")
	assert.Empty(t, h.stdout.String(), "a parse failure must write nothing to stdout")
}

func TestExecute_WriteError(t *testing.T) {
	h := &harness{}
	err := execute([]string{"demo", "--config-dir", t.TempDir()}, failWriter{}, &h.stderr, h.exit)
	require.Error(t, err, "execute returned nil error for a failing stdout")
}

// ///////////////////////////////////////////////
// Helpers
// ///////////////////////////////////////////////

func (h *harness) exit(code int) {
	h.codes = append(h.codes, code)
	panic(errExit)
}

// runExpectingExit calls execute and recovers the [errExit] panic, so a case
// can assert on what happened up to the exit.
func runExpectingExit(t *testing.T, h *harness, args []string) {
	t.Helper()
	defer func() {
		r := recover()
		require.NotNil(t, r, "execute(%v) returned without exiting", args)
		require.ErrorIs(t, r.(error), errExit)
	}()
	_ = execute(args, &h.stdout, &h.stderr, h.exit)
}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
