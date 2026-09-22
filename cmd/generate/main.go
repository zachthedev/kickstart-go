// Command generate runs every registered generator in
// [generate.Default] and writes its
// outputs. Intended to be invoked from go:generate directives and from the
// pre-commit hook.
//
// Subcommands:
//
//	generate run                       write every registered output
//	generate run <path> [<path>...]    write only the listed outputs
//	generate list outputs              print every registered output path
//	generate list inputs               print every registered input pattern
//	generate list input-regexp         print those patterns as one regexp
//
// With no arguments, generate defaults to "run".
package main

//go:generate go run .

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"zach.tools/go/kickstart/internal/buildenv"
	"zach.tools/go/kickstart/internal/generate"
)

// The project's registered outputs. cmd/generate owns this list so
// internal/generate stays free of project-specific entries.
//
// TODO(kickstart): register your project's generated files here.
func init() {
	// README.md explains the marker convention without spelling the marker,
	// so no file is skipped: every match is a site.
	generate.Default.Register(generate.OutputEntry{
		Path:     "MARKERS.md",
		Inputs:   []string{"**/*.go", "**/*.md", "**/*.yml", "**/*.json", "**/*.toml", "**/*.sh", "Taskfile.yml", "go.mod"},
		Generate: generate.MarkerTable{Root: ".", ProjectName: "kickstart"}.Generate,
	})
	generate.Default.Register(generate.OutputEntry{
		Path:     ".env.template",
		Inputs:   []string{"internal/buildenv/*.go"},
		Template: true,
		Generate: generate.DotEnv{ProjectName: "kickstart", Vars: envVars()}.Generate,
	})
}

// envVars copies internal/buildenv's declarations into the generator's own
// type, so the package that owns the schema does not import the generator.
func envVars() []generate.EnvVar {
	declared := buildenv.Variables()
	out := make([]generate.EnvVar, len(declared))
	for i, v := range declared {
		out[i] = generate.EnvVar{
			Name:    v.Name,
			Purpose: v.Purpose,
			Example: v.Example,
			Absent:  v.Absent,
		}
	}
	return out
}

func main() {
	if err := dispatch(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ensureProjectRoot walks up from cwd to find the directory containing
// go.mod and chdirs there so OutputEntry paths resolve consistently
// whether generate is invoked from a go:generate directive (cwd = package
// dir) or from the project root (cwd = root already).
func ensureProjectRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	start := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if dir == start {
				return nil
			}
			return os.Chdir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("go.mod not found in any ancestor of %s", start)
		}
		dir = parent
	}
}

// dispatch parses argv and runs the matching subcommand. Factored out of
// main so tests can exercise it.
func dispatch(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "run" {
		if err := ensureProjectRoot(); err != nil {
			return err
		}
		var paths []string
		if len(args) > 0 {
			paths = args[1:]
		}
		return generate.Default.Run(paths...)
	}
	switch args[0] {
	case "list":
		return dispatchList(args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q; try 'generate help'", args[0])
	}
}

func dispatchList(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("list requires a target: outputs or inputs")
	}
	switch args[0] {
	case "outputs":
		for _, p := range generate.Default.Outputs() {
			fmt.Fprintln(stdout, p)
		}
	case "inputs":
		for _, p := range generate.Default.Inputs() {
			fmt.Fprintln(stdout, p)
		}
	case "input-regexp":
		fmt.Fprintln(stdout, generate.Default.InputRegexp())
	default:
		return fmt.Errorf("unknown list target %q; want outputs, inputs or input-regexp", args[0])
	}
	return nil
}

func printUsage(stdout io.Writer) {
	fmt.Fprintln(stdout, `Usage: generate [subcommand]

Subcommands:
  run [path...]    Write registered outputs. With no paths, writes every
                   registered output. With one or more paths, writes only
                   the listed entries (each must be a registered path).
  list outputs     Print every registered output path, one per line.
  list inputs      Print every registered input pattern, one per line.
  list input-regexp
                   Print those patterns as one extended regular expression,
                   for matching against a list of paths.
  help             Show this help.`)
}
