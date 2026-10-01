package remote

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"zach.tools/go/kickstart/internal/gittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The stand-in git and the test signal each other through marker files beside
// the stand-in. The stand-in writes standInStarted once its child runs. The
// test writes standInRelease to end the child. The child writes standInExited
// as it ends.
const (
	standInStarted = "started"
	standInRelease = "release"
	standInExited  = "exited"
)

// standInHoldArg is the argument the stand-in git hands the child it starts.
const standInHoldArg = "hold-output"

// standInHold caps how long the stand-in's child holds its output when
// nothing releases it.
const standInHold = time.Minute

// TestMain lets the test binary stand in for git: a copy named git runs
// standInGit instead of the tests.
func TestMain(m *testing.M) {
	if strings.HasPrefix(strings.ToLower(filepath.Base(os.Args[0])), "git") {
		os.Exit(standInGit(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// standInGit plays Git for Windows' launcher after it dies, while the git.exe
// it started still holds the output. It starts a child on its own output and
// exits. Called with standInHoldArg, it is that child: it holds the output
// until the test writes standInRelease or standInHold passes.
func standInGit(args []string) int {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	dir := filepath.Dir(self)
	if len(args) > 0 && args[0] == standInHoldArg {
		release := filepath.Join(dir, standInRelease)
		for end := time.Now().Add(standInHold); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(release); err == nil {
				break
			}
		}
		if err := os.WriteFile(filepath.Join(dir, standInExited), nil, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	child := exec.Command(self, standInHoldArg) // #nosec G204 -- the stand-in starts a copy of its own binary
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	if err := os.WriteFile(filepath.Join(dir, standInStarted), nil, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	return 0
}

// ///////////////////////////////////////////////
// Test Helpers
// ///////////////////////////////////////////////

// setOwnerRepo overrides the package-level owner and repo for testing.
// It first triggers ensureInit so the sync.Once is consumed (preventing
// git commands from running during test), then sets the desired values.
// Original values are restored via t.Cleanup.
func setOwnerRepo(t *testing.T, o, r string) {
	t.Helper()

	// Ensure initOnce is consumed so ensureInit is a no-op.
	ensureInit()

	origOwner, origRepo := owner, repo
	owner = o
	repo = r

	t.Cleanup(func() {
		owner = origOwner
		repo = origRepo
	})
}

// resetInit restores package state so ensureInit's sync.Once fires afresh.
// Tests using this helper must not run in parallel with each other.
func resetInit(t *testing.T) {
	t.Helper()
	initOnce = sync.Once{}
	owner = ""
	repo = ""
	ldOwner = ""
	ldRepo = ""
	t.Cleanup(func() {
		initOnce = sync.Once{}
		owner = ""
		repo = ""
		ldOwner = ""
		ldRepo = ""
	})
}

// setLookupTimeout sets remoteLookupTimeout for the rest of the test.
func setLookupTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := remoteLookupTimeout
	remoteLookupTimeout = d
	t.Cleanup(func() { remoteLookupTimeout = orig })
}

// githubRemoteRepo creates a repository whose origin is a GitHub URL and
// returns its directory. The caller isolates git first.
func githubRemoteRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", "https://github.com/testowner/testrepo.git"},
	} {
		// Args are test-constant string literals; flagging as tainted is a
		// false positive here.
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- the test runs git in its own temporary repository
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return dir
}

// standInGitDir copies the test binary into a fresh directory as git and
// returns the directory. Cleanup releases the child the stand-in starts and
// waits for it to exit, because a running copy keeps its file in use and the
// directory cannot be removed until it ends.
func standInGitDir(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self)
	require.NoError(t, err)
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o700)) // #nosec G703 -- the test copies its own binary into its own temporary directory
	t.Cleanup(func() {
		if err := os.WriteFile(filepath.Join(dir, standInRelease), nil, 0o600); err != nil {
			t.Errorf("releasing the stand-in's child: %v", err)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, standInStarted)); errors.Is(err, fs.ErrNotExist) {
			return
		}
		assert.Eventually(t, func() bool {
			_, err := os.Stat(filepath.Join(dir, standInExited))
			return err == nil
		}, 10*time.Second, 10*time.Millisecond, "the stand-in's child still runs after its release")
	})
	return dir
}

