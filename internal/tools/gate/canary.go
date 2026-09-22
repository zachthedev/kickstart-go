package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// commandRunner runs one program and returns its stdout. It is a parameter so
// a test substitutes it and starts no linter.
type commandRunner func(name string, args ...string) ([]byte, error)

// shellCheckCanary is a workflow whose only finding belongs to ShellCheck.
// actionlint reads it clean on its own and reports SC2086 over the unquoted
// expansion once ShellCheck runs.
const shellCheckCanary = `name: canary
on: push
jobs:
  canary:
    runs-on: ubuntu-latest
    steps:
      - run: echo $GITHUB_REF
`

const shellCheckFinding = "SC2086"

// canaryFindings proves ShellCheck ran under actionlint. actionlint exits 0
// with ShellCheck absent or failing to start, and no flag changes that, so a
// clean run over the tree carries weight only after this finding came back.
// -pyflakes= turns the Python pass off, because nothing pins pyflakes and
// actionlint skips that pass without a word when it is missing.
func canaryFindings(run commandRunner, actionlint, shellcheck string) ([]string, error) {
	dir, err := os.MkdirTemp("", "actionlint-canary-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	workflow := filepath.Join(dir, "canary.yml")
	if err := os.WriteFile(workflow, []byte(shellCheckCanary), 0o600); err != nil {
		return nil, err
	}
	// A finding makes actionlint exit 1, which is the outcome this check wants.
	out, err := run(actionlint, "-shellcheck="+shellcheck, "-pyflakes=", workflow)
	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && !exited {
		return nil, fmt.Errorf("running %s: %w", actionlint, err)
	}
	if !bytes.Contains(out, []byte(shellCheckFinding)) {
		return []string{fmt.Sprintf("actionlint found no %s in a script that carries one, so ShellCheck never ran. Check that %s starts. actionlint printed: %s", shellCheckFinding, shellcheck, out)}, nil
	}
	return nil, nil
}

// runCommand is the commandRunner that starts the program. The name is the
// path Taskfile.yml resolved through `mise which`, which is the point: the
// gate runs the pinned binary and nothing off PATH.
func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output() //nolint:gosec // the caller names the pinned binary on purpose
}
