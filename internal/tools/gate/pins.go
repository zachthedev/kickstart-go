package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// tool is one entry mise.toml pins, with the GitHub release its artifacts
// come from. A bare registry key names no owner, so this table is the one
// place a tool's account is written outside the generated lockfile.
type tool struct {
	key        string
	owner      string
	repository string
	// tagPrefix is what the release tag carries ahead of the version.
	tagPrefix string
	// attested tools publish GitHub attestations, so the lockfile must record
	// them, or a downgrade to unattested bytes passes.
	attested bool
}

// pinsFile is the part of mise.toml the check reads.
type pinsFile struct {
	Tools    map[string]string `toml:"tools"`
	Settings struct {
		LockfilePlatforms []string `toml:"lockfile_platforms"`
	} `toml:"settings"`
}

// lockFile is mise.lock: one array of entries per tool, each entry holding
// its version, its backend and one table per platform under a dotted key.
type lockFile struct {
	Tools map[string][]map[string]any `toml:"tools"`
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	pinsPath     = "mise.toml"
	lockPath     = "mise.lock"
	releaseHost  = "github.com"
	apiHost      = "api.github.com"
	attested     = "github-attestations"
	relockAdvice = "Write it again with: mise lock"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// tools is every tool the gate runs through mise. mise refuses a version the
// lockfile does not name and a platform it does not cover, and accepts an
// entry with no checksum, a rewritten backend, a swapped url host or a
// deleted provenance line. Those four sit in a generated file a bump rewrites
// wholesale, so the expectations live here and the lockfile is held to them.
// MISE_BACKENDS_<TOOL> overrides a backend from the environment and no
// setting reports it, so that gap stays open.
var tools = []tool{
	{key: "actionlint", owner: "rhysd", repository: "actionlint", tagPrefix: "v", attested: true},
	{key: "shellcheck", owner: "koalaman", repository: "shellcheck", tagPrefix: "v", attested: false},
	{key: "zizmor", owner: "zizmorcore", repository: "zizmor", tagPrefix: "v", attested: true},
}

var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ///////////////////////////////////////////////
// The check
// ///////////////////////////////////////////////

// pinsFindings reads mise.toml and mise.lock under dir against tools and
// returns every disagreement.
func pinsFindings(dir string) ([]string, error) {
	var pins pinsFile
	if err := decodeTOML(filepath.Join(dir, pinsPath), &pins); err != nil {
		return nil, err
	}
	var lock lockFile
	if err := decodeTOML(filepath.Join(dir, lockPath), &lock); err != nil {
		return nil, err
	}
	return lockFindings(pins, lock), nil
}

// lockFindings holds the lockfile to the pins and to tools.
func lockFindings(pins pinsFile, lock lockFile) []string {
	var found []string
	// Every assertion below runs once per named platform, so a pins file that
	// names none would assert nothing and pass.
	if len(pins.Settings.LockfilePlatforms) == 0 {
		found = append(found, fmt.Sprintf("%s names no lockfile_platforms, so no platform entry can be asserted", pinsPath))
	}
	expected := make([]string, 0, len(tools))
	for _, t := range tools {
		expected = append(expected, t.key)
	}
	for key := range pins.Tools {
		if !slices.Contains(expected, key) {
			found = append(found, fmt.Sprintf("%s pins %s, and pins.go holds no expectation for it", pinsPath, key))
		}
	}
	for _, t := range tools {
		version, pinned := pins.Tools[t.key]
		if !pinned {
			found = append(found, fmt.Sprintf("pins.go expects %s, and %s does not pin it", t.key, pinsPath))
			continue
		}
		entries := lock.Tools[t.key]
		if len(entries) != 1 {
			found = append(found, fmt.Sprintf("%s records %d entries for %s, and one is expected. %s", lockPath, len(entries), t.key, relockAdvice))
			continue
		}
		entry := entries[0]
		if got, _ := entry["version"].(string); got != version {
			found = append(found, fmt.Sprintf("%s pins %s %s, and %s records %q. %s", pinsPath, t.key, version, lockPath, got, relockAdvice))
		}
		coordinate := "aqua:" + t.owner + "/" + t.repository
		if got, _ := entry["backend"].(string); got != coordinate {
			found = append(found, fmt.Sprintf("%s records backend %q for %s, and the tool comes from %s", lockPath, got, t.key, coordinate))
		}
		for _, platform := range pins.Settings.LockfilePlatforms {
			table, ok := entry["platforms."+platform].(map[string]any)
			if !ok {
				found = append(found, fmt.Sprintf("%s records no %s entry for %s. %s", lockPath, platform, t.key, relockAdvice))
				continue
			}
			found = append(found, platformFindings(t, version, platform, table)...)
		}
		// A platform table lockfile_platforms does not name is installed from
		// by a contributor on that platform and read by nothing above, so it
		// is refused rather than left unasserted.
		for key := range entry {
			platform, isPlatform := strings.CutPrefix(key, "platforms.")
			if isPlatform && !slices.Contains(pins.Settings.LockfilePlatforms, platform) {
				found = append(found, fmt.Sprintf("%s records a %s entry for %s, and %s names no such platform in lockfile_platforms. %s", lockPath, platform, t.key, pinsPath, relockAdvice))
			}
		}
	}
	return found
}

// platformFindings holds one platform table to the tool's release.
func platformFindings(t tool, version, platform string, table map[string]any) []string {
	label := t.key + " " + platform
	var found []string
	checksum, _ := table["checksum"].(string)
	if !sha256Pattern.MatchString(checksum) {
		found = append(found, fmt.Sprintf("%s records checksum %q in %s, and a sha256 digest is 64 hex digits", label, checksum, lockPath))
	}
	release := fmt.Sprintf("/%s/%s/releases/download/%s%s/", t.owner, t.repository, t.tagPrefix, version)
	if finding := addressFinding(label+" url", table["url"], releaseHost, release); finding != "" {
		found = append(found, finding)
	}
	api := fmt.Sprintf("/repos/%s/%s/releases/", t.owner, t.repository)
	if finding := addressFinding(label+" url_api", table["url_api"], apiHost, api); finding != "" {
		found = append(found, finding)
	}
	if provenance, _ := table["provenance"].(string); t.attested && provenance != attested {
		found = append(found, fmt.Sprintf("%s records provenance %q, and %s/%s attests its releases, so %s must record %s", label, provenance, t.owner, t.repository, lockPath, attested))
	}
	return found
}

// addressFinding holds one recorded address to a host and a path prefix. The
// parsed host and path are read, never the text, so credentials, a port, a
// query, a fragment or a percent escape are refused before the comparison. A
// path that is not already in its canonical form is refused too: every client
// removes dot segments before the request, so a path that reads as under the
// prefix and requests another owner's release is the shape this catches.
func addressFinding(label string, value any, host, prefix string) string {
	text, _ := value.(string)
	parsed, err := url.Parse(text)
	switch {
	case text == "" || err != nil:
		return fmt.Sprintf("%s is not a url: %q", label, text)
	case parsed.Scheme != "https":
		return fmt.Sprintf("%s is not https: %s", label, text)
	case parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "":
		return fmt.Sprintf("%s carries credentials, a port, a query or a fragment: %s", label, text)
	case strings.Contains(parsed.EscapedPath(), "%"):
		return fmt.Sprintf("%s carries a percent escape in its path: %s", label, text)
	case !canonicalPath(parsed.Path):
		return fmt.Sprintf("%s path is not canonical, so a client would request another path: %s", label, parsed.Path)
	case parsed.Hostname() != host:
		return fmt.Sprintf("%s host is %s, and it must be %s", label, parsed.Hostname(), host)
	case !strings.HasPrefix(parsed.Path, prefix):
		return fmt.Sprintf("%s path is %s, and it must sit under %s", label, parsed.Path, prefix)
	}
	return ""
}

// canonicalPath reports whether p is what a client would request as it is:
// rooted, equal to its own cleaned form, and free of empty, "." and ".."
// segments. path.Clean alone keeps a trailing slash away and folds "..", so the
// segment scan names the shapes Clean would silently rewrite.
func canonicalPath(p string) bool {
	if !strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for segment := range strings.SplitSeq(p[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// ///////////////////////////////////////////////
// Readers
// ///////////////////////////////////////////////

// decodeTOML reads one TOML file into out.
func decodeTOML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}
