package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// output is what one program printed and the code it exited with.
type output struct {
	stdout []byte
	stderr []byte
	code   int
}

// commandRunner runs one program to completion, with env added to the
// environment the gate inherited less withheldNames, and returns what it
// printed. An error means the program did not run, or did not finish within
// its limits. An exit code is part of the output. It is a parameter so a test
// substitutes it and starts no tool.
type commandRunner func(name string, env []string, args ...string) (output, error)

// canary is one workflow the canary check hands actionlint, and the text its
// findings must carry.
type canary struct {
	name    string
	text    string
	want    string
	missing string
}

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// canaries prove ShellCheck runs behind the stand-in and the stand-in runs in
// front of it. The first carries an unquoted expansion, which actionlint reads
// clean on its own and ShellCheck reports as SC2086. The second disables that
// code with a directive, which only the stand-in refuses.
var canaries = []canary{
	{
		name: "canary.yml",
		text: `name: canary
on: push
jobs:
  canary:
    runs-on: ubuntu-latest
    steps:
      - run: echo $GITHUB_REF
`,
		want:    "SC2086",
		missing: "actionlint found no SC2086 in a script that carries one, so ShellCheck never ran behind the stand-in",
	},
	{
		name: "directive.yml",
		text: `name: canary
on: push
jobs:
  canary:
    runs-on: ubuntu-latest
    steps:
      - run: |
          # shellcheck disable=SC2086
          echo $GITHUB_REF
`,
		want:    "the gate refuses a ShellCheck directive",
		missing: "actionlint reported no refusal of a ShellCheck directive, so the stand-in never ran in front of ShellCheck",
	},
}

// ///////////////////////////////////////////////
// The check
// ///////////////////////////////////////////////

// canaryFindings proves the -shellcheck value the workflows row passes,
// shellCheck, puts the stand-in in front of ShellCheck. actionlint exits 0
// with ShellCheck absent or failing to start, and no flag changes that, so a
// clean run over the tree carries weight only after both canaries came back.
// -pyflakes= turns the Python pass off, because nothing pins pyflakes and
// actionlint skips that pass without a word when it is missing. The canaries
// run under the same config as the workflows row, and actionlint starts
// ShellCheck with --norc, so no .shellcheckrc reaches either.
func canaryFindings(run commandRunner, actionlint, shellCheck string) ([]string, error) {
	dir, err := os.MkdirTemp("", "actionlint-canary-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	config, err := emptyActionlintConfig(dir)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, canary := range canaries {
		workflow := filepath.Join(dir, canary.name)
		if err := os.WriteFile(workflow, []byte(canary.text), 0o600); err != nil {
			return nil, err
		}
		// A finding makes actionlint exit 1, which is the outcome this check
		// wants. Any other exit means actionlint failed, and the finding says
		// so rather than guess what ShellCheck did.
		out, err := run(actionlint, nil, "-shellcheck="+shellCheck, "-pyflakes=", "-config-file", config, workflow)
		if err != nil {
			return nil, fmt.Errorf("running %s: %w", actionlint, err)
		}
		switch {
		case out.code != 0 && out.code != 1:
			found = append(found, fmt.Sprintf("actionlint exited %d over %s, so the canary proves nothing. actionlint printed: %s%s", out.code, canary.name, out.stdout, out.stderr))
		case !bytes.Contains(withoutEscapes(out.stdout), []byte(canary.want)):
			found = append(found, fmt.Sprintf("%s. Check that %s starts. actionlint printed: %s%s", canary.missing, shellCheck, out.stdout, out.stderr))
		}
	}
	return found, nil
}

// emptyActionlintConfig writes an empty actionlint config into dir and returns
// its path. Named with -config-file, it stops actionlint reading
// .github/actionlint.yaml or .yml, whose paths block can waive any finding.
func emptyActionlintConfig(dir string) (string, error) {
	config := filepath.Join(dir, "actionlint.yaml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		return "", err
	}
	return config, nil
}
