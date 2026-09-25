package main

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// diskScope is where the gate looks for a configSearch's names in the work
// tree, committed or not. Tracked names are refused wherever they sit.
type diskScope int

// configSearch is how one program the gate starts finds its configuration
// when no flag names the one file to read: the file the repository keeps, and
// every other name the program looks for. The gate refuses each other name,
// tracked or on disk, so the file it keeps is the one there is to read and a
// local gate agrees with CI's.
type configSearch struct {
	// what names the kind of file, as a finding calls it.
	what string
	// named is the one file the gate names, which passes in this spelling
	// alone. Empty when the gate names a file of its own.
	named string
	// anywhere are base names the program reads in any directory.
	anywhere []string
	// atRoot are paths the program reads from the root, where the gate
	// starts it.
	atRoot []string
	// disk is where the anywhere names are refused on disk. The atRoot names
	// are refused on disk unless disk is diskNever.
	disk diskScope
	// reads says what the program does with another name, for the finding.
	reads string
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// diskWalked refuses a name wherever the walk of the work tree reaches.
	diskWalked diskScope = iota
	// diskPackages refuses a name wherever ./... reaches: the walk less every
	// directory whose name starts with a dot or an underscore, and testdata.
	diskPackages
	// diskRoot refuses a name at the root alone, where the gate starts the
	// program that reads it.
	diskRoot
	// diskNever refuses a name only when committed. It marks a personal
	// override, which .gitignore names.
	diskNever
)

