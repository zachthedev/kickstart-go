package version

import (
	"os/exec"
	"runtime/debug"
	"testing"

	modsemver "golang.org/x/mod/semver"

	"zach.tools/go/kickstart/internal/gittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ///////////////////////////////////////////////
// Info
// ///////////////////////////////////////////////

func TestInfo_PrefersInjectedSemver(t *testing.T) {
	semver = "9.9.9+test"
	t.Cleanup(func() { semver = "" })

	assert.Equal(t, "9.9.9+test", Info(), "Info()")
}

func TestInfo_FallsBackToBuildInfo(t *testing.T) {
	semver = ""
	t.Cleanup(func() { semver = "" })

	// The pattern rejects an empty string, so it covers both claims.
	assert.Regexp(t, `^[0-9]`, Info(), "Info() must start with a major version")
}

// ///////////////////////////////////////////////
// fromBuildInfo (via the readBuildInfo indirection)
// ///////////////////////////////////////////////

// stubBuildInfo swaps readBuildInfo for the duration of a test and restores
// it on cleanup. Also clears semver so fromBuildInfo is the path exercised.
func stubBuildInfo(t *testing.T, bi *debug.BuildInfo, ok bool) {
	t.Helper()
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return bi, ok }
	semver = ""
	t.Cleanup(func() {
		readBuildInfo = orig
		semver = ""
	})
}

func TestFromBuildInfo_ReadBuildInfoFails(t *testing.T) {
	// debug.ReadBuildInfo returns ok=false in binaries stripped of module
	// info (rare, but possible in custom builds). Fall back to "0.0.0-dev".
	stubBuildInfo(t, nil, false)
	assert.Equal(t, "0.0.0-dev", Info(), "Info()")
}

func TestFromBuildInfo_HonorsGoInstallVersion(t *testing.T) {
	// `go install pkg@v1.2.3` populates Main.Version; we surface that
	// verbatim after trimming the leading "v".
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
	}, true)
	assert.Equal(t, "1.2.3", Info(), "Info()")
}

func TestFromBuildInfo_UsesVCSSettings(t *testing.T) {
	// When Main.Version is "(devel)" (the go-test / bare go-build default),
	// fall through to VCS settings. A long SHA is truncated to 7 chars.
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc1234def56789"},
			{Key: "vcs.modified", Value: "true"},
		},
	}, true)
	want := "0.0.0-dev+gabc1234.dirty"
	assert.Equal(t, want, Info(), "Info()")
}

func TestFromBuildInfo_VCSShortSHA(t *testing.T) {
	// If vcs.revision is shorter than 7 chars (pathological but possible in
	// hand-crafted BuildInfo), keep the whole string rather than slicing.
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc12"},
		},
	}, true)
	want := "0.0.0-dev+gabc12"
	assert.Equal(t, want, Info(), "Info()")
}

func TestFromBuildInfo_NoVCSNoVersion(t *testing.T) {
	// Clean BuildInfo with no vcs.revision setting: fall all the way to
	// "0.0.0-dev" with no metadata.
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
	}, true)
	assert.Equal(t, "0.0.0-dev", Info(), "Info()")
}

// ///////////////////////////////////////////////
// DockerTag
// ///////////////////////////////////////////////

func TestDockerTag_ReplacesPlus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no build meta", "1.2.3", "1.2.3"},
		{"single plus", "1.2.3+dirty", "1.2.3-dirty"},
		{"past-tag with build meta", "1.2.3-dev.5+gabc1234", "1.2.3-dev.5-gabc1234"},
		{"past-tag with dirty meta", "1.2.3-dev.5+gabc1234.dirty", "1.2.3-dev.5-gabc1234.dirty"},
		{"dev build", "0.0.0-dev+gabc1234", "0.0.0-dev-gabc1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inject the value under test directly so we don't depend on
			// a build-time ldflags injection or on git state.
			semver = tt.input
			t.Cleanup(func() { semver = "" })

			got := DockerTag()
			assert.Equal(t, tt.want, got, "DockerTag() with semver")
		})
	}
}

// ///////////////////////////////////////////////
// FromGit
// ///////////////////////////////////////////////

// TestFromGit_InRepo bootstraps a scratch git repo with a known tag so the
// happy path (git describe succeeds, parseDescribe normalizes the output)
// exercises deterministically regardless of the host repo's VCS state.
//
// gittest.Isolate drops every inherited git variable and masks the user's
// configuration, so neither the setup nor FromGit's own git call can commit
// or tag in the repository a hook runs for, and no signing agent is reached.
func TestFromGit_InRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	gittest.Isolate(t)
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		flags := []string{
			"-C", dir,
			"-c", "commit.gpgsign=false",
			"-c", "tag.gpgsign=false",
		}
		cmd := exec.Command("git", append(flags, args...)...) // #nosec G204 -- the test runs git in its own temporary repository
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init")
	git("commit", "--allow-empty", "-m", "seed")
	git("tag", "v1.2.3")

	t.Chdir(dir)
	assert.Equal(t, "1.2.3", FromGit(), "FromGit() in seeded repo")
}

// TestFromGit_OutsideRepo changes into a scratch directory that isn't a git
// checkout, forcing runGit to fail. FromGit falls back to "0.0.0-dev".
func TestFromGit_OutsideRepo(t *testing.T) {
	gittest.Isolate(t)
	t.Chdir(t.TempDir())
	assert.Equal(t, "0.0.0-dev", FromGit(), "FromGit() outside a git repo")
}

// ///////////////////////////////////////////////
// parseDescribe
// ///////////////////////////////////////////////

