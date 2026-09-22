// Command gate carries the two checks `task check` runs that no tool
// provides. Each one is a named hand-rolled exception, and each file names the
// gap it closes:
//
//	gate pins                             mise.toml and mise.lock against the
//	                                      expectations in pins.go
//	gate canary <actionlint> <shellcheck> actionlint reports a ShellCheck finding
//
// Every subcommand exits 1 with one finding per line on stderr, and 0 with no
// output when the check holds. Taskfile.yml is the only caller.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run dispatches one subcommand and returns the exit code, so a test drives
// the real dispatch without touching the process.
func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "gate: a subcommand is required: pins or canary")
		return 2
	}
	var findings []string
	var err error
	switch args[0] {
	case "pins":
		findings, err = pinsFindings(".")
	case "canary":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "gate canary: the actionlint and shellcheck paths are required, in that order")
			return 2
		}
		findings, err = canaryFindings(runCommand, args[1], args[2])
	default:
		fmt.Fprintf(stderr, "gate: unknown subcommand %q: want pins or canary\n", args[0])
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
