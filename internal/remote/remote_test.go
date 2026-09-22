package remote

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	dir := t.TempDir()
	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(emptyConfig, nil, 0o600))
	run := func(args ...string) {
		t.Helper()
		// Args are test-constant string literals; flagging as tainted is a
		// false positive here.
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec
		// Isolate from the user's git config so no hooks/signing/identity
		// settings leak into the test invocation.
		cmd.Env = append(cmd.Environ(),
			"GIT_CONFIG_GLOBAL="+emptyConfig,
			"GIT_CONFIG_SYSTEM="+emptyConfig,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("remote", "add", "origin", "https://github.com/testowner/testrepo.git")

	t.Chdir(dir)
	resetInit(t)

	ensureInit()

	assert.Equal(t, "testowner", owner, "owner")
	assert.Equal(t, "testrepo", repo, "repo")
}

// TestEnsureInit_GitFails covers the error branch where no ldflags are set
// and `git remote get-url origin` fails (running outside a git checkout).
// Running in a plain temp dir guarantees the failure regardless of whether
// the host repo has an origin configured.
func TestEnsureInit_GitFails(t *testing.T) {
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
