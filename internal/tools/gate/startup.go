package main

import (
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// vendor is where go builds dependencies from when the directory exists and
	// no -mod flag says otherwise.
	vendor = "vendor"
	// trackedShown is how many tracked paths under vendor a finding names
	// before it counts the rest.
	trackedShown = 5
	// excerptBytes is how much of a file's text a finding quotes.
	excerptBytes = 200
	// metaConfigKey is the key cosmiconfig reads its meta config from in the
	// root package.json and package.yaml.
	metaConfigKey = "cosmiconfig"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// bunEnvFiles are the files Bun 1.4.2 loads into its environment at startup,
// from the directory it starts in: the plain pair and each mode's pair. The
// first is also the file Taskfile.yml's dotenv loads into every task.
var bunEnvFiles = []string{
	".env", ".env.local",
	".env.development", ".env.development.local",
	".env.production", ".env.production.local",
	".env.test", ".env.test.local",
}

// ///////////////////////////////////////////////
// The checks
// ///////////////////////////////////////////////

// startupFindings refuses what the shared commits job leaves to the gate among
// the tracked files a program reads before any check of its own. The commits
// job refuses an env file at the root, a node_modules path, an .npmrc, a
// patchedDependencies key and a bunfig.toml key before a merge. This refuses
// the rest:
//
//   - an env file Bun loads, below the root, which the commits job reads at the
//     root alone
//   - a package.json carrying a duplicated key at any depth, and a root one
//     carrying a cosmiconfig key, which the commits job reads nowhere
//   - a vendor directory at the root, which go builds from in place of the
//     module cache unless a -mod flag says otherwise, while CI's gate sets
//     -mod=readonly
//
// Names compare through fold, because a case-insensitive filesystem opens a
// tracked .ENV as .env. A contributor's own untracked file passes.
func startupFindings(dir string, tracked []string) ([]string, error) {
	var found, vendored []string
	for _, name := range tracked {
		key, base := fold(name), fold(path.Base(name))
		switch {
		case path.Dir(name) != "." && slices.ContainsFunc(bunEnvFiles, func(env string) bool { return base == fold(env) }):
			found = append(found, fmt.Sprintf("%q is tracked, and Bun loads a file of that name into its environment from the directory it starts in. Remove it from the index with git rm --cached", name))
		case key == fold(vendor) || strings.HasPrefix(key, fold(vendor)+"/"):
			vendored = append(vendored, strconv.Quote(name))
		case base == fold("package.json"):
			refused, err := packageFindings(dir, name)
			if err != nil {
				return nil, err
			}
			found = append(found, refused...)
		}
	}
	if len(vendored) > 0 {
		found = append(found, trackedUnder(vendored, vendor, "which go builds from in place of the module cache unless a -mod flag says otherwise"))
	}
	return found, nil
}

// trackedUnder names the first few quoted paths under dir and counts the
// rest, so a committed package reads as one finding. why says what reads dir.
func trackedUnder(quoted []string, dir, why string) string {
	shown := quoted[:min(len(quoted), trackedShown)]
	more := ""
	if extra := len(quoted) - len(shown); extra > 0 {
		more = fmt.Sprintf(" and %d more", extra)
	}
	verb := "are"
	if len(quoted) == 1 {
		verb = "is"
	}
	return fmt.Sprintf("%s%s %s tracked under %s, %s. Remove them from the index with git rm -r --cached",
		strings.Join(shown, ", "), more, verb, dir, why)
}

// packageFindings refuses the tracked package.json at name when it is not
// valid JSON under RFC 7493, which refuses a duplicated key at any depth. Bun
// keeps the first copy of a key, while jq, which the shared commits job reads
// the file with, and Go keep the last, so a duplicate lets a check read a value
// Bun never uses. At the root it also refuses a cosmiconfig key: commitlint
// searches through cosmiconfig, which reads that key as its meta config
// whatever config commitlint names, and an $import there runs a module. A
// tracked file the work tree has deleted passes, because nothing can read it.
func packageFindings(dir, name string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if !jsontext.Value(data).IsValid() {
		return []string{fmt.Sprintf("%q is not valid JSON under RFC 7493, which refuses a duplicated key at any depth. Bun keeps the first copy of a key and jq and Go the last, so no check can tell what Bun reads", name)}, nil
	}
	if path.Dir(name) != "." {
		return nil, nil
	}
	// A root package.json that is valid JSON and no object carries no key for
	// cosmiconfig to read.
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) == nil {
		if _, ok := fields[metaConfigKey]; ok {
			return []string{fmt.Sprintf("%q carries a %s key, which cosmiconfig reads as commitlint's meta config whatever config commitlint names, and an $import there runs a module. Remove the key", name, metaConfigKey)}, nil
		}
	}
	return nil, nil
}

// excerpt quotes the start of a file's text for a finding, so a control
// character cannot reach the terminal and a large file stays one line.
func excerpt(data []byte) string {
	if len(data) <= excerptBytes {
		return strconv.Quote(string(data))
	}
	return strconv.Quote(string(data[:excerptBytes])) + "..."
}
