package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// noColor goes into the environment of every program commandWithin
	// starts, after what the program inherits, so it wins over an inherited
	// NO_COLOR. A tool that honors it prints plain text. One that colors its
	// output anyway, as some do when CI is set or FORCE_COLOR asks, is read
	// through withoutEscapes.
	noColor = "NO_COLOR=1"
	// pipeGrace is the WaitDelay of every program the gate starts: once the
	// program exits, a process it started has this long to release its
	// output before Wait closes the pipes and returns exec.ErrWaitDelay. The
	// gate sets no deadline of its own on a row. CI's gate job carries
	// timeout-minutes, and an interrupt ends the program.
	pipeGrace = 5 * time.Second
	// ghTimeout bounds `gh auth token`, past which the zizmor row reads gh as
	// holding no token and runs offline.
	ghTimeout = 5 * time.Second
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

var (
	// terminalEscape matches the two escape sequences a tool writes to color
	// or link its output: an ECMA-48 control sequence (ESC [, then parameter,
	// intermediate and final bytes) and an operating system command (ESC ],
	// ended by BEL or by ESC \).
	terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

	// withheldNames are the variables no program the gate starts inherits.
	// SHELLCHECK_OPTS adds arguments to every ShellCheck run, actionlint's
	// included, where an --exclude from the shell or a .env silences a finding.
	// BUN_OPTIONS adds command-line flags to every Bun process, a --preload
	// among them, which runs code before Prettier's first line, and
	// BUN_INSPECT_PRELOAD runs a module in every Bun start. BUN_INSPECT and
	// BUN_INSPECT_CONNECT_TO open Bun's inspector to a debugger.
	// The token names are the ones gh, zizmor and mise read a GitHub token
	// from: gh gets its own two back for `gh auth token` alone, and zizmor gets
	// the token gh answers with. GH_HOST would point gh and zizmor at another
	// host, and the two ZIZMOR_ names turn the online audits off whatever mode
	// the zizmor row prints.
	withheldNames = []string{
		"SHELLCHECK_OPTS", "BUN_OPTIONS", "BUN_INSPECT_PRELOAD", "BUN_INSPECT", "BUN_INSPECT_CONNECT_TO",
		"GH_TOKEN", "GITHUB_TOKEN", "GITHUB_API_TOKEN", "ZIZMOR_GITHUB_TOKEN",
		"MISE_GITHUB_TOKEN", "MISE_GITHUB_ENTERPRISE_TOKEN",
		"GH_HOST", "ZIZMOR_OFFLINE", "ZIZMOR_NO_ONLINE_AUDITS",
	}
)

// ///////////////////////////////////////////////
// Running a program
// ///////////////////////////////////////////////

// commandWithin returns the commandRunner that starts each program inside
// ctx, ended at timeout when timeout is not zero, with pipeGrace as its
// WaitDelay and the gate's environment less withheldNames, then noColor, then
// env. The name is an absolute path, one Taskfile.yml resolved through `mise
// which` or one resolveProgram found, which is the point: the gate runs the
// pinned binary and nothing a bare name finds. A nonzero exit is output. A
// program ended at timeout or on an interrupt is an error wrapping the
// context's, and so is any other failure, a pipe still held past pipeGrace
// included.
func commandWithin(ctx context.Context, timeout time.Duration) commandRunner {
	return func(name string, env []string, args ...string) (output, error) {
		runCtx := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		cmd := exec.CommandContext(runCtx, name, args...) // #nosec G702 -- the caller names the pinned binary on purpose
		cmd.Env = slices.Concat(inheritedEnvironment(os.Environ(), runtime.GOOS), []string{noColor}, env)
		cmd.WaitDelay = pipeGrace
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if ended := runCtx.Err(); ended != nil {
			return output{}, fmt.Errorf("the gate ended %s: %w", filepath.Base(name), ended)
		}
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return output{stdout: []byte(stdout.String()), stderr: []byte(stderr.String()), code: exit.ExitCode()}, nil
		}
		if err != nil {
			return output{}, err
		}
		return output{stdout: []byte(stdout.String()), stderr: []byte(stderr.String())}, nil
	}
}

// withoutEscapes returns out with every terminalEscape removed. A row reads a
// tool's output through it before matching a line, since a tool can color on
// a CI runner what it prints plain here, and a colored line matches nothing
// the row looks for.
func withoutEscapes(out []byte) []byte {
	return terminalEscape.ReplaceAll(out, nil)
}

// inheritedEnvironment is environ less every entry withheldNames names,
// compared the way goos compares variable names.
func inheritedEnvironment(environ []string, goos string) []string {
	return slices.DeleteFunc(slices.Clone(environ), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return slices.ContainsFunc(withheldNames, func(withheld string) bool { return sameName(name, withheld, goos) })
	})
}