// ///////////////////////////////////////////////
// githubRemoteRe
// ///////////////////////////////////////////////

func TestGithubRemoteRe_Matches(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "HTTPS URL",
			input:     "https://github.com/user/repo",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS URL with .git",
			input:     "https://github.com/user/repo.git",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL",
			input:     "git@github.com:user/repo.git",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL without .git",
			input:     "git@github.com:user/repo",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS with org name",
			input:     "https://github.com/my-org/my-project",
			wantOwner: "my-org",
			wantRepo:  "my-project",
		},
		{
			name:      "SSH with org name",
			input:     "git@github.com:my-org/my-project.git",
			wantOwner: "my-org",
			wantRepo:  "my-project",
		},
		{
			name:      "HTTPS dotted repo name",
			input:     "https://github.com/vuejs/vue.js",
			wantOwner: "vuejs",
			wantRepo:  "vue.js",
		},
		{
			name:      "SSH dotted repo name with .git",
			input:     "git@github.com:vuejs/vue.js.git",
			wantOwner: "vuejs",
			wantRepo:  "vue.js",
		},
		{
			name:      "HTTPS trailing newline",
			input:     "https://github.com/user/repo\n",
			wantOwner: "user",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS dotted repo trailing newline",
			input:     "https://github.com/user/vue.js\n",
			wantOwner: "user",
			wantRepo:  "vue.js",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := githubRemoteRe.FindStringSubmatch(tt.input)
			require.Len(t, m, 3, "%q did not match", tt.input)
			assert.Equal(t, tt.wantOwner, m[1], "owner")
			assert.Equal(t, tt.wantRepo, m[2], "repo")
		})
	}
}

func TestGithubRemoteRe_NoMatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"GitLab HTTPS", "https://gitlab.com/user/repo"},
		{"GitLab SSH", "git@gitlab.com:user/repo.git"},
		{"Bitbucket HTTPS", "https://bitbucket.org/user/repo"},
		{"random string", "just some text"},
		{"empty string", ""},
		{"partial URL", "github.com"},
		// A GitHub account name is alphanumeric with hyphens, so a dotted
		// owner names no real account. A repository name may carry dots.
		{"dotted owner", "https://github.com/my.company/my-project"},
		// An owner group wide enough to hold "@" puts the text after it in
		// the authority once the value reaches a URL.
		{"owner carrying a host", "https://github.com/owner@evil.example.test/repo"},
		{"owner carrying a query", "https://github.com/owner?x=1/repo"},
		{"owner as a parent reference", "https://github.com/../repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := githubRemoteRe.FindStringSubmatch(tt.input)
			assert.Nil(t, m, "%q matched the GitHub remote pattern", tt.input)
		})
	}
}

// ///////////////////////////////////////////////
// ensureInit
// ///////////////////////////////////////////////

// TestEnsureInit_LdFlagsTakePrecedence covers the ldflags-set branch: when
// both ldOwner and ldRepo carry values (as they do in a release build),
// ensureInit adopts them and skips the git subprocess entirely.
func TestEnsureInit_LdFlagsTakePrecedence(t *testing.T) {
	resetInit(t)
	ldOwner = "ld-owner"
	ldRepo = "ld-repo"

	ensureInit()

	assert.Equal(t, "ld-owner", owner, "owner")
	assert.Equal(t, "ld-repo", repo, "repo")
}

