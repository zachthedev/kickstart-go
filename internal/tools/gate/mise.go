package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// urlReplacementsEnv carries the url_replacements rule to every mise command
// the gate runs. The environment replaces the whole map and outranks every
// config file, so no committed config file can lift the rule there.
const urlReplacementsEnv = "MISE_URL_REPLACEMENTS"

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// miseInherited names, per operating system, the only variables a mise child
// takes from the environment it inherits. On Windows mise 2026.9.11 needs
// SystemRoot, LOCALAPPDATA and a temporary directory: systemFolders supplies
// the first two from the system, and TEMP and TMP come through. Elsewhere mise
// derives every directory from HOME and the temporary one from TMPDIR, and
// reads no PATH for an aqua install. The proxy variables let a machine behind
// a proxy fetch, and no certificate override comes through. Every MISE_ name,
// XDG_ directory and token stays out, whatever set it: a .env Taskfile.yml
// loads, the shell or CI.
var miseInherited = map[string][]string{
	"windows": {"TEMP", "TMP", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"},
	"unix":    {"HOME", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"},
}

// ///////////////////////////////////////////////
// The runner
// ///////////////////////////////////////////////

// runMise runs mise with args from the working directory, under the
// environment miseEnvironment builds, inside ctx with pipeGrace as its
// WaitDelay, and returns mise's exit code. It runs the pins checks first, because that environment trusts the
// checkout and mise renders a trusted config's templates on load: a finding
// exits 1 with the findings on stderr, and mise never starts.
func runMise(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "gate mise: reading the working directory: %v\n", err)
		return 2
	}
	findings, err := pinsFindings(root, gitLister(ctx))
	if err != nil {
		fmt.Fprintf(stderr, "gate mise: %v\n", err)
		return 2
	}
	if len(findings) > 0 {
		fmt.Fprintln(stderr, strings.Join(findings, "\n"))
		return 1
	}
	program, err := resolveProgram("mise", root)
	if err != nil {
		fmt.Fprintf(stderr, "gate mise: %v\n", err)
		return 2
	}
	folders, err := systemFolders()
	if err != nil {
		fmt.Fprintf(stderr, "gate mise: %v\n", err)
		return 2
	}
	cmd := exec.CommandContext(ctx, program, args...) // #nosec G702 -- resolveProgram returns an absolute path outside the checkout
	cmd.Env = miseEnvironment(os.Environ(), runtime.GOOS, root, folders)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = pipeGrace
	err = cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintf(stderr, "gate mise: running %s: %v\n", program, err)
		return 2
	}
	return 0
}

// miseEnvironment builds a mise child's whole environment: the names
// miseInherited allows for goos, copied from environ, the folders the system
// named, then what the gate sets. That leaves mise.toml as the one config file
// mise reads, with no .tool-versions, no environment layer and no per-platform
// layer. It sends every url_api fetch nowhere, and it trusts the checkout at
// root, whose mise.toml runMise has just held to data alone. `mise which`
// refuses an untrusted checkout, and naming the one path trusts nothing else.
func miseEnvironment(environ []string, goos, root string, folders map[string]string) []string {
	allowed := miseInherited["unix"]
	if goos == "windows" {
		allowed = miseInherited["windows"]
	}
	var env []string
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		if slices.ContainsFunc(allowed, func(want string) bool { return sameName(name, want, goos) }) {
			env = append(env, entry)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(folders)) {
		env = append(env, name+"="+folders[name])
	}
	rule, _ := json.Marshal(map[string]string{urlReplacementKey: urlReplacementTarget})
	return append(env,
		"MISE_OVERRIDE_CONFIG_FILENAMES="+pinsPath,
		"MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES=none",
		"MISE_ENV=",
		"MISE_AUTO_ENV=false",
		urlReplacementsEnv+"="+string(rule),
		"MISE_TRUSTED_CONFIG_PATHS="+root,
	)
}

// sameName compares two variable names the way goos does: without regard to
// case on Windows, exactly elsewhere.
func sameName(a, b, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolveProgram finds name on PATH and returns its absolute path. exec.LookPath
// already refuses a match in the working directory. The result must also be
// absolute, and no directory above it may be root, so a PATH entry naming the
// checkout, or GODEBUG=execerrdot=0, cannot hand the gate a committed file.
// Directories compare by file identity rather than by spelling, which an 8.3
// name or a subst drive changes. The walk runs over the found path and over
// its final path, every link and junction followed, because a PATH entry
// outside the checkout can link into a directory below it.
func resolveProgram(name, root string) (string, error) {
	found, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("finding %s on PATH: %w", name, err)
	}
	if !filepath.IsAbs(found) {
		return "", fmt.Errorf("%s resolved to %q, which is not an absolute path", name, found)
	}
	final, err := finalPath(found)
	if err != nil {
		return "", err
	}
	checkout, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspecting %s: %w", root, err)
	}
	for _, spelling := range []string{found, final} {
		inside, err := underDirectory(spelling, checkout)
		if err != nil {
			return "", fmt.Errorf("inspecting the directories above %s: %w", spelling, err)
		}
		if inside {
			return "", fmt.Errorf("%s resolved to %q, inside the checkout at %q", name, found, spelling)
		}
	}
	return found, nil
}

// underDirectory reports whether any directory above path is dir, compared by
// file identity.
func underDirectory(path string, dir os.FileInfo) (bool, error) {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Stat(parent)
		if err != nil {
			return false, err
		}
		if os.SameFile(info, dir) {
			return true, nil
		}
		if filepath.Dir(parent) == parent {
			return false, nil
		}
	}
}
