package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// shellCheckReport is one finding in ShellCheck's JSON output, the form
// actionlint reads back from the program its -shellcheck flag names.
type shellCheckReport struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Level   string `json:"level"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// standInArgument is the first argument that starts the gate as ShellCheck's
// stand-in. The gate names it in actionlint's -shellcheck value alone, and no
// usage line lists it.
const standInArgument = "shellcheck-stand-in"

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// shellCheckDirective matches a ShellCheck directive in one line of a script:
// #, any space ShellCheck reads as line whitespace, the word shellcheck, and
// one more such space. ShellCheck 0.11.0 reads the word in lowercase alone
// (src/ShellCheck/Parser.hs), so matching it in any case refuses more than
// ShellCheck honors, never less.
var shellCheckDirective = regexp.MustCompile(`(?i)#[\s\x{00a0}\x{2002}-\x{2009}\x{200b}\x{202f}]*shellcheck[\s\x{00a0}\x{2002}-\x{2009}\x{200b}\x{202f}]`)

// ///////////////////////////////////////////////
// The stand-in
// ///////////////////////////////////////////////

// standIn is the gate as ShellCheck under actionlint's -shellcheck flag. args
// is the pinned ShellCheck's path and then ShellCheck's own arguments, and
// stdin is one run: script, decoded from the workflow's YAML with every
// expression blanked, which no regex over the workflow file sees through
// escapes and folding. A line carrying a ShellCheck directive comes back as
// an error finding in ShellCheck's JSON form, and ShellCheck does not run.
// Otherwise ShellCheck runs over the same bytes without SHELLCHECK_OPTS, its
// stderr and exit code pass through, and its stdout passes once it exited 0 or
// 1, having read and checked the script. Any other exit keeps its stdout back,
// since actionlint reads a failed run's `[]` as a clean one. A failure of the
// stand-in's own exits 2 with nothing on stdout, which actionlint reports as a
// failed run.
func standIn(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "gate: the ShellCheck stand-in needs the ShellCheck path")
		return 2
	}
	script, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "gate: reading the script: %v\n", err)
		return 2
	}
	if refused := directiveReports(script); len(refused) > 0 {
		data, err := json.Marshal(refused)
		if err != nil {
			fmt.Fprintf(stderr, "gate: encoding the refusal: %v\n", err)
			return 2
		}
		_, _ = stdout.Write(data)
		return 1
	}
	out, code, err := runShellCheck(ctx, args, script, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gate: running %s: %v\n", args[0], err)
		return 2
	}
	if code != 0 && code != 1 {
		fmt.Fprintf(stderr, "gate: %s exited %d, so its output is held back\n", args[0], code)
		return code
	}
	_, _ = io.WriteString(stdout, out)
	return code
}

// runShellCheck runs args, ShellCheck and its arguments, over script and
// returns its stdout and exit code, with its stderr written to stderr. The
// script is written on a goroutine of its own through a pipe the stand-in
// holds, since os/exec drops the broken pipe a ShellCheck that exits before
// reading its input leaves. A write that fails is an error: ShellCheck did not
// read the whole script. A script the pipe buffers whole is written before
// ShellCheck reads it, so only a longer one can show the failure.
func runShellCheck(ctx context.Context, args []string, script []byte, stderr io.Writer) (string, int, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G204 G702 -- actionlint passes back the ShellCheck path the gate named in its -shellcheck value
	cmd.Env = inheritedEnvironment(os.Environ(), runtime.GOOS)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = pipeGrace
	input, err := cmd.StdinPipe()
	if err != nil {
		return "", 0, err
	}
	if err := cmd.Start(); err != nil {
		return "", 0, err
	}
	written := make(chan error, 1)
	go func() {
		_, err := input.Write(script)
		written <- errors.Join(err, input.Close())
	}()
	err = cmd.Wait()
	_, _ = io.WriteString(stderr, errOut.String())
	code := 0
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		return "", 0, err
	}
	if err := <-written; err != nil {
		return "", 0, fmt.Errorf("ShellCheck did not read the whole script: %w", err)
	}
	return out.String(), code, nil
}

// directiveReports returns a finding for each line of script that carries a
// ShellCheck directive, numbered from 1 as ShellCheck numbers its input.
func directiveReports(script []byte) []shellCheckReport {
	var refused []shellCheckReport
	for i, line := range strings.Split(string(script), "\n") {
		if shellCheckDirective.MatchString(line) {
			refused = append(refused, shellCheckReport{
				Line:   i + 1,
				Column: 1,
				Level:  "error",
				Message: fmt.Sprintf("the gate refuses a ShellCheck directive in a workflow script, since it silences ShellCheck for the lines after it: %q. Rewrite the script so ShellCheck passes",
					strings.TrimSpace(line)),
			})
		}
	}
	return refused
}

// standInCommand is the -shellcheck value that makes actionlint start self,
// the running gate, as ShellCheck's stand-in over the pinned shellcheck.
// actionlint splits the value into words, where an unquoted backslash is an
// escape and a path with one resolves to nothing, which turns ShellCheck off
// without a word. So each word is single-quoted with forward slashes, and a
// path holding a single quote, which that quoting cannot carry, is an error.
func standInCommand(self, shellcheck string) (string, error) {
	words := make([]string, 0, 3)
	for _, word := range []string{self, standInArgument, shellcheck} {
		word = filepath.ToSlash(word)
		if strings.Contains(word, "'") {
			return "", fmt.Errorf("%s holds a single quote, which the -shellcheck value cannot carry", word)
		}
		words = append(words, "'"+word+"'")
	}
	return strings.Join(words, " "), nil
}