// TestEnsureInit_ParsesGithubRemote covers the git-success branch: when no
// ldflags are set and `git remote get-url origin` returns a parseable
// GitHub URL, ensureInit populates owner/repo from it. We bootstrap a
// scratch repo with a canned remote so the assertion doesn't depend on the
// host checkout's actual remote state.
func TestEnsureInit_ParsesGithubRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	// No inherited git state reaches the setup below or ensureInit's own git
	// call, so neither can add a remote to the repository a hook runs for.
	gittest.Isolate(t)
	t.Chdir(githubRemoteRepo(t))
	resetInit(t)
	// The assertions are about the parse, so a deadline no machine stall
	// reaches keeps a stalled runner from failing them.
	// TestEnsureInit_ExpiredDeadlineStopsGit holds the deadline itself.
	setLookupTimeout(t, time.Minute)

	ensureInit()

	assert.Equal(t, "testowner", owner, "owner")
	assert.Equal(t, "testrepo", repo, "repo")
}

// TestEnsureInit_ExpiredDeadlineStopsGit covers the bound on the git call.
// With the deadline already past, git never starts, so a repository whose
// remote would parse leaves owner and repo empty.
func TestEnsureInit_ExpiredDeadlineStopsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	gittest.Isolate(t)
	t.Chdir(githubRemoteRepo(t))
	resetInit(t)
	setLookupTimeout(t, 0)

	ensureInit()

	assert.Empty(t, owner, "owner resolved past an expired deadline")
	assert.Empty(t, repo, "repo resolved past an expired deadline")
}

// TestEnsureInit_WaitDelayEndsHeldOutput covers lookupWaitDelay. Git for
// Windows' launcher can exit while the git.exe it started still holds the
// output. Such a git holds ensureInit for the wait delay and no longer.
func TestEnsureInit_WaitDelayEndsHeldOutput(t *testing.T) {
	gittest.Isolate(t)
	dir := standInGitDir(t)
	t.Setenv("PATH", dir)
	resetInit(t)
	// A deadline no machine stall reaches gives the stand-in time to start its
	// child. It also leaves the wait delay as the one thing that ends the wait.
	setLookupTimeout(t, time.Minute)

	began := time.Now()
	ensureInit()
	waited := time.Since(began)

	require.FileExists(t, filepath.Join(dir, standInStarted), "the stand-in never started the child that holds its output")
	assert.Less(t, waited, standInHold/2, "ensureInit waited for the child holding git's output")
	assert.Empty(t, owner, "owner")
	assert.Empty(t, repo, "repo")
}

// TestEnsureInit_GitFails covers the error branch where no ldflags are set
// and `git remote get-url origin` fails (running outside a git checkout).
// Running in a plain temp dir guarantees the failure regardless of whether
// the host repo has an origin configured.
func TestEnsureInit_GitFails(t *testing.T) {
	gittest.Isolate(t)
	t.Chdir(t.TempDir())
	resetInit(t)

	ensureInit()

	assert.Empty(t, owner, "owner")
	assert.Empty(t, repo, "repo")
}

func TestEnsureInit_NoGitRemote(t *testing.T) {
	// Exercise behavior through setOwnerRepo + Owner()/Repo(). The helper
	// consumes initOnce so ensureInit is a no-op afterwards; setting both
	// to empty simulates "no git remote".
	setOwnerRepo(t, "", "")

	assert.Empty(t, Owner(), "Owner()")
	assert.Empty(t, Repo(), "Repo()")
}

// ///////////////////////////////////////////////
// Owner
// ///////////////////////////////////////////////

func TestOwner(t *testing.T) {
	setOwnerRepo(t, "myowner", "myrepo")
	assert.Equal(t, "myowner", Owner(), "Owner()")
}

func TestOwner_Empty(t *testing.T) {
	setOwnerRepo(t, "", "")
	assert.Empty(t, Owner(), "Owner()")
}

// ///////////////////////////////////////////////
// Repo
// ///////////////////////////////////////////////

func TestRepo(t *testing.T) {
	setOwnerRepo(t, "myowner", "myrepo")
	assert.Equal(t, "myrepo", Repo(), "Repo()")
}

