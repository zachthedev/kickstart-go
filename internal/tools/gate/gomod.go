package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// goModFindings refuses the go.mod directives that change what the gate
// checks or how its own build behaves. A replace builds a dependency, the
// gate's own included, from wherever it points, the checkout among them. A
// godebug line changes how every binary the module builds behaves, the gate's
// own process included. An ignore line takes its directories out of every
// ./... row. CI and the push hook refuse the first two before the gate is
// built, with scripts/go-mod-check.sh, and this check says the same after.
func goModFindings(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, goModule))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", goModule, err)
	}
	// Parse, not ParseLax, which skips replace, godebug and ignore as the
	// statements of a dependency's go.mod.
	file, err := modfile.Parse(goModule, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", goModule, err)
	}
	var found []string
	for _, replace := range file.Replace {
		found = append(found, fmt.Sprintf("%s replaces %s with %s, which builds that module, a dependency of the gate's own build among them, from there. Remove the replace",
			goModule, replace.Old.Path, replace.New.Path))
	}
	for _, setting := range file.Godebug {
		found = append(found, fmt.Sprintf("%s sets godebug %s=%s, which changes how every binary this module builds behaves, the gate included. Remove the line",
			goModule, setting.Key, setting.Value))
	}
	for _, ignored := range file.Ignore {
		found = append(found, fmt.Sprintf("%s ignores %s, which every ./... row then skips. Remove the line",
			goModule, ignored.Path))
	}
	return found, nil
}
