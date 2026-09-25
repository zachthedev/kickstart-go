package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// rowResult is what one row that walks the tree reports: what its tool
// checked, the findings that fail the row, and the tool's own output to show
// beside them.
type rowResult struct {
	summary  string
	findings []string
	relay    []byte
}

// shellSetting is one shell: value in a workflow: where it sits, the value as
// a string, empty when it is not one, and the value as a finding prints it.
type shellSetting struct {
	where string
	value string
	text  string
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// taploFound opens the line taplo 0.10.0 logs once it has collected the
	// files it will check. It lists them after taploFiles as a Rust debug
	// list of absolute paths. taplo logs no such line when its config
	// excludes every file it was handed.
	taploFound = "found files "
	taploFiles = "files=["
	// verbosePrefix opens the lines actionlint -verbose adds. actionlint lints
	// files in parallel and writes the prefix apart from the rest of the
	// line, so one line can carry two prefixes, or none.
	verbosePrefix = "verbose: "
	// workflowsDir is where GitHub reads workflows, one level deep.
	workflowsDir = ".github/workflows"
	// installedBin is where bun install puts each package's command, the one
	// bunx runs.
	installedBin = "node_modules/.bin"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

var (
	// actionlintFinished matches the line actionlint -verbose logs once it has
	// linted a file, "Found total 0 errors in 93 ms for <path>", behind any
	// number of prefixes.
	actionlintFinished = regexp.MustCompile(`^(?:verbose: )*Found total .* for (.+)$`)
	// allowedShells are the shell: values a workflow may name. actionlint hands
	// a run: script to ShellCheck under bash or sh alone, and pwsh is the one
	// other shell the set runs, so any other value, a command line such as
	// /bin/bash -e {0} included, runs a script no ShellCheck reads.
	allowedShells = []string{"bash", "sh", "pwsh"}
)

// ///////////////////////////////////////////////
// What the rows walk
// ///////////////////////////////////////////////

// walkedFindings refuses a tracked workflow whose extension is not a
// lowercase .yml, which actionlint's file list and zizmor's collection would
// each skip. Names compare through fold.
func walkedFindings(tracked []string) []string {
	var found []string
	for _, name := range tracked {
		ext := path.Ext(name)
		if fold(path.Dir(name)) == fold(workflowsDir) && (fold(ext) == fold(".yml") || fold(ext) == fold(".yaml")) && ext != ".yml" {
			found = append(found, fmt.Sprintf("%q is a workflow named %s, and every workflow here ends in .yml, the one spelling actionlint's own list and zizmor's collection both match. Rename it",
				name, ext))
		}
	}
	return found
}

// ///////////////////////////////////////////////
// The rows
// ///////////////////////////////////////////////

// tomlFindings runs taplo over every TOML file tracked, named on its command
// line, and holds taplo's own list of the files it checked to that list. taplo
// exits 0 having checked nothing when a named file is missing or its config
// excludes every one, so its exit code proves nothing alone. RUST_LOG is set,
// because an inherited value silences the line this check reads.
func tomlFindings(run commandRunner, taplo, root string, tracked []string) (rowResult, error) {
	var handed []string
	for _, name := range tracked {
		if fold(path.Ext(name)) == fold(".toml") {
			handed = append(handed, name)
		}
	}
	if len(handed) == 0 {
		return rowResult{findings: []string{"git tracks no TOML file, so the toml row has nothing to hand taplo and checks nothing"}}, nil
	}
	args := append([]string{"fmt", "--check", "--colors", "never", "--config", taploConfig, "--"}, handed...)
	out, err := run(taplo, []string{"RUST_LOG=info"}, args...)
	if err != nil {
		return rowResult{}, fmt.Errorf("running %s: %w", taplo, err)
	}
	result := rowResult{summary: fmt.Sprintf("taplo checked %s: %s", countFiles(len(handed), "TOML"), strings.Join(handed, ", "))}
	listed, ok, err := taploChecked(withoutEscapes(out.stderr))
	switch {
	case err != nil:
		result.findings = append(result.findings, fmt.Sprintf("taplo's found files line cannot be read back: %v", err))
	case !ok && out.code == 0:
		result.findings = append(result.findings, fmt.Sprintf("taplo printed no found files line, so it checked none of the %d TOML files the row handed it. A config that excludes every one does this", len(handed)))
	case !ok:
		// taplo stopped before it listed any file, and its exit below is the finding.
	default:
		result.findings = append(result.findings, matchChecked(root, handed, listed)...)
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("taplo exited %d", out.code))
	}
	if len(result.findings) > 0 {
		result.relay = out.stderr
	}
	return result, nil
}