func TestRepo_Empty(t *testing.T) {
	setOwnerRepo(t, "", "")
	assert.Empty(t, Repo(), "Repo()")
}

// ///////////////////////////////////////////////
// RawURL
// ///////////////////////////////////////////////

func TestRawURL_Format(t *testing.T) {
	setOwnerRepo(t, "testowner", "testrepo")

	got := RawURL("data/tiers.json")
	want := "https://raw.githubusercontent.com/testowner/testrepo/main/data/tiers.json"
	assert.Equal(t, want, got, "RawURL")
}

func TestRawURL_EmptyWhenNotConfigured(t *testing.T) {
	setOwnerRepo(t, "", "")

	result := RawURL("some/path.json")
	assert.Empty(t, result, "RawURL with empty owner/repo")
}

func TestRawURL_OwnerOnly(t *testing.T) {
	setOwnerRepo(t, "testowner", "")

	got := RawURL("file.txt")
	assert.Empty(t, got, "RawURL with repo empty")
}

func TestRawURL_RepoOnly(t *testing.T) {
	setOwnerRepo(t, "", "testrepo")

	got := RawURL("file.txt")
	assert.Empty(t, got, "RawURL with owner empty")
}

func TestRawURL_EmptyPath(t *testing.T) {
	setOwnerRepo(t, "testowner", "testrepo")

	got := RawURL("")
	want := "https://raw.githubusercontent.com/testowner/testrepo/main"
	assert.Equal(t, want, got)
}

func TestRawURL_EscapesEachSegment(t *testing.T) {
	setOwnerRepo(t, "testowner", "testrepo")

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "space",
			path: "data/some file.json",
			want: "https://raw.githubusercontent.com/testowner/testrepo/main/data/some%20file.json",
		},
		{
			name: "carriage return and newline",
			path: "a\r\nX-Injected: 1",
			want: "https://raw.githubusercontent.com/testowner/testrepo/main/a%0D%0AX-Injected:%201",
		},
		{
			name: "question mark stays in the path",
			path: "a?b=c",
			want: "https://raw.githubusercontent.com/testowner/testrepo/main/a%3Fb=c",
		},
		{
			name: "fragment marker stays in the path",
			path: "a#b",
			want: "https://raw.githubusercontent.com/testowner/testrepo/main/a%23b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RawURL(tt.path))
		})
	}
}

// The host must stay raw.githubusercontent.com whatever the path carries.
// A path that escapes into the authority is the failure this guards.
func TestRawURL_HostIsAlwaysGitHub(t *testing.T) {
	setOwnerRepo(t, "testowner", "testrepo")

	for _, path := range []string{
		"../../evil.example.test/x",
		"..\\..\\evil",
		"//evil.example.test/x",
		"a\r\nHost: evil.example.test",
	} {
		u, err := url.Parse(RawURL(path))
		require.NoError(t, err, "RawURL(%q) is not a parseable URL", path)
		assert.Equal(t, "raw.githubusercontent.com", u.Host, "RawURL(%q) host", path)
	}
}

// ldflags reach the binary from the build command and no regex has filtered
// them, so ensureInit validates them like a parsed remote.
func TestEnsureInit_RejectsMalformedLdFlags(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
	}{
		{name: "owner carrying a host", owner: "owner@evil.example.test", repo: "repo"},
		{name: "owner as a parent reference", owner: "..", repo: "repo"},
		{name: "owner with a slash", owner: "owner/extra", repo: "repo"},
		{name: "repo with a slash", owner: "owner", repo: "repo/extra"},
		{name: "repo carrying a query", owner: "owner", repo: "repo?x=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetInit(t)
			ldOwner, ldRepo = tt.owner, tt.repo
			t.Cleanup(func() { ldOwner, ldRepo = "", "" })

			assert.Empty(t, Owner(), "a rejected owner must not be adopted")
			assert.Empty(t, Repo(), "a rejected repo must not be adopted")
			assert.Empty(t, RawURL("x"), "no URL is built from rejected values")
		})
	}
}
