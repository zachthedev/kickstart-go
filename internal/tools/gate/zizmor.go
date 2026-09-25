package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// zizmorCompleted comes before the path on the line zizmor 1.30.1 logs at info
// once it has audited a file. The path carries the platform's separator.
const zizmorCompleted = "completed "

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// ghTokenNames are the variables gh reads a token from before its own store.
// Every other program the gate starts goes without them.
var ghTokenNames = []string{"GH_TOKEN", "GITHUB_TOKEN"}

// ///////////////////////////////////////////////
// The row
// ///////////////////////////////////////////////

// zizmorFindings runs zizmor over .github with every ignore rule off, and
// holds the files its log says it completed to every tracked workflow. zizmor
// exits 0 over input it never read, so its exit code proves nothing alone.
// The shared workflows job holds the callee of every job that passes
// `secrets: inherit`, which a waiver in .github/zizmor.yml cannot bind.
//
// zizmor runs online when `gh auth token` answers within ghTimeout, because
// some audits read the pinned actions' repositories, and offline otherwise, so
// a gh that hangs cannot hold the gate. The summary says which, since a green
// offline run skipped those audits. The token reaches zizmor alone, and ask
// runs gh. gh is empty when PATH holds no gh outside the checkout.
//
// The config is named, so no other zizmor config applies, and
// --strict-collection fails on a file zizmor cannot parse rather than dropping
// it. --collect=all turns off the .gitignore files, .git/info/exclude and the
// global excludes file that collecting a directory otherwise honors, so no
// ignore line hides a workflow. Over .github it still collects dependabot.yml
// and .github/actions without walking node_modules or a worktree. RUST_LOG is
// set, because an inherited value silences the completed lines.
func zizmorFindings(run, ask commandRunner, gh, zizmor string, tracked []string) (rowResult, error) {
	workflows := trackedWorkflows(tracked)
	if len(workflows) == 0 {
		return rowResult{findings: []string{"git tracks no file in " + workflowsDir + ", so the zizmor row has no workflow to hold zizmor to and checks nothing"}}, nil
	}
	env := []string{"RUST_LOG=info"}
	args := []string{"--no-progress", "--strict-collection", "--config", zizmorConfig}
	mode := "offline, since gh auth token did not answer"
	if token := ghToken(ask, gh); token != "" {
		env = append(env, "GH_TOKEN="+token)
		mode = "online, since gh auth token answered"
	} else {
		args = append(args, "--offline")
	}
	out, err := run(zizmor, env, append(args, "--collect=all", ".github")...)
	if err != nil {
		return rowResult{}, fmt.Errorf("running %s: %w", zizmor, err)
	}
	var completed []string
	for line := range strings.Lines(string(withoutEscapes(out.stderr))) {
		if _, name, ok := strings.Cut(strings.TrimRight(line, "\r\n"), zizmorCompleted); ok {
			completed = append(completed, strings.ReplaceAll(name, `\`, "/"))
		}
	}
	result := rowResult{
		summary: fmt.Sprintf("zizmor ran %s, and completed %s: %s", mode, countFiles(len(completed), ""), strings.Join(completed, ", ")),
		relay:   slices.Concat(out.stdout, out.stderr),
	}
	for _, name := range workflows {
		if !slices.Contains(completed, name) {
			result.findings = append(result.findings, fmt.Sprintf("zizmor logged no completed line for %s, so it did not audit it", name))
		}
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("zizmor exited %d, running %s", out.code, mode))
	}
	return result, nil
}

// ghToken returns the token `gh auth token` answers with, or nothing when gh
// is absent, fails, prints nothing, outlives ghTimeout or leaves a process
// holding its output. gh gets the token
// variables every other program goes without, so it answers as it would in
// the contributor's shell. Nothing prints the token.
func ghToken(ask commandRunner, gh string) string {
	if gh == "" {
		return ""
	}
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if slices.ContainsFunc(ghTokenNames, func(want string) bool { return sameName(name, want, runtime.GOOS) }) {
			env = append(env, entry)
		}
	}
	out, err := ask(gh, env, "auth", "token")
	if err != nil || out.code != 0 {
		return ""
	}
	return strings.TrimSpace(string(out.stdout))
}