// formatFindings runs Prettier's check over the tree and counts the files it
// reports checking. Prettier starts through `bun x --bun --no-install`, bunx
// under the pinned Bun, which runs the command the install put in
// node_modules/.bin under Bun rather than a node on PATH, and fetches nothing.
// --debug-check makes Prettier name each file it formats, and --check beside it
// still fails a file whose formatting differs. Prettier exits 0 over a tree
// where it matched nothing, so the count is what proves the row read anything.
// Every config Prettier would search for is named or turned off: .prettierrc,
// .prettierignore in place of the .gitignore pair, and no .editorconfig.
func formatFindings(run commandRunner, bun, root string) (rowResult, error) {
	if err := installedTool(root, "prettier", runtime.GOOS); err != nil {
		return rowResult{}, err
	}
	out, err := run(bun, nil, "x", "--bun", "--no-install", "prettier", "--check", "--debug-check",
		"--no-editorconfig", "--config", prettierrc, "--ignore-path", prettierIgnore, ".")
	if err != nil {
		return rowResult{}, fmt.Errorf("running %s: %w", bun, err)
	}
	checked := 0
	for line := range strings.Lines(string(withoutEscapes(out.stdout))) {
		name := strings.TrimRight(line, "\r\n")
		if name == "" {
			continue
		}
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); err == nil && info.Mode().IsRegular() {
			checked++
		}
	}
	result := rowResult{summary: "prettier checked " + countFiles(checked, "")}
	if checked == 0 && out.code == 0 {
		result.findings = append(result.findings, "Prettier named no file it checked, so the format row checked nothing")
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("prettier exited %d", out.code))
	}
	if len(result.findings) > 0 {
		result.relay = out.stderr
	}
	return result, nil
}

// installedTool refuses a bunx start of tool unless the command the install
// writes for it resolves, through every link, to a regular file: tool.exe on
// Windows, and tool elsewhere, where the install writes a link. Without one,
// bunx runs a copy from a parent directory's node_modules/.bin, from PATH or
// from its own cache, none of them the version bun.lock pins. A link a removed
// package left behind points at nothing.
func installedTool(root, tool, goos string) error {
	command := tool
	if goos == "windows" {
		command += ".exe"
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(installedBin), command))
	if err == nil && info.Mode().IsRegular() {
		return nil
	}
	return fmt.Errorf("%s is not installed in this checkout: run bun install --frozen-lockfile, or bun install --frozen-lockfile --ignore-scripts in a worktree (CONTRIBUTING.md#setup)", tool)
}

// actionlintFindings runs actionlint over every workflow file tracked, named
// on its command line, and holds the files its -verbose log says it finished to
// that list. shellCheck is the -shellcheck value, which starts the gate as
// ShellCheck's stand-in. It names an empty config, so no
// .github/actionlint.yaml can waive a finding. It also refuses a shell: value
// under which actionlint hands ShellCheck nothing.
// actionlint's findings and every line it prints outside -verbose reach the
// relay.
func actionlintFindings(run commandRunner, actionlint, shellCheck, root string, tracked []string) (rowResult, error) {
	handed := trackedWorkflows(tracked)
	if len(handed) == 0 {
		return rowResult{findings: []string{"git tracks no file in " + workflowsDir + ", so the workflows row has nothing to hand actionlint and checks nothing"}}, nil
	}
	shells, err := shellFindings(root, handed)
	if err != nil {
		return rowResult{}, err
	}
	dir, err := os.MkdirTemp("", "actionlint-config-")
	if err != nil {
		return rowResult{}, err
	}
	defer os.RemoveAll(dir)
	config, err := emptyActionlintConfig(dir)
	if err != nil {
		return rowResult{}, err
	}
	args := append([]string{"-shellcheck=" + shellCheck, "-pyflakes=", "-verbose", "-config-file", config, "--"}, handed...)
	out, err := run(actionlint, nil, args...)
	if err != nil {
		return rowResult{}, fmt.Errorf("running %s: %w", actionlint, err)
	}
	result := rowResult{
		summary:  fmt.Sprintf("actionlint checked %s: %s", countFiles(len(handed), "workflow"), strings.Join(handed, ", ")),
		findings: shells,
		relay:    out.stdout,
	}
	var linted []string
	for line := range strings.Lines(string(out.stderr)) {
		text := strings.TrimRight(string(withoutEscapes([]byte(line))), "\r\n")
		if match := actionlintFinished.FindStringSubmatch(text); match != nil {
			linted = append(linted, filepath.ToSlash(match[1]))
			continue
		}
		if !strings.HasPrefix(text, verbosePrefix) {
			result.relay = append(result.relay, line...)
		}
	}
	for _, name := range handed {
		if !slices.Contains(linted, name) {
			result.findings = append(result.findings, fmt.Sprintf("actionlint did not report linting %s, which the workflows row handed it", name))
		}
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("actionlint exited %d", out.code))
	}
	return result, nil
}

