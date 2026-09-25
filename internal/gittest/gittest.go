// Package gittest keeps a test's git processes inside the repository the test
// creates.
//
// A git hook exports its repository to every process it starts: an absolute
// GIT_DIR in a linked worktree, and GIT_INDEX_FILE in every pre-commit hook.
// A test that runs git, or runs code that runs git, under such a hook acts on
// the repository the hook runs for: it commits onto its branch, tags it, or
// adds a remote. [Isolate] takes that state away first.
package gittest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ///////////////////////////////////////////////
// Isolation
// ///////////////////////////////////////////////

// Isolate removes every inherited GIT_ variable from the process environment
// for the rest of the test. It then points git's global and system
// configuration at an empty file, and stops repository discovery at the
// test's temporary root.
//
// Every git process the test starts afterward sees only the state the test
// creates, and so does code under test that starts git itself. A caller still
// names its repository with git -C.
//
// Isolate changes the process environment, so the test must not run in
// parallel. t.Setenv panics if it does.
func Isolate(t testing.TB) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		// t.Setenv records the value it restores at cleanup, and the unset
		// holds until then.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("gittest: unsetting %s: %v", name, err)
		}
	}

	scratch := t.TempDir()
	emptyConfig := filepath.Join(scratch, "gitconfig")
	if err := os.WriteFile(emptyConfig, nil, 0o600); err != nil {
		t.Fatalf("gittest: writing an empty git configuration: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", emptyConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", emptyConfig)
	// Every t.TempDir of one test sits under one root. A repository the test
	// creates lies below it, so git finds that repository and nothing above.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(scratch))
}
