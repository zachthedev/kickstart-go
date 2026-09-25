package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// trackedLister returns every path git tracks in the work tree at dir, as git
// records it. It is a parameter so a test substitutes it and starts no git.
type trackedLister func(dir string) ([]string, error)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// dubiousOwnership is what git prints when another account owns the
	// checkout.
	dubiousOwnership = "detected dubious ownership"
	// ownershipRemedy follows git's own message, whose safe.directory remedy
	// sits in the global config the gate never reads.
	ownershipRemedy = "The gate reads no global git config, so a safe.directory entry cannot reach it. The fix is the directory's owner: " +
		"clone the checkout as the account that runs the gate, or have that account take ownership of it"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// gitSwitches are the variables git starts with, besides SystemRoot on
// Windows. git then reads no system or global configuration, and no variable
// a .env sets reaches it: not GIT_INDEX_FILE, which points ls-files at an
// empty index, not GIT_DIR, which names another repository, and not HOME or
// XDG_CONFIG_HOME, which choose a global configuration. Git for Windows reads
// /dev/null as an empty file too.
var gitSwitches = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}

// strayConfigs names, by directory, every entry mise reads as
// a configuration or lock file besides mise.toml and mise.lock: the local and
// environment layers, the .mise and mise directories, .miserc.toml, which
// selects an environment, and .tool-versions. rootFindings refuses .config,
// which mise reads too, whole.
var strayConfigs = []struct {
	dir      string
	patterns []string
}{
	{dir: ".", patterns: []string{"mise.*.toml", "mise.*.lock", ".mise", ".mise.*", ".miserc.toml", ".tool-versions", "mise"}},
}

// linkDirs are the directories whose entries mise can reach through a link:
// the root, where it looks for every config file, and the two directories it
// reads configs from there.
var linkDirs = []string{".", ".mise", "mise"}

// rootRefused are the entries the root may not hold in any form, committed
// or not, each with what reads it.
var rootRefused = []struct {
	name  string
	reads string
}{
	{name: ".config", reads: "mise, the dotnet tool manifest, cosmiconfig's meta config and lefthook all read configs from it"},
	{name: vendor, reads: "go builds from it in place of the module cache unless a -mod flag says otherwise, and CI's gate sets -mod=readonly, so a local gate would check other code"},
	{name: "'", reads: "actionlint 1.7.12 looks the whole -shellcheck value up as one program path before it splits it into words, and the gate's value opens with a quote, so on Linux and macOS a program under it would run in place of ShellCheck's stand-in"},
}

// ///////////////////////////////////////////////
// The checks
// ///////////////////////////////////////////////

// treeFindings refuses what the checkout can carry past mise.toml and
// mise.lock: another mise config or lock file, a link where mise looks for
// one, an entry the root may not hold (rootFindings), a tracked file a
// program the gate starts reads before any check (startupFindings), a config
// the gate names no program to read, tracked or on disk (searchFindings), a
// go.mod directive that reaches the gate's own build or narrows ./...
// (goModFindings), and a tracked file a row would skip (walkedFindings).
func treeFindings(dir string, tracked trackedLister) ([]string, error) {
	found, err := configFindings(dir)
	if err != nil {
		return nil, err
	}
	links, err := linkFindings(dir)
	if err != nil {
		return nil, err
	}
	found = append(found, links...)
	refused, err := rootFindings(dir)
	if err != nil {
		return nil, err
	}
	found = append(found, refused...)
	paths, err := tracked(dir)
	if err != nil {
		return nil, fmt.Errorf("listing tracked files: %w", err)
	}
	startup, err := startupFindings(dir, paths)
	if err != nil {
		return nil, err
	}
	found = append(found, startup...)
	searched, err := searchFindings(dir, paths)
	if err != nil {
		return nil, err
	}
	found = append(found, searched...)
	module, err := goModFindings(dir)
	if err != nil {
		return nil, err
	}
	found = append(found, module...)
	walked, err := walkedFindings(dir, paths)
	if err != nil {
		return nil, err
	}
	return append(found, walked...), nil
}

// configFindings refuses every mise configuration or lock file under dir
// other than mise.toml and mise.lock. mise merges each config file it finds
// with that file's sibling lockfile, highest precedence first. A committed
// mise.local.toml beside a mise.local.lock would install from a url nothing
// here reads. Names compare through fold, because a case-insensitive
// filesystem hands mise the file under any spelling.
func configFindings(dir string) ([]string, error) {
	var found []string
	for _, stray := range strayConfigs {
		entries, err := readRealDir(filepath.Join(dir, stray.dir))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := fold(entry.Name())
			if slices.ContainsFunc(stray.patterns, func(pattern string) bool {
				matched, _ := path.Match(fold(pattern), name)
				return matched
			}) {
				found = append(found, fmt.Sprintf("%q is a mise configuration or lock file, and mise merges it over %s and %s, the only two pins.go reads. Remove it",
					path.Join(stray.dir, entry.Name()), pinsPath, lockPath))
			}
		}
	}
	return found, nil
}

// linkFindings refuses a symlink or a junction in any of linkDirs. mise
// follows a link to a config file the name scan never sees. Go reports a
// Windows junction as irregular rather than as a symlink, so both count.
func linkFindings(dir string) ([]string, error) {
	var found []string
	for _, sub := range linkDirs {
		entries, err := readRealDir(filepath.Join(dir, sub))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			info, err := os.Lstat(filepath.Join(dir, sub, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("inspecting %s: %w", entry.Name(), err)
			}
			if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				found = append(found, fmt.Sprintf("%q is a link, and mise follows it to files this check never reads. Replace it with what it points at, or remove it",
					path.Join(sub, entry.Name())))
			}
		}
	}
	return found, nil
}

