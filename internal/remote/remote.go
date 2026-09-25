// Package remote centralizes GitHub owner/repo metadata for the project.
//
// Owner and repo are resolved lazily on first access. Values set at build
// time via ldflags take precedence; otherwise the package derives them
// from the local git remote origin. Use this package to build URLs that
// must point back at the canonical project location (issue links, raw
// content, update checks).
package remote

import (
	"context"
	"log/slog"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Set at build time via:
//
//	-X <module>/internal/remote.ldOwner=...
//	-X <module>/internal/remote.ldRepo=...
var (
	ldOwner string
	ldRepo  string
)

var (
	initOnce sync.Once
	owner    string
	repo     string
)

// githubRemoteRe extracts owner and repo from GitHub remote URLs.
// Matches both HTTPS (github.com/) and SSH (github.com:) formats.
// Uses a non-greedy second group with an optional .git suffix so that
// dotted repo names (e.g. "vue.js") are captured correctly.
//
// Both groups are confined to the characters GitHub allows in an account or
// repository name. A looser class admits "owner@host", which lands in the
// authority when the value reaches a URL and points it at another server.
var githubRemoteRe = regexp.MustCompile(
	`github\.com[:/]([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9._-]+?)(?:\.git)?\s*$`)

// identRe accepts an owner or repository name. It also guards the ldflags
// path, which no regex has filtered.
var identRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ensureInit lazily resolves owner and repo on first call. Build-time ldflags
// are preferred; otherwise the values are derived from the local git remote origin.
func ensureInit() {
	initOnce.Do(func() {
		if ldOwner != "" && ldRepo != "" {
			if !identRe.MatchString(ldOwner) || !identRe.MatchString(ldRepo) {
				slog.Debug("remote: ldflags owner or repo is not a valid GitHub name",
					"owner", ldOwner, "repo", ldRepo)
				return
			}
			owner = ldOwner
			repo = ldRepo
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
		if err != nil {
			slog.Debug("remote: ldflags not set and git remote unavailable", "error", err)
			return
		}
		m := githubRemoteRe.FindStringSubmatch(string(out))
		if len(m) == 3 {
			owner = m[1]
			repo = m[2]
		}
	})
}

// Owner returns the GitHub repository owner.
func Owner() string {
	ensureInit()
	return owner
}

// Repo returns the GitHub repository name.
func Repo() string {
	ensureInit()
	return repo
}

// RawURL returns the raw GitHub URL for a file on the main branch.
// Returns empty string if owner/repo could not be determined.
//
// The result is assembled through [url.URL] and every path segment is
// escaped, so a separator or a control character in path stays inside the
// path and cannot reach the host or the query.
func RawURL(path string) string {
	ensureInit()
	if owner == "" || repo == "" {
		return ""
	}
	segments := []string{owner, repo, "main"}
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	base := url.URL{Scheme: "https", Host: "raw.githubusercontent.com"}
	return base.JoinPath(segments...).String()
}
