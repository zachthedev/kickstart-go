package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// zizmorReport is the part of one finding in zizmor 1.30.1's JSON report the
// hold reads: the audit, and where the finding sits.
type zizmorReport struct {
	Ident     string           `json:"ident"`
	Locations []zizmorLocation `json:"locations"`
}

// zizmorLocation is one location of a finding. For secrets-inherit the one
// Primary location is the job's uses: value, which Feature carries, and a
// Related one is its secrets: line.
type zizmorLocation struct {
	Symbolic struct {
		Key struct {
			Local struct {
				VerbatimPath string `json:"verbatim_path"`
			} `json:"Local"`
		} `json:"key"`
		Kind string `json:"kind"`
	} `json:"symbolic"`
	Concrete struct {
		Location struct {
			StartPoint struct {
				Row int `json:"row"`
			} `json:"start_point"`
		} `json:"location"`
		Feature string `json:"feature"`
	} `json:"concrete"`
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// zizmorCompleted comes before the path on the line zizmor 1.30.1 logs at
	// info once it has audited a file. The path carries the platform's
	// separator.
	zizmorCompleted = "completed "
	// reusableWorkflows opens the call of every job that passes
	// `secrets: inherit`: zachthedev/.github's own reusable workflows, which
	// read the app's secrets.
	reusableWorkflows = "zachthedev/.github/.github/workflows/"
	// secretsInherit is the audit whose findings the hold reads.
	secretsInherit = "secrets-inherit"
)

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
// holdInheritedCalls then reads every secrets-inherit finding with no waiver
// applied.
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
	held, holdFindings, holdRelay, err := holdInheritedCalls(run, zizmor)
	if err != nil {
		return rowResult{}, err
	}
	calls := "calls"
	if held == 1 {
		calls = "call"
	}
	result := rowResult{
		summary: fmt.Sprintf("zizmor ran %s, and completed %s: %s. %d secrets-inherit %s held to %s",
			mode, countFiles(len(completed), ""), strings.Join(completed, ", "), held, calls, strings.TrimSuffix(reusableWorkflows, "/")),
		relay: slices.Concat(out.stdout, out.stderr, holdRelay),
	}
	for _, name := range workflows {
		if !slices.Contains(completed, name) {
			result.findings = append(result.findings, fmt.Sprintf("zizmor logged no completed line for %s, so it did not audit it", name))
		}
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("zizmor exited %d, running %s", out.code, mode))
	}
	result.findings = append(result.findings, holdFindings...)
	return result, nil
}

// holdInheritedCalls runs zizmor offline over .github with no config and no
// ignore of either kind, reads its JSON report, and holds the callee of every
// job that passes `secrets: inherit` to zachthedev/.github's reusable
// workflows, compared without regard to case, quotes stripped. A waiver in
// zizmor.yml binds to a file or a line, never to a callee, so a waived job can
// call anything and stay waived. This pass reads each finding before any
// waiver applies. It returns how many calls it held, the findings, and
// zizmor's output to relay beside them. zizmor exits 10 to 14 over findings,
// and any other nonzero exit means it did not audit.
func holdInheritedCalls(run commandRunner, zizmor string) (int, []string, []byte, error) {
	out, err := run(zizmor, nil, "--offline", "--no-config", "--no-ignores", "--strict-collection", "--format", "json", "--collect=all", ".github")
	if err != nil {
		return 0, nil, nil, fmt.Errorf("running %s: %w", zizmor, err)
	}
	if out.code != 0 && (out.code < 10 || out.code > 14) {
		return 0, []string{fmt.Sprintf("zizmor could not audit .github with no config (exit %d), so no secrets-inherit call is held", out.code)}, out.stderr, nil
	}
	var reports []zizmorReport
	if err := json.Unmarshal(withoutEscapes(out.stdout), &reports); err != nil {
		return 0, []string{fmt.Sprintf("zizmor's JSON report cannot be read, so no secrets-inherit call is held: %v", err)}, slices.Concat(out.stdout, out.stderr), nil
	}
	held := 0
	var found []string
	for _, report := range reports {
		if report.Ident != secretsInherit {
			continue
		}
		primary := slices.DeleteFunc(slices.Clone(report.Locations), func(location zizmorLocation) bool { return location.Symbolic.Kind != "Primary" })
		if len(primary) != 1 || primary[0].Symbolic.Key.Local.VerbatimPath == "" || primary[0].Concrete.Feature == "" {
			found = append(found, fmt.Sprintf("a secrets-inherit finding in zizmor's JSON report has no one primary location naming a file and a callee, so the hold cannot read it: %d locations", len(report.Locations)))
			continue
		}
		name := strings.ReplaceAll(primary[0].Symbolic.Key.Local.VerbatimPath, `\`, "/")
		line := primary[0].Concrete.Location.StartPoint.Row + 1
		callee := strings.Trim(primary[0].Concrete.Feature, `"'`)
		if !strings.HasPrefix(fold(callee), fold(reusableWorkflows)) {
			found = append(found, fmt.Sprintf("%s:%d passes secrets: inherit to %s, which is not one of %s's reusable workflows, and a waiver in %s binds to a file or a line, not a callee. Call %s, or pass the secrets the job needs by name",
				name, line, callee, strings.TrimSuffix(reusableWorkflows, "/.github/workflows/"), zizmorConfig, reusableWorkflows))
			continue
		}
		held++
	}
	return held, found, out.stderr, nil
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