// shellFindings refuses every shell: value outside allowedShells in the
// workflows named, under defaults.run at the top or in a job, and on a step.
// It reads the YAML decoded, aliases resolved, so no spelling hides a value,
// and matches keys in any case, which refuses more than GitHub reads, never
// less. A workflow that does not parse is refused, since the gate cannot read
// it. A tracked file missing from the work tree passes, and actionlint's own
// list check reports it.
func shellFindings(root string, workflows []string) ([]string, error) {
	var found []string
	for _, name := range workflows {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var document any
			err := decoder.Decode(&document)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				found = append(found, fmt.Sprintf("%s does not parse as YAML, so the gate cannot read its shell: values: %v", name, err))
				break
			}
			for _, setting := range workflowShells(document) {
				if !slices.Contains(allowedShells, setting.value) {
					found = append(found, fmt.Sprintf("%s sets %s to %s, and actionlint hands a script to ShellCheck under bash or sh alone. Name one of %s",
						name, setting.where, setting.text, strings.Join(allowedShells, ", ")))
				}
			}
		}
	}
	return found, nil
}

// workflowShells lists every shell: value in one decoded workflow: under
// defaults.run at the top and in each job, and on each job's steps. A job id
// and a value come from the workflow's text, so each is escaped for the
// finding, and no control character reaches the terminal.
func workflowShells(document any) []shellSetting {
	var settings []shellSetting
	add := func(where string, node any) {
		if value, ok := lookup(node, "shell"); ok {
			text, _ := value.(string)
			settings = append(settings, shellSetting{where: where, value: text, text: strconv.Quote(fmt.Sprint(value))})
		}
	}
	add("defaults.run.shell", mappingValue(mappingValue(document, "defaults"), "run"))
	jobs := mappingValue(document, "jobs")
	for _, id := range mappingKeys(jobs) {
		job := mappingValue(jobs, id)
		shown := escaped(id)
		add("jobs."+shown+".defaults.run.shell", mappingValue(mappingValue(job, "defaults"), "run"))
		steps, _ := mappingValue(job, "steps").([]any)
		for i, step := range steps {
			add(fmt.Sprintf("jobs.%s.steps[%d].shell", shown, i), step)
		}
	}
	return settings
}