func TestParseDescribe_Shapes(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want string
	}{
		// No tags: git describe --always emits the short SHA directly.
		{"no tags clean", "abc1234", "0.0.0-dev+gabc1234"},
		{"no tags dirty", "abc1234-dirty", "0.0.0-dev+gabc1234.dirty"},
		{"no tags long SHA", "abcdef1234567890", "0.0.0-dev+gabcdef1234567890"},

		// Exact tag (HEAD == tag).
		{"exact tag clean", "v1.2.3", "1.2.3"},
		{"exact tag dirty", "v1.2.3-dirty", "1.2.3+dirty"},
		{"exact prerelease tag", "v2.0.0-rc.1", "2.0.0-rc.1"},
		{"exact prerelease dirty", "v2.0.0-rc.1-dirty", "2.0.0-rc.1+dirty"},
		{"double-digit major", "v12.0.0", "12.0.0"},

		// Past tag: base + commit count + short SHA.
		{"5 past tag clean", "v1.2.3-5-gabc1234", "1.2.3-dev.5+gabc1234"},
		{"5 past tag dirty", "v1.2.3-5-gabc1234-dirty", "1.2.3-dev.5+gabc1234.dirty"},
		{"10 past tag clean", "v1.2.3-10-gdef5678", "1.2.3-dev.10+gdef5678"},
		{"past prerelease clean", "v2.0.0-rc.1-3-gabc1234", "2.0.0-rc.1.dev.3+gabc1234"},
		{"past prerelease dirty", "v2.0.0-rc.1-3-gabc1234-dirty", "2.0.0-rc.1.dev.3+gabc1234.dirty"},

		// Long SHAs.
		{"past tag long SHA", "v1.2.3-5-gabcdef1234567890", "1.2.3-dev.5+gabcdef1234567890"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDescribe(tt.desc)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Past-tag output has to sort correctly once the count crosses ten. The
// commit count gets its own prerelease identifier ("dev.5", "dev.10"), which
// SemVer compares numerically. Raw git-describe output fuses it into one
// alphanumeric identifier, which SemVer compares lexically and which orders
// 10 before 5. Both are asserted, so the reformat is measured against the
// shape it exists to correct.
func TestParseDescribe_PastTagOrdering(t *testing.T) {
	five := parseDescribe("v1.2.3-5-gabc1234")
	ten := parseDescribe("v1.2.3-10-gdef5678")

	require.Equal(t, "1.2.3-dev.5+gabc1234", five, "5-past")
	require.Equal(t, "1.2.3-dev.10+gdef5678", ten, "10-past")

	assert.Negative(t, modsemver.Compare("v"+five, "v"+ten),
		"%s must sort before %s", five, ten)
	assert.Positive(t, modsemver.Compare("v1.2.3-5-gabc1234", "v1.2.3-10-gdef5678"),
		"raw git-describe output sorts backwards, which is why parseDescribe reformats it")
}

// ///////////////////////////////////////////////
// splitPastTag
// ///////////////////////////////////////////////

func TestSplitPastTag_Cases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBase  string
		wantAhead int
		wantSHA   string
		wantOK    bool
	}{
		{"simple", "1.2.3-5-gabc1234", "1.2.3", 5, "abc1234", true},
		{"prerelease base", "2.0.0-rc.1-3-gabc1234", "2.0.0-rc.1", 3, "abc1234", true},
		{"double-digit count", "1.2.3-15-gabc1234", "1.2.3", 15, "abc1234", true},
		{"exact tag (no -g suffix)", "1.2.3", "", 0, "", false},
		{"zero count is rejected", "1.2.3-0-gabc1234", "", 0, "", false},
		{"short SHA too short", "1.2.3-5-gabc12", "", 0, "", false},
		{"non-hex SHA rejected", "1.2.3-5-gxyz1234", "", 0, "", false},
		{"no dash before -g", "base-gabc1234", "", 0, "", false},
		{"non-numeric count", "1.2.3-abc-gdef4567", "", 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, ahead, sha, ok := splitPastTag(tt.input)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			assert.Equal(t, tt.wantBase, base, "base")
			assert.Equal(t, tt.wantAhead, ahead, "ahead")
			assert.Equal(t, tt.wantSHA, sha, "sha")
		})
	}
}

// ///////////////////////////////////////////////
// runGit
// ///////////////////////////////////////////////

func TestRunGit_InvalidSubcommand(t *testing.T) {
	// A garbage subcommand makes git exit non-zero without needing any
	// particular working directory shape.
	_, err := runGit("definitely-not-a-real-subcommand")
	assert.Error(t, err, "runGit with invalid subcommand should return error")
}

func TestRunGit_Version(t *testing.T) {
	// Happy-path smoke test: --version always succeeds when git is on PATH, so
	// a missing git skips and any other failure fails.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	gittest.Isolate(t)
	out, err := runGit("--version")
	require.NoError(t, err, "runGit(--version)")
	assert.NotEmpty(t, out, "runGit(--version) returned empty output")
}

// ///////////////////////////////////////////////
// isShortSHA
// ///////////////////////////////////////////////

func TestIsShortSHA_LengthBounds(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"6 chars too short", "abc123", false},
		{"7 chars OK", "abc1234", true},
		{"40 chars OK", "abcdef1234567890abcdef1234567890abcdef12", true},
		{"41 chars too long", "abcdef1234567890abcdef1234567890abcdef123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isShortSHA(tt.input))
		})
	}
}

// ///////////////////////////////////////////////
// isHex
// ///////////////////////////////////////////////

func TestIsHex_Cases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid hex", "abc1234", true},
		{"upper not accepted", "ABC1234", false},
		{"empty rejected", "", false},
		{"has letter g", "abcg123", false},
		{"all digits", "1234567", true},
		{"all letters", "abcdeff", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isHex(tt.input))
		})
	}
}