// rootFindings refuses every entry at the root of dir that rootRefused names,
// through fold, whatever its kind and whether or not git tracks it.
func rootFindings(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	var found []string
	for _, entry := range entries {
		for _, refused := range rootRefused {
			if fold(entry.Name()) == fold(refused.name) {
				found = append(found, fmt.Sprintf("%q sits at the root, and %s. Remove it, committed or not", entry.Name(), refused.reads))
			}
		}
	}
	return found, nil
}

// readRealDir lists dir when it is a real directory. A missing entry, a
// file or a link lists nothing, and linkFindings reports the link one level up.
func readRealDir(dir string) ([]fs.DirEntry, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("inspecting %s: %w", dir, err)
	case !info.IsDir() || info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	return entries, nil
}

// gitLister returns the trackedLister that asks git, resolved from PATH alone
// and started under gitEnvironment inside ctx. It lists every path,
// and startupFindings matches names through fold: a pathspec compares case
// even under core.ignorecase, and its icase magic folds ASCII alone.
func gitLister(ctx context.Context) trackedLister {
	return func(dir string) ([]string, error) {
		return gitTracked(ctx, dir)
	}
}

// gitTracked lists every path git tracks in the work tree at dir. It first
// asks git which work tree it reads and refuses one that is not dir itself: an
// empty .git directory in dir makes git read the repository above it, and
// ls-files then lists that repository's index without an error.
func gitTracked(ctx context.Context, dir string) ([]string, error) {
	git, err := resolveProgram("git", dir)
	if err != nil {
		return nil, err
	}
	folders, err := systemFolders()
	if err != nil {
		return nil, err
	}
	env := gitEnvironment(folders)
	top, err := runGit(ctx, git, env, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if err := sameWorkTree(strings.TrimRight(string(top), "\r\n"), dir); err != nil {
		return nil, err
	}
	out, err := runGit(ctx, git, env, dir, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range bytes.SplitSeq(out, []byte{0}) {
		if len(name) > 0 {
			names = append(names, string(name))
		}
	}
	return names, nil
}

// runGit runs git with args in dir under env and returns what it printed. A
// nonzero exit carries git's reason, and a stop over the directory's owner
// says what fixes it.
func runGit(ctx context.Context, git string, env []string, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir}, args...)...) // #nosec G204 -- git is the absolute path resolveProgram found outside the checkout, and every caller names its arguments
	cmd.Env = env
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	cmd.WaitDelay = pipeGrace
	err := cmd.Run()
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		reason := strings.TrimSpace(stderr.String())
		if strings.Contains(reason, dubiousOwnership) {
			return nil, fmt.Errorf("git %s: %w: %s. %s", args[0], err, strconv.Quote(reason), ownershipRemedy)
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strconv.Quote(reason))
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.Bytes(), nil
}

// sameWorkTree refuses a work tree git names, top, that is not dir, compared
// by file identity, so no spelling of either path matters.
func sameWorkTree(top, dir string) error {
	topInfo, err := os.Stat(filepath.FromSlash(top))
	if err != nil {
		return fmt.Errorf("inspecting the work tree git names, %q: %w", top, err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", dir, err)
	}
	if !os.SameFile(topInfo, dirInfo) {
		return fmt.Errorf("git reads the work tree at %q, not the checkout at %q, so its list of tracked files is another repository's. An empty .git directory in the checkout does this", top, dir)
	}
	return nil
}

// gitEnvironment builds git's whole environment: gitSwitches, and the
// SystemRoot folders names, which only Windows supplies. git ls-files reads
// no other variable. os/exec adds SYSTEMROOT from the gate's own environment
// when a Windows child's list lacks it, so the system's value goes in first.
func gitEnvironment(folders map[string]string) []string {
	env := slices.Clone(gitSwitches)
	if root, ok := folders["SystemRoot"]; ok {
		env = append(env, "SystemRoot="+root)
	}
	return env
}

// fold keys a name for comparison with a name a tool reads: every default
// ignorable code point removed, then the whole name mapped to upper case and
// back to lower case under Unicode's full mappings. NTFS and APFS open a name
// in any case, and HFS+ drops ignorable code points, so two names this maps
// to one key can open one file. The mapping takes the long s, the dotless i
// and the Kelvin sign to s, i and k, and ß to ss, which merges more than a
// filesystem does: every name the gate refuses is ASCII, so the cost is a
// false refusal at worst. The Rust and Bun gates key names the same way.
func fold(name string) string {
	kept := strings.Map(func(r rune) rune {
		if defaultIgnorable(r) {
			return -1
		}
		return r
	}, name)
	return cases.Lower(language.Und).String(cases.Upper(language.Und).String(kept))
}

// defaultIgnorable reports whether r has Unicode's Default_Ignorable_Code_Point
// property, derived from the property tables the unicode package carries the
// way DerivedCoreProperties.txt derives it: Other_Default_Ignorable_Code_Point,
// format characters and variation selectors, less white space, the
// interlinear annotation and Egyptian hieroglyph format characters, and the
// prepended concatenation marks.
func defaultIgnorable(r rune) bool {
	switch {
	case unicode.Is(unicode.White_Space, r), unicode.Is(unicode.Prepended_Concatenation_Mark, r):
		return false
	case r >= 0xFFF9 && r <= 0xFFFB, r >= 0x13430 && r <= 0x13440:
		return false
	}
	return unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r)
}
