// Command gate carries the checks `task check` runs that no tool provides,
// the rows whose tools cannot say by themselves what they checked, and the one
// way the gate starts mise. Each is a named hand-rolled exception, and each
// file names the gap it closes:
//
//	gate pins                                 mise.toml, mise.lock and the
//	                                          tree around them, against
//	                                          pins.go, configs.go and
//	                                          startup.go
//	gate canary <actionlint> <shellcheck>     actionlint reports a ShellCheck
//	                                          finding and the stand-in's
//	                                          refusal of a directive
//	gate toml <taplo>                         taplo over every tracked TOML
//	                                          file, checked against its list
//	gate format                               Prettier over the tree, counted
//	gate actionlint <actionlint> <shellcheck> actionlint over every tracked
//	                                          workflow, checked against its
//	                                          list, with no shell: value
//	                                          ShellCheck skips
//	gate scripts <shellcheck>                 ShellCheck over every tracked
//	                                          .sh file, checked against its
//	                                          list, each directive named and
//	                                          reasoned
//	gate zizmor <zizmor>                      zizmor over .github, online when
//	                                          gh holds a token, checked against
//	                                          every tracked workflow
//	gate packages -platforms <pairs>          the packages ./... matches,
//	  -tags <sets> -release <pairs>           counted, with no Go file
//	  [<generated>...]                        outside every build lint and
//	                                          vet read, no lax generated
//	                                          marker outside the files
//	                                          cmd/generate writes, and no
//	                                          inline waiver no linter checks
//	                                          or whose reason is invisible
//	go test -json ... | gate tests            the tests go test ran, failing
//	                                          when none ran, every one
//	                                          skipped or one failed
//	gate mise <args>                          mise, once the pins checks pass,
//	                                          under an environment built from
//	                                          an allow-list
//
// pins and canary exit 1 with one finding per line on stderr, and 0 with no
// output when the check holds. toml, format, actionlint, scripts, zizmor and
// packages print what their tool checked and exit 0, or exit 1 with the
// tool's output and the findings on stderr. mise exits 1 the same way when a
// pins check fails, and otherwise with mise's own code. Every program the gate
// starts runs under process.go's limits. Taskfile.yml is the caller, and CI
// and the push hook run pins on its own before any task starts. The canary
// and actionlint rows also start the gate itself, under actionlint, as
// standin.go's ShellCheck stand-in.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	// Every program the gate starts leads a process tree of its own, which a
	// terminal's interrupt does not reach, so the gate ends each tree itself.
	// A second interrupt ends the gate at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches one subcommand and returns the exit code, so a test drives
// the real dispatch without touching the process. ctx ends every program the
// subcommand starts. stdin reaches the tests row and the ShellCheck stand-in
// alone.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "gate: a subcommand is required: pins, canary, toml, format, actionlint, scripts, zizmor, packages, tests or mise")
		return 2
	}
	switch args[0] {
	case standInArgument:
		return standIn(ctx, args[1:], stdin, stdout, stderr)
	case "tests":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "gate tests: go test -json arrives on stdin, and the row takes no argument")
			return 2
		}
		return testsRow(stdin, stdout, stderr)
	}
	if code, ok := walkCommand(ctx, args, stdout, stderr); ok {
		return code
	}
	var findings []string
	var err error
	switch args[0] {
	case "pins":
		findings, err = pinsFindings(".", gitLister(ctx))
	case "canary":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "gate canary: the actionlint and shellcheck paths are required, in that order")
			return 2
		}
		var shellCheck string
		if shellCheck, err = shellCheckCommand(args[2]); err == nil {
			findings, err = canaryFindings(commandWithin(ctx, 0), args[1], shellCheck)
		}
	case "mise":
		return runMise(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "gate: unknown subcommand %q: want pins, canary, toml, format, actionlint, scripts, zizmor, packages, tests or mise\n", args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "gate %s: %v\n", args[0], err)
		return 2
	}
	if len(findings) > 0 {
		fmt.Fprintln(stderr, strings.Join(findings, "\n"))
		return 1
	}
	return 0
}