// escaped is s with every control character, quote and backslash escaped as a
// Go string literal writes it, less the quotes around it.
func escaped(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

// lookup returns the value under key in node, matched in any case, and
// whether node is a mapping holding it.
func lookup(node any, key string) (any, bool) {
	for name, value := range mappingEntries(node) {
		if strings.EqualFold(name, key) {
			return value, true
		}
	}
	return nil, false
}

// mappingValue is the value under key in node, or nil.
func mappingValue(node any, key string) any {
	value, _ := lookup(node, key)
	return value
}

// mappingKeys returns the keys of node, sorted, or none when it is not a
// mapping.
func mappingKeys(node any) []string {
	return slices.Sorted(maps.Keys(mappingEntries(node)))
}

// mappingEntries is node as a mapping with its keys written as text, or nil
// when it is not one. The decoder gives a mapping whose keys are not all
// strings, such as a job named 1, as map[any]any.
func mappingEntries(node any) map[string]any {
	switch mapping := node.(type) {
	case map[string]any:
		return mapping
	case map[any]any:
		entries := make(map[string]any, len(mapping))
		for key, value := range mapping {
			entries[fmt.Sprint(key)] = value
		}
		return entries
	}
	return nil
}

// trackedWorkflows is every tracked file directly under .github/workflows
// with a YAML extension, in any case, which the workflows rows hand their
// tools. walkedFindings refuses every spelling but .yml.
func trackedWorkflows(tracked []string) []string {
	var workflows []string
	for _, name := range tracked {
		ext := fold(path.Ext(name))
		if fold(path.Dir(name)) == fold(workflowsDir) && (ext == fold(".yml") || ext == fold(".yaml")) {
			workflows = append(workflows, name)
		}
	}
	return workflows
}

// ///////////////////////////////////////////////
// Reading taplo's list
// ///////////////////////////////////////////////

// taploChecked finds taplo's found files line in its log and returns the
// paths it lists. ok is false when no such line is there.
func taploChecked(log []byte) (paths []string, ok bool, err error) {
	for line := range strings.Lines(string(log)) {
		if !strings.Contains(line, taploFound) {
			continue
		}
		_, list, found := strings.Cut(line, taploFiles)
		if !found {
			return nil, true, errors.New("the line lists no files")
		}
		items, err := debugStrings("[" + list)
		return items, true, err
	}
	return nil, false, nil
}

// matchChecked compares the paths taplo listed with the names the row handed
// it, by file identity, so no spelling of the checkout's path matters. It
// returns a finding for each handed file taplo did not check and each file it
// checked that the row did not hand it.
func matchChecked(root string, handed, listed []string) []string {
	var found []string
	matched := make([]bool, len(handed))
	for _, listedPath := range listed {
		info, err := os.Stat(filepath.FromSlash(listedPath))
		if err != nil {
			found = append(found, fmt.Sprintf("taplo checked %q, which the gate cannot open: %v", listedPath, err))
			continue
		}
		index := slices.IndexFunc(handed, func(name string) bool {
			want, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
			return err == nil && os.SameFile(info, want)
		})
		if index < 0 {
			found = append(found, fmt.Sprintf("taplo checked %q, which the toml row did not hand it", listedPath))
			continue
		}
		matched[index] = true
	}
	for i, name := range handed {
		if !matched[i] {
			found = append(found, fmt.Sprintf("taplo did not check %s, which the toml row handed it. A file missing from the work tree, or one .taplo.toml excludes, does this", name))
		}
	}
	return found
}

// debugStrings reads a Rust debug list of strings, ["a", "b"], the form taplo
// logs its files in. It knows the two escapes a path can need, \" and \\, and
// refuses any other, so a name it cannot read back fails the row rather than
// passing unread.
func debugStrings(list string) ([]string, error) {
	rest, ok := strings.CutPrefix(list, "[")
	if !ok {
		return nil, errors.New("the list does not open with [")
	}
	var items []string
	for {
		if strings.HasPrefix(rest, "]") {
			return items, nil
		}
		item, after, err := debugString(rest)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if next, ok := strings.CutPrefix(after, ", "); ok {
			rest = next
			continue
		}
		if !strings.HasPrefix(after, "]") {
			return nil, fmt.Errorf("the list continues with %s after an item", excerpt([]byte(after)))
		}
		rest = after
	}
}

// debugString reads one quoted Rust debug string from the start of s and
// returns it with the text after its closing quote.
func debugString(s string) (string, string, error) {
	body, ok := strings.CutPrefix(s, `"`)
	if !ok {
		return "", "", fmt.Errorf("an item opens with %s, not a quote", excerpt([]byte(s)))
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		switch c := body[i]; c {
		case '"':
			return b.String(), body[i+1:], nil
		case '\\':
			if i+1 == len(body) || (body[i+1] != '"' && body[i+1] != '\\') {
				return "", "", fmt.Errorf("an item carries an escape this check does not read: %s", excerpt([]byte(body[i:])))
			}
			i++
			b.WriteByte(body[i])
		default:
			b.WriteByte(c)
		}
	}
	return "", "", errors.New("an item has no closing quote")
}

// ///////////////////////////////////////////////
// Summaries
// ///////////////////////////////////////////////

// countFiles says how many files of kind a row checked, as its summary reads:
// "3 TOML files", "1 workflow file", "29 files".
func countFiles(n int, kind string) string {
	words := []string{strconv.Itoa(n)}
	if kind != "" {
		words = append(words, kind)
	}
	if n == 1 {
		return strings.Join(append(words, "file"), " ")
	}
	return strings.Join(append(words, "files"), " ")
}
