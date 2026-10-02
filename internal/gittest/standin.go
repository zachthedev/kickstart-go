package gittest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ///////////////////////////////////////////////
// Stand-in git
// ///////////////////////////////////////////////

// StandIn is a copy of the test binary named git, alone in a directory of
// its own. Run as git, it plays Git for Windows' launcher after it dies,
// while the git.exe it started still holds the output. It starts a child on
// its own output and exits, and the child holds that output until the test
// ends or StandInHold passes.
type StandIn struct {
	// Dir holds the copy. A test sets PATH to it alone.
	Dir string
}

// StandInHold caps how long the stand-in's child holds its output when
// nothing releases it.
const StandInHold = time.Minute

// The stand-in and the test signal each other through marker files in Dir.
// The stand-in writes standInStarted once its child runs. The test writes
// standInRelease to end the child. The child writes standInExited as it
// ends.
const (
	standInStarted = "started"
	standInRelease = "release"
	standInExited  = "exited"
)

// standInHoldArg is the argument the stand-in hands the child it starts.
const standInHoldArg = "hold-output"

// standInEnv names the stand-in's directory. NewStandIn sets it, so a copy
// of a test binary named git is a stand-in only when a test made it one.
const standInEnv = "GITTEST_STANDIN"

// standInNested marks every process a stand-in starts. A stand-in that finds
// it set, and is not the holding child, ends at once. Without it, a TestMain
// that never calls StandInMain runs its tests in every git those tests start.
const standInNested = "GITTEST_STANDIN_NESTED"

// releaseWait bounds how long cleanup waits for a released child to exit.
const releaseWait = 10 * time.Second

// standInProcess records whether this process runs as a stand-in. Its
// initializer runs while the package initializes, so a nested stand-in ends
// before any TestMain runs.
var standInProcess = checkStandIn(os.Args)

// StandInMain runs the stand-in when the test binary runs as git, and
// returns otherwise. A package whose tests call NewStandIn calls it first in
// TestMain, before m.Run.
func StandInMain() {
	if standInProcess {
		os.Exit(standInGit(os.Args[1:]))
	}
}

// NewStandIn copies the test binary into a fresh directory as git and
// returns it. Cleanup releases the child the stand-in starts and waits for it
// to exit, because a running copy keeps its file in use and the directory
// cannot be removed until it ends.
//
// NewStandIn sets an environment variable, so the test must not run in
// parallel. t.Setenv panics if it does.
func NewStandIn(t testing.TB) StandIn {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("gittest: locating the test binary: %v", err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("gittest: reading the test binary: %v", err)
	}
	s := StandIn{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(s.Dir, standInName()), data, 0o700); err != nil { // #nosec G306 G703 -- the stand-in must be executable, and it is the test's own binary in its own temporary directory
		t.Fatalf("gittest: writing the stand-in: %v", err)
	}
	t.Setenv(standInEnv, s.Dir)
	t.Cleanup(func() { s.release(t) })
	return s
}

// Started reports whether the stand-in started the child that holds its
// output. A test that sees false never exercised a held output.
func (s StandIn) Started() bool {
	_, err := os.Stat(filepath.Join(s.Dir, standInStarted))
	return err == nil
}

// release ends the child the stand-in started, if it started one, and waits
// for the child to exit.
func (s StandIn) release(t testing.TB) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.Dir, standInRelease), nil, 0o600); err != nil {
		t.Errorf("gittest: releasing the stand-in's child: %v", err)
		return
	}
	if !s.Started() {
		return
	}
	exited := filepath.Join(s.Dir, standInExited)
	for end := time.Now().Add(releaseWait); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(exited); err == nil {
			return
		}
		if time.Now().After(end) {
			t.Errorf("gittest: the stand-in's child still runs %s after its release", releaseWait)
			return
		}
	}
}

// standInName is the stand-in's file name, which PATH lookup finds as git.
func standInName() string {
	if runtime.GOOS == "windows" {
		return "git.exe"
	}
	return "git"
}

// isStandIn reports whether a process started as arg0, with dir as the value
// of standInEnv, is a stand-in. It takes a copy named git under a test that
// called NewStandIn. A test binary's own name, such as gittest.test, never
// matches, and neither does a fake git another package's tests build.
func isStandIn(arg0, dir string) bool {
	name := strings.ToLower(filepath.Base(arg0))
	return dir != "" && strings.TrimSuffix(name, ".exe") == "git"
}

// checkStandIn reports whether a process started with args runs as a
// stand-in. It ends a stand-in that another stand-in started, unless it is the
// holding child, and marks every stand-in so its children can tell.
func checkStandIn(args []string) bool {
	if !isStandIn(args[0], os.Getenv(standInEnv)) {
		return false
	}
	if os.Getenv(standInNested) != "" && (len(args) < 2 || args[1] != standInHoldArg) { // coverage-ignore: the refusal exits during package initialization, which writes no coverage counters
		fmt.Fprintln(os.Stderr, "gittest: a stand-in started another; TestMain must call gittest.StandInMain before m.Run")
		os.Exit(125)
	}
	if err := os.Setenv(standInNested, "1"); err != nil { // coverage-ignore: only an operating-system failure reaches it
		fmt.Fprintln(os.Stderr, err)
		os.Exit(125)
	}
	return true
}

// standInGit runs the stand-in and turns its failure into an exit code.
func standInGit(args []string) int {
	if err := runStandIn(args); err != nil { // coverage-ignore: only an operating-system failure reaches it
		fmt.Fprintln(os.Stderr, "gittest: stand-in:", err)
		return 127
	}
	return 0
}

// runStandIn is the stand-in. Called with standInHoldArg, it is the child: it
// holds the output it inherited until the test writes standInRelease or
// StandInHold passes. Otherwise it starts that child on its own output and
// writes standInStarted.
func runStandIn(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating itself: %w", err)
	}
	dir := filepath.Dir(self)
	if len(args) > 0 && args[0] == standInHoldArg {
		release := filepath.Join(dir, standInRelease)
		for end := time.Now().Add(StandInHold); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(release); err == nil {
				break
			}
		}
		return os.WriteFile(filepath.Join(dir, standInExited), nil, 0o600)
	}
	child := exec.Command(self, standInHoldArg) // #nosec G204 -- the stand-in starts a copy of its own binary
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		return fmt.Errorf("starting its child: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, standInStarted), nil, 0o600)
}