const (
	// prettierrc is the one Prettier config, which the format row names.
	prettierrc = ".prettierrc"
	// prettierIgnore is the one Prettier ignore file, which the format row
	// names in place of Prettier's default pair, .gitignore included.
	prettierIgnore = ".prettierignore"
	// taploConfig is the one taplo config, which the toml row names.
	taploConfig = ".taplo.toml"
	// zizmorConfig is the one zizmor config, which the zizmor row names.
	zizmorConfig = ".github/zizmor.yml"
	// lefthookConfig is the file lefthook reads first. Its hook scripts name
	// no config, so no other name may exist beside it.
	lefthookConfig = "lefthook.yml"
	// worktrees is where the Claude Code CLI makes worktrees, each holding a
	// copy of every file here. .prettierignore names it, and ./... skips it.
	worktrees = ".claude/worktrees"
	// taskfile is the Taskfile Task reads first. The gate commands name
	// none, so no other name may exist beside it.
	taskfile = "Taskfile.yml"
	// goModule is the one module file. Every ./... row skips a directory
	// holding another.
	goModule = "go.mod"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// lefthookNames are the main configs lefthook 2.1.14 reads from the root, the
// first it finds. lefthook also reads .config, which the gate refuses whole.
var lefthookNames = withExtensions([]string{"lefthook", ".lefthook"}, ".yml", ".yaml", ".json", ".jsonc", ".toml")

// lefthookLocalNames are the local configs lefthook 2.1.14 reads from the
// root and merges over the main one: a contributor's own override, which
// .gitignore names.
var lefthookLocalNames = withExtensions([]string{"lefthook-local", ".lefthook-local"}, ".yml", ".yaml", ".json", ".jsonc", ".toml")

// taskNames are the Taskfiles Task 3.53.1 looks for, first found first, and
// the .taskrc files it reads its own settings from.
var taskNames = []string{
	"Taskfile.yml", "taskfile.yml", "Taskfile.yaml", "taskfile.yaml",
	"Taskfile.dist.yml", "taskfile.dist.yml", "Taskfile.dist.yaml", "taskfile.dist.yaml",
	".taskrc.yml", ".taskrc.yaml",
}

// configSearches is every config a program the gate or its hooks start finds
// by searching, where no flag names the one to read: actionlint's, which a
// run by hand reads, lefthook's and Task's, which take no name from the
// repository, go's module and workspace files, and the project configs Bun
// applies to a tool's imports. Prettier, commitlint, golangci-lint, taplo and
// zizmor are not here: each row names its config, and naming it stops every
// other read each tool makes, measured with a planted config of every kind.
// ShellCheck is not here either: actionlint starts it with --norc, and no
// program the gate starts inherits SHELLCHECK_OPTS.
var configSearches = []configSearch{
	{
		what: "an actionlint config", atRoot: []string{".github/actionlint.yaml", ".github/actionlint.yml"},
		reads: "actionlint reads it when no config is named, and its paths block can waive any finding",
	},
	{
		what: "a lefthook config", named: lefthookConfig, atRoot: lefthookNames,
		reads: "lefthook reads it in place of lefthook.yml, and its hook scripts name no config",
	},
	{
		what: "a lefthook local override", atRoot: lefthookLocalNames, disk: diskNever,
		reads: "lefthook merges it over lefthook.yml, where it can replace the pins job, and it belongs to one contributor, which .gitignore says",
	},
	{
		what: "a Task config", named: taskfile, atRoot: taskNames,
		reads: "Task reads another Taskfile when Taskfile.yml is absent, and a .taskrc for its own settings",
	},
	{
		what: "a Go module file below the root", named: goModule, anywhere: []string{goModule}, disk: diskPackages,
		reads: "every ./... row skips the module it starts, so nothing in the gate checks the code below it",
	},
	{
		what: "a Go workspace file", anywhere: []string{"go.work", "go.work.sum"}, disk: diskRoot,
		reads: "go builds the modules and replacements go.work names in place of the ones go.mod names",
	},
	{
		// On disk the root alone: a tool in the root node_modules resolves
		// through the config found walking up from its own files, which
		// reaches node_modules' own and the root's.
		what: "a TypeScript or JavaScript project config", anywhere: []string{"tsconfig.json", "jsconfig.json"}, disk: diskRoot,
		reads: "Bun applies its paths and baseUrl to the imports of commitlint's and Prettier's own code, and nothing here is TypeScript",
	},
}

// ///////////////////////////////////////////////
// The checks
// ///////////////////////////////////////////////

// searchFindings refuses every name a program the gate starts would read as
// its configuration, other than the one file the gate names for it: tracked
// wherever it sits, and in the work tree, committed or not, where its search's
// disk scope reaches, so a local gate agrees with CI's. Names compare through
// fold, because a case-insensitive filesystem opens a TaskFile.YML as
// Taskfile.yml. The named file passes in its own spelling alone.
func searchFindings(dir string, tracked []string) ([]string, error) {
	onDisk, err := diskPaths(dir)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, name := range tracked {
		if finding := searchFinding(name, true); finding != "" {
			found = append(found, finding)
		}
	}
	for _, name := range onDisk {
		if slices.Contains(tracked, name) {
			continue
		}
		if finding := searchFinding(name, false); finding != "" {
			found = append(found, finding)
		}
	}
	return found, nil
}

// searchFinding refuses name when a program the gate starts reads it as its
// configuration: tracked, whichever search names it, or untracked, where the
// search's disk scope reaches.
func searchFinding(name string, tracked bool) string {
	key, base := fold(name), fold(path.Base(name))
	for _, search := range configSearches {
		if name == search.named {
			continue
		}
		anywhere := slices.ContainsFunc(search.anywhere, func(want string) bool { return base == fold(want) }) &&
			(tracked || search.reachesOnDisk(name))
		atRoot := slices.ContainsFunc(search.atRoot, func(want string) bool { return key == fold(want) }) &&
			(tracked || search.disk != diskNever)
		if anywhere || atRoot {
			return fmt.Sprintf("%q is %s, and %s. Remove it", name, search.what, search.reads)
		}
	}
	return ""
}

// reachesOnDisk reports whether the search's disk scope covers name, a path
// the walk of the work tree found.
func (search configSearch) reachesOnDisk(name string) bool {
	switch search.disk {
	case diskWalked:
		return true
	case diskPackages:
		dir := path.Dir(name)
		return dir == "." || !slices.ContainsFunc(strings.Split(dir, "/"), func(segment string) bool {
			return strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "_") || segment == "testdata"
		})
	case diskRoot:
		return path.Dir(name) == "."
	default:
		return false
	}
}

// diskPaths walks the work tree at dir and returns the path of every entry
// that is not a directory, relative and slash-separated. It skips what every
// program the gate starts skips: node_modules, which an install fills with
// packages' own configs, the version control directories Prettier's walk
// skips, and the worktrees under .claude, each holding a copy of every file
// here. Each compares through fold. It follows no link.
func diskPaths(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, full)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		switch {
		case name == ".":
			return nil
		case !entry.IsDir():
			paths = append(paths, name)
			return nil
		}
		base := fold(entry.Name())
		if base == fold(nodeModules) || fold(name) == fold(worktrees) ||
			slices.ContainsFunc(skippedDirs, func(skip string) bool { return base == fold(skip) }) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking the work tree: %w", err)
	}
	return paths, nil
}

// withExtensions returns every name in names with each extension appended, in
// order, name by name.
func withExtensions(names []string, extensions ...string) []string {
	var out []string
	for _, name := range names {
		for _, extension := range extensions {
			out = append(out, name+extension)
		}
	}
	return out
}