// walkCommand dispatches the subcommands that walk the tree from the working
// directory and returns the exit code, with ok false for any other
// subcommand.
func walkCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (code int, ok bool) {
	tools := commandWithin(ctx, 0)
	tracked := func(row func(root string, tracked []string) (rowResult, error)) func(root string) (rowResult, error) {
		return func(root string) (rowResult, error) {
			paths, err := gitLister(ctx)(root)
			if err != nil {
				return rowResult{}, fmt.Errorf("listing tracked files: %w", err)
			}
			return row(root, paths)
		}
	}
	usage := func(message string) (int, bool) {
		fmt.Fprintf(stderr, "gate %s: %s\n", args[0], message)
		return 2, true
	}
	switch args[0] {
	case "toml":
		if len(args) != 2 {
			return usage("the taplo path is required")
		}
		return walk(stdout, stderr, args[0], tracked(func(root string, paths []string) (rowResult, error) {
			return tomlFindings(tools, args[1], root, paths)
		})), true
	case "format":
		return walk(stdout, stderr, args[0], func(root string) (rowResult, error) {
			bun, err := resolveProgram("bun", root)
			if err != nil {
				return rowResult{}, err
			}
			return formatFindings(tools, bun, root)
		}), true
	case "actionlint":
		if len(args) != 3 {
			return usage("the actionlint and shellcheck paths are required, in that order")
		}
		return walk(stdout, stderr, args[0], tracked(func(root string, paths []string) (rowResult, error) {
			shellCheck, err := shellCheckCommand(args[2])
			if err != nil {
				return rowResult{}, err
			}
			return actionlintFindings(tools, args[1], shellCheck, root, paths)
		})), true
	case "scripts":
		if len(args) != 2 {
			return usage("the shellcheck path is required")
		}
		return walk(stdout, stderr, args[0], tracked(func(root string, paths []string) (rowResult, error) {
			return scriptsFindings(tools, args[1], root, paths)
		})), true
	case "zizmor":
		if len(args) != 2 {
			return usage("the zizmor path is required")
		}
		return walk(stdout, stderr, args[0], tracked(func(root string, paths []string) (rowResult, error) {
			// A gh missing from PATH, or found inside the checkout, holds no
			// token the gate will use.
			gh, _ := resolveProgram("gh", root)
			return zizmorFindings(tools, commandWithin(ctx, ghTimeout), gh, args[1], paths)
		})), true
	case "packages":
		cover, generated, err := parseCoverage(args[1:])
		if err != nil {
			return usage(err.Error() + ". Taskfile.yml passes -platforms, -tags and -release, then the generated files")
		}
		return walk(stdout, stderr, args[0], tracked(func(root string, paths []string) (rowResult, error) {
			goTool, err := resolveProgram("go", root)
			if err != nil {
				return rowResult{}, err
			}
			return packagesFindings(tools, goTool, root, paths, cover, generated)
		})), true
	}
	return 0, false
}

// shellCheckCommand is the -shellcheck value that starts this binary as
// ShellCheck's stand-in in front of shellcheck.
func shellCheckCommand(shellcheck string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the gate's own binary for the ShellCheck stand-in: %w", err)
	}
	return standInCommand(self, shellcheck)
}

// walk runs one row that walks the tree from the working directory. It prints
// what the row's tool checked and returns 0, or prints the tool's output and
// the findings on stderr and returns 1.
func walk(stdout, stderr io.Writer, name string, row func(root string) (rowResult, error)) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "gate %s: reading the working directory: %v\n", name, err)
		return 2
	}
	result, err := row(root)
	if err != nil {
		fmt.Fprintf(stderr, "gate %s: %v\n", name, err)
		return 2
	}
	if len(result.findings) > 0 {
		_, _ = stderr.Write(result.relay)
		fmt.Fprintln(stderr, strings.Join(result.findings, "\n"))
		return 1
	}
	fmt.Fprintln(stdout, result.summary)
	return 0
}
