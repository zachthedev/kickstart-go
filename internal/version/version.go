// Package version provides the build's SemVer 2.0.0 version string.
//
// The string is injected at link time via ldflags and falls back to
// runtime/debug BuildInfo when ldflags are absent (bare `go build`,
// `go install pkg@version`, or execution outside a module).
//
// # Output shape
//
// Strict SemVer 2.0.0: MAJOR.MINOR.PATCH[-prerelease][+buildmeta]
//
//	Clean release tag v1.2.3:         1.2.3
//	Dirty release tag v1.2.3:         1.2.3+dirty
//	Untagged, clean:                  0.0.0-dev+gabc1234
//	Untagged, dirty:                  0.0.0-dev+gabc1234.dirty
//	5 past v1.2.3, clean:             1.2.3-dev.5+gabc1234
//	5 past v1.2.3, dirty:             1.2.3-dev.5+gabc1234.dirty
//	3 past v2.0.0-rc.1, clean:        2.0.0-rc.1.dev.3+gabc1234
//
// Build metadata (after '+') is ignored by semver comparators, so
// clean and dirty binaries from the same commit compare equal. Dirty is
// provenance, not a version ordering.
package version

import (
	"context"
	"fmt"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// gitTimeout bounds one git call. runGit is reached from scripts/build.sh, so
// a git that never answers holds the build rather than one caller: a
// repository on a disconnected network share, or a credential helper waiting
// on a prompt no build has a terminal for. The fallback version is usable, so
// giving up costs less than hanging.
const gitTimeout = 2 * time.Second

// ///////////////////////////////////////////////
// Link-time injection point
// ///////////////////////////////////////////////

// semver is set at build time via:
//
//	go build -ldflags "-X <module>/internal/version.semver=$(VERSION)"
//
// Leave it empty in bare `go build` and Info() will derive from BuildInfo.
var semver = ""

// readBuildInfo is the BuildInfo source used by fromBuildInfo. It defaults
// to debug.ReadBuildInfo; callers that need a different source (forks with
// custom VCS metadata, instrumentation hooks, or offline contexts where
// the stdlib returns ok=false) can reassign it. The indirection is why
// BuildInfo-derived version output stays deterministic across those
// environments.
var readBuildInfo = debug.ReadBuildInfo

// ///////////////////////////////////////////////
// Runtime API
// ///////////////////////////////////////////////

// Info returns the build's SemVer 2.0.0 version string.
//
// Preference:
//  1. ldflags-injected `semver` (release builds via scripts/build.sh)
//  2. BuildInfo-derived fallback (bare `go build` in a git checkout)
//  3. "0.0.0-dev" (no VCS info available, e.g., built outside a module)
func Info() string {
	if semver != "" {
		return semver
	}
	return fromBuildInfo()
}

// DockerTag returns Info() with '+' replaced by '-'. Docker image tags and
// OCI artifact references reject '+' per the distribution spec; this is
// the idiomatic mapping.
//
// Precedence remains stable because '+buildmeta' is comparator-ignored in
// SemVer, so collapsing it into the pre-release section with '-' still
// sorts identically against the clean tag.
func DockerTag() string {
	return strings.ReplaceAll(Info(), "+", "-")
}

// ///////////////////////////////////////////////
// Build-time helper (called by internal/tools/version)
// ///////////////////////////////////////////////

// FromGit shells out to `git describe --tags --match v* --always --dirty`
// and reformats the output into strict SemVer 2.0.0.
//
// Intended for use at build time by a small helper main package whose
// stdout is fed into the linker flags scripts/build.sh assembles. Returns
// "0.0.0-dev" if git is unavailable or the tree is not a repository.
func FromGit() string {
	desc, err := runGit("describe", "--tags", "--match", "v*", "--always", "--dirty")
	if err != nil || desc == "" {
		return "0.0.0-dev"
	}
	return parseDescribe(desc)
}

// ///////////////////////////////////////////////
// Internal parsing
// ///////////////////////////////////////////////

// parseDescribe converts git-describe output into strict SemVer 2.0.0.
// Handles three shapes emitted by `git describe --tags --always`:
//
//	<sha>                no tags in repo
//	v<base>              exact tag
//	v<base>-<N>-g<sha>   past tag
//
// A trailing "-dirty" suffix on any shape is peeled off and re-applied
// in the build metadata section (".dirty") per SemVer convention.
func parseDescribe(desc string) string {
	dirty := strings.HasSuffix(desc, "-dirty")
	desc = strings.TrimSuffix(desc, "-dirty")

	// No tags: git describe with --always emits the short SHA directly.
	if isShortSHA(desc) {
		return buildNoTag(desc, dirty)
	}

	// Both exact-tag and past-tag start with "v".
	desc = strings.TrimPrefix(desc, "v")

	if base, ahead, sha, ok := splitPastTag(desc); ok {
		return buildPastTag(base, ahead, sha, dirty)
	}
	return buildExactTag(desc, dirty)
}

// splitPastTag parses "<base>-<N>-g<sha>" from the right. The base may
// itself contain hyphens (e.g., pre-release tags like "2.0.0-rc.1"), so
// we peel from the end.
func splitPastTag(s string) (base string, ahead int, sha string, ok bool) {
	gIdx := strings.LastIndex(s, "-g")
	if gIdx < 0 {
		return "", 0, "", false
	}
	sha = s[gIdx+2:]
	if !isHex(sha) || len(sha) < 7 {
		return "", 0, "", false
	}
	rest := s[:gIdx]
	dashIdx := strings.LastIndex(rest, "-")
	if dashIdx < 0 {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(rest[dashIdx+1:])
	if err != nil || n <= 0 {
		return "", 0, "", false
	}
	return rest[:dashIdx], n, sha, true
}

func buildNoTag(sha string, dirty bool) string {
	meta := "g" + sha
	if dirty {
		meta += ".dirty"
	}
	return "0.0.0-dev+" + meta
}

func buildExactTag(base string, dirty bool) string {
	if !dirty {
		return base
	}
	return base + "+dirty"
}

// buildPastTag appends ".dev.<N>" to the existing prerelease chain (if
// the base has one) or opens a new prerelease with "-dev.<N>" (if the
// base is a clean release). Build metadata carries the commit SHA plus
// an optional ".dirty" tag.
func buildPastTag(base string, ahead int, sha string, dirty bool) string {
	var pre string
	if strings.Contains(base, "-") {
		pre = fmt.Sprintf("%s.dev.%d", base, ahead)
	} else {
		pre = fmt.Sprintf("%s-dev.%d", base, ahead)
	}
	meta := "g" + sha
	if dirty {
		meta += ".dirty"
	}
	return pre + "+" + meta
}

// ///////////////////////////////////////////////
// BuildInfo fallback
// ///////////////////////////////////////////////

// fromBuildInfo derives a fallback semver from runtime/debug.BuildInfo.
// Used when `semver` is unset (bare `go build` or `go install pkg@ver`).
func fromBuildInfo() string {
	bi, ok := readBuildInfo()
	if !ok {
		return "0.0.0-dev"
	}
	// `go install pkg@v1.2.3` populates Main.Version. Honor it.
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	var sha string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				sha = s.Value[:7]
			} else {
				sha = s.Value
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if sha == "" {
		return "0.0.0-dev"
	}
	return buildNoTag(sha, dirty)
}

// ///////////////////////////////////////////////
// Low-level helpers
// ///////////////////////////////////////////////

// runGit runs one git command and returns its trimmed standard output. The
// call is bounded by [gitTimeout].
func runGit(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isShortSHA reports whether s is 7-40 chars of lowercase hex. Matches
// the shape `git describe --always` emits when no tags exist.
func isShortSHA(s string) bool {
	return isHex(s) && len(s) >= 7 && len(s) <= 40
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
