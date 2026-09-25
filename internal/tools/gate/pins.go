package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
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
	// versionPattern is the shape of the tool's release versions. The pin
	// and the lockfile's version must match it before a url is built from
	// them.
	versionPattern *regexp.Regexp
	// assets names the release asset for each lockfile platform, with
	// versionPlaceholder standing for the pinned version.
	assets map[string]string
	// attested tools publish GitHub attestations, so the lockfile must record
	// them, or a downgrade to unattested bytes passes.
	attested bool
}

// pinsFile is mise.toml as the check reads it. A [tools] entry is a version
// string or a table, so it stays untyped until toolVersion reads it.
type pinsFile struct {
	Tools    map[string]any `toml:"tools"`
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
	pinsPath           = "mise.toml"
	lockPath           = "mise.lock"
	attested           = "github-attestations"
	relockAdvice       = "Write it again with: mise lock"
	versionPlaceholder = "{version}"
	// lockfileVersion is the format mise lock writes, and the one every rule
	// here is written against.
	lockfileVersion int64 = 1
)

// The one url_replacements rule. mise fetches url, and falls back to url_api
// when that fetch fails. The lockfile carries url_api as an opaque asset id
// bound to no version, so a url_api naming another release of the same
// repository installs that release. The rule sends every url_api fetch to a
// host that does not exist, so the fallback fails instead. url stays bound
// byte for byte below.
const (
	urlReplacementKey    = `regex:^https://api\.github\.com/repos/[^/]+/[^/]+/releases/assets/.*$`
	urlReplacementTarget = "https://url-api-refused.invalid/"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// tools is every tool the gate runs through mise. mise refuses a version the
// lockfile does not name and a platform it does not cover, and accepts an
// entry with no checksum, a rewritten backend, another url or url_api, or a
// deleted provenance line. Those sit in a generated file a bump rewrites
// wholesale, so the expectations live here and the lockfile is held to them.
// A bump that renames an upstream asset edits the asset here in the same
// change. MISE_BACKENDS_<TOOL> overrides a backend from the environment and no
// setting reports it, so miseEnvironment keeps every inherited MISE_ name out.
var tools = []tool{
	{
		key: "actionlint", owner: "rhysd", repository: "actionlint", tagPrefix: "v", versionPattern: threePartVersion, attested: true,
		assets: map[string]string{
			"linux-x64":   "actionlint_{version}_linux_amd64.tar.gz",
			"macos-arm64": "actionlint_{version}_darwin_arm64.tar.gz",
			"windows-x64": "actionlint_{version}_windows_amd64.zip",
		},
	},
	{
		key: "shellcheck", owner: "koalaman", repository: "shellcheck", tagPrefix: "v", versionPattern: threePartVersion, attested: false,
		assets: map[string]string{
			"linux-x64":   "shellcheck-v{version}.linux.x86_64.tar.xz",
			"macos-arm64": "shellcheck-v{version}.darwin.aarch64.tar.xz",
			"windows-x64": "shellcheck-v{version}.zip",
		},
	},
	{
		key: "taplo", owner: "tamasfe", repository: "taplo", tagPrefix: "", versionPattern: threePartVersion, attested: false,
		assets: map[string]string{
			"linux-x64":   "taplo-linux-x86_64.gz",
			"macos-arm64": "taplo-darwin-aarch64.gz",
			"windows-x64": "taplo-windows-x86_64.zip",
		},
	},
	{
		key: "zizmor", owner: "zizmorcore", repository: "zizmor", tagPrefix: "v", versionPattern: threePartVersion, attested: true,
		assets: map[string]string{
			"linux-x64":   "zizmor-x86_64-unknown-linux-gnu.tar.gz",
			"macos-arm64": "zizmor-aarch64-apple-darwin.tar.gz",
			"windows-x64": "zizmor-x86_64-pc-windows-msvc.zip",
		},
	},
}

var (
	sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// threePartVersion is major.minor.patch in ASCII digits, the form all four
	// tools release under. It admits no slash, dot segment or other byte, so
	// a url built from a version that matches it names that release alone.
	threePartVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// mise.toml holds these three tables and nothing else. [hooks], [env], [vars],
// [tasks] and a tool's options can each run a command during `mise install`,
// whatever the trust state, so a table that reads as data is the only kind
// allowed. The two below are compared whole, as go-toml decodes them.
var (
	pinsTables         = []string{"tools", "tool_config", "settings"}
	expectedToolConfig = map[string]any{"locked": true}
	expectedSettings   = map[string]any{
		"locked":                        true,
		"lockfile":                      true,
		"lockfile_platforms":            []any{"linux-x64", "macos-arm64", "windows-x64"},
		"url_replacements":              map[string]any{urlReplacementKey: urlReplacementTarget},
		"locked_verify_provenance":      true,
		"provenance_api_failures_fatal": true,
		"github_attestations":           true,
		"aqua":                          map[string]any{"github_attestations": true},
	}
)

// The keys mise lock writes, at each level of mise.lock. A key outside them
// is one mise may act on and nothing here reads.
var (
	lockTopKeys      = []string{"lockfile_version", "tools"}
	lockEntryKeys    = []string{"version", "backend", "specifiers", "options"}
	lockPlatformKeys = []string{"checksum", "url", "url_api", "provenance"}
)

// ///////////////////////////////////////////////
// The check
// ///////////////////////////////////////////////

// pinsFindings reads mise.toml and mise.lock under dir against tools, and the
// tree around them against treeFindings. It returns every disagreement.
func pinsFindings(dir string, tracked trackedLister) ([]string, error) {
	var pins pinsFile
	var pinsRaw map[string]any
	if err := decodeTOML(filepath.Join(dir, pinsPath), &pins, &pinsRaw); err != nil {
		return nil, err
	}
	var lock lockFile
	var lockRaw map[string]any
	if err := decodeTOML(filepath.Join(dir, lockPath), &lock, &lockRaw); err != nil {
		return nil, err
	}
	found, err := treeFindings(dir, tracked)
	if err != nil {
		return nil, err
	}
	found = append(found, tableFindings(pinsRaw)...)
	found = append(found, keyFindings(lockPath, "", lockRaw, lockTopKeys)...)
	if got, _ := lockRaw["lockfile_version"].(int64); got != lockfileVersion {
		found = append(found, fmt.Sprintf("%s lockfile_version is %s, and mise lock writes %d. %s", lockPath, show(lockRaw, "lockfile_version"), lockfileVersion, relockAdvice))
	}
	found = append(found, toolKeyFindings(pins, lock)...)
	return append(found, lockFindings(pins, lock)...), nil
}

// toolKeyFindings refuses a tool mise.toml pins or mise.lock records that
// tools does not name. lockFindings asserts only the tools it names, so an
// entry outside them would go unread.
func toolKeyFindings(pins pinsFile, lock lockFile) []string {
	expected := make([]string, 0, len(tools))
	for _, t := range tools {
		expected = append(expected, t.key)
	}
	var found []string
	for _, key := range slices.Sorted(maps.Keys(pins.Tools)) {
		if !slices.Contains(expected, key) {
			found = append(found, fmt.Sprintf("%s pins %q, and pins.go holds no expectation for it", pinsPath, key))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(lock.Tools)) {
		if !slices.Contains(expected, key) {
			found = append(found, fmt.Sprintf("%s records %q, and pins.go holds no expectation for it. %s", lockPath, key, relockAdvice))
		}
	}
	return found
}

// tableFindings holds mise.toml to its three tables, and [tool_config] and
// [settings] to their expected values, whole.
func tableFindings(raw map[string]any) []string {
	found := keyFindings(pinsPath, "", raw, pinsTables)
	for _, table := range []struct {
		name string
		want map[string]any
	}{{name: "tool_config", want: expectedToolConfig}, {name: "settings", want: expectedSettings}} {
		got, _ := raw[table.name].(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(mergeKeys(got, table.want))) {
			if !reflect.DeepEqual(got[key], table.want[key]) {
				found = append(found, fmt.Sprintf("%s [%s] %s is %s, and it must be %s", pinsPath, table.name, key, show(got, key), show(table.want, key)))
			}
		}
	}
	return found
}

// keyFindings refuses every key of table outside allowed. where names the
// table in the finding.
func keyFindings(file, where string, table map[string]any, allowed []string) []string {
	var found []string
	for _, key := range slices.Sorted(maps.Keys(table)) {
		if !slices.Contains(allowed, key) {
			found = append(found, fmt.Sprintf("%s%s carries %q, which mise can act on and pins.go does not read. %s", file, where, key, relockAdvice))
		}
	}
	return found
}

// mergeKeys returns a set holding the keys of both maps.
func mergeKeys(a, b map[string]any) map[string]bool {
	keys := map[string]bool{}
	for key := range a {
		keys[key] = true
	}
	for key := range b {
		keys[key] = true
	}
	return keys
}

// show renders table[key] for a finding, quoted so a control character in the
// file cannot reach the terminal, or "absent".
func show(table map[string]any, key string) string {
	value, ok := table[key]
	if !ok {
		return "absent"
	}
	return strconv.Quote(fmt.Sprint(value))
}

// toolVersion reads a [tools] entry: a version string, or a table holding
// version and, at most, a version_prefix equal to the tool's tag prefix. Any
// other key is an option mise acts on during an install.
func toolVersion(t tool, entry any) (string, []string) {
	if version, ok := entry.(string); ok {
		return version, nil
	}
	table, ok := entry.(map[string]any)
	if !ok {
		return "", []string{fmt.Sprintf("%s [tools] %s is %s, and it must be a version or a table holding one", pinsPath, t.key, strconv.Quote(fmt.Sprint(entry)))}
	}
	return optionsVersion(pinsPath+" [tools] "+t.key, t, table)
}

// optionsVersion holds a table to version plus an optional version_prefix
// equal to the tool's tag prefix, and returns the version. where names the
// table in a finding.
func optionsVersion(where string, t tool, table map[string]any) (string, []string) {
	var found []string
	for _, key := range slices.Sorted(maps.Keys(table)) {
		switch key {
		case "version":
		case "version_prefix":
			if prefix, _ := table[key].(string); prefix != t.tagPrefix {
				found = append(found, fmt.Sprintf("%s version_prefix is %s, and %s tags carry %q", where, show(table, key), t.key, t.tagPrefix))
			}
		default:
			found = append(found, fmt.Sprintf("%s carries %q, and an entry holds only version and version_prefix", where, key))
		}
	}
	version, _ := table["version"].(string)
	return version, found
}

// lockFindings holds the lockfile to the pins and to tools.
func lockFindings(pins pinsFile, lock lockFile) []string {
	var found []string
	platforms := pins.Settings.LockfilePlatforms
	// Every assertion below runs once per named platform, so a pins file that
	// names none would assert nothing and pass.
	if len(platforms) == 0 {
		found = append(found, fmt.Sprintf("%s names no lockfile_platforms, so no platform entry can be asserted", pinsPath))
	}
	for _, t := range tools {
		found = append(found, assetFindings(t, platforms)...)
		pinned, ok := pins.Tools[t.key]
		if !ok {
			found = append(found, fmt.Sprintf("pins.go expects %s, and %s does not pin it", t.key, pinsPath))
			continue
		}
		version, refused := toolVersion(t, pinned)
		found = append(found, refused...)
		entries := lock.Tools[t.key]
		if len(entries) != 1 {
			found = append(found, fmt.Sprintf("%s records %d entries for %s, and one is expected. %s", lockPath, len(entries), t.key, relockAdvice))
			continue
		}
		entry := entries[0]
		found = append(found, entryFindings(t, entry)...)
		if refused := versionFindings(t, version, entry); len(refused) > 0 {
			found = append(found, refused...)
			continue
		}
		coordinate := "aqua:" + t.owner + "/" + t.repository
		if got, _ := entry["backend"].(string); got != coordinate {
			found = append(found, fmt.Sprintf("%s records backend %q for %s, and the tool comes from %s", lockPath, got, t.key, coordinate))
		}
		// mise also reads a nested [tools.<tool>.platforms.<name>] table, for
		// any platform. The quoted form below is the one mise lock writes, so
		// the nested form is refused outright.
		if _, nested := entry["platforms"]; nested {
			found = append(found, fmt.Sprintf("%s carries a nested platforms table for %s, and only the quoted \"platforms.<name>\" form mise lock writes is read. %s",
				lockPath, t.key, relockAdvice))
		}
		for _, platform := range platforms {
			table, ok := entry["platforms."+platform].(map[string]any)
			if !ok {
				found = append(found, fmt.Sprintf("%s records no %q entry for %s. %s", lockPath, platform, t.key, relockAdvice))
				continue
			}
			if asset, ok := t.assets[platform]; ok {
				found = append(found, platformFindings(t, version, platform, asset, table)...)
			}
		}
		// A platform table lockfile_platforms does not name is installed from
		// by a contributor on that platform and read by nothing above, so it
		// is refused rather than left unasserted.
		for key := range entry {
			platform, isPlatform := strings.CutPrefix(key, "platforms.")
			if isPlatform && !slices.Contains(platforms, platform) {
				found = append(found, fmt.Sprintf("%s records a %q entry for %s, and %s names no such platform in lockfile_platforms. %s", lockPath, platform, t.key, pinsPath, relockAdvice))
			}
		}
	}
	return found
}

// versionFindings holds the pin and the lockfile's version to the tool's
// release shape, then to each other. Every url is built from the version, so
// a version carrying a slash or a dot segment would build a url that walks out
// of the release and still compares equal. A refusal here stops the tool's
// checks before any url is built.
func versionFindings(t tool, pinned string, entry map[string]any) []string {
	var found []string
	if !t.versionPattern.MatchString(pinned) {
		found = append(found, fmt.Sprintf("%s pins %s at %q, and a %s release version matches %s", pinsPath, t.key, pinned, t.key, t.versionPattern))
	}
	recorded, _ := entry["version"].(string)
	if !t.versionPattern.MatchString(recorded) {
		found = append(found, fmt.Sprintf("%s records %s at %q, and a %s release version matches %s. %s", lockPath, t.key, recorded, t.key, t.versionPattern, relockAdvice))
	}
	if len(found) == 0 && recorded != pinned {
		found = append(found, fmt.Sprintf("%s pins %s %q, and %s records %q. %s", pinsPath, t.key, pinned, lockPath, recorded, relockAdvice))
	}
	return found
}

// entryFindings holds a lockfile entry to the keys mise lock writes: an entry
// key from lockEntryKeys or a quoted platform table, an options table holding
// only version and the tag prefix, and platform tables holding only
// lockPlatformKeys. The nested platforms form and a platform lockfile_platforms
// does not name carry findings of their own in lockFindings.
func entryFindings(t tool, entry map[string]any) []string {
	var found []string
	where := fmt.Sprintf(" [[tools.%s]]", t.key)
	for _, key := range slices.Sorted(maps.Keys(entry)) {
		platformKey, isPlatform := strings.CutPrefix(key, "platforms.")
		switch {
		case key == "platforms":
		case isPlatform:
			table, _ := entry[key].(map[string]any)
			found = append(found, keyFindings(lockPath, fmt.Sprintf(" [tools.%s.%q]", t.key, "platforms."+platformKey), table, lockPlatformKeys)...)
		case key == "options":
			options, ok := entry[key].(map[string]any)
			if !ok {
				found = append(found, fmt.Sprintf("%s%s options is %s, and it must be a table", lockPath, where, show(entry, key)))
				continue
			}
			_, refused := optionsVersion(lockPath+where+" options", t, options)
			found = append(found, refused...)
		case !slices.Contains(lockEntryKeys, key):
			found = append(found, fmt.Sprintf("%s%s carries %q, which mise can act on and pins.go does not read. %s", lockPath, where, key, relockAdvice))
		}
	}
	return found
}

// assetFindings holds a tool's asset constants to the lockfile platforms. A
// platform with no asset would go unbound, and an asset for a platform the
// pins do not name is a stale constant.
func assetFindings(t tool, platforms []string) []string {
	var found []string
	for _, platform := range platforms {
		if _, ok := t.assets[platform]; !ok {
			found = append(found, fmt.Sprintf("pins.go holds no asset for %s on %q, which lockfile_platforms names", t.key, platform))
		}
	}
	for _, platform := range slices.Sorted(maps.Keys(t.assets)) {
		if !slices.Contains(platforms, platform) {
			found = append(found, fmt.Sprintf("pins.go names an asset for %s on %s, and lockfile_platforms does not name %s", t.key, platform, platform))
		}
	}
	return found
}

// platformFindings holds one platform table to the tool's release. No parser
// reads url or url_api: url must equal the address built from the constants,
// byte for byte, and url_api must be the repository's asset path followed by
// an asset id. A parser that reads an address differently from mise's is the
// gap a comparison of text does not have.
func platformFindings(t tool, version, platform, asset string, table map[string]any) []string {
	label := t.key + " " + platform
	var found []string
	checksum, _ := table["checksum"].(string)
	if !sha256Pattern.MatchString(checksum) {
		found = append(found, fmt.Sprintf("%s records checksum %q in %s, and a sha256 digest is 64 hex digits", label, checksum, lockPath))
	}
	wantURL := "https://github.com/" + t.owner + "/" + t.repository + "/releases/download/" +
		t.tagPrefix + version + "/" + strings.ReplaceAll(asset, versionPlaceholder, version)
	if got, _ := table["url"].(string); got != wantURL {
		found = append(found, fmt.Sprintf("%s url is %q, and it must be %q", label, got, wantURL))
	}
	apiPrefix := "https://api.github.com/repos/" + t.owner + "/" + t.repository + "/releases/assets/"
	gotAPI, _ := table["url_api"].(string)
	if id, ok := strings.CutPrefix(gotAPI, apiPrefix); !ok || !allDigits(id) {
		found = append(found, fmt.Sprintf("%s url_api is %q, and it must be %s followed by an asset id", label, gotAPI, apiPrefix))
	}
	if provenance, _ := table["provenance"].(string); t.attested && provenance != attested {
		found = append(found, fmt.Sprintf("%s records provenance %q, and %s/%s attests its releases, so %s must record %s", label, provenance, t.owner, t.repository, lockPath, attested))
	}
	return found
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ///////////////////////////////////////////////
// Readers
// ///////////////////////////////////////////////

// decodeTOML reads one TOML file into each of outs, so one read feeds a
// typed view and a raw one.
func decodeTOML(path string, outs ...any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, out := range outs {
		if err := toml.Unmarshal(data, out); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	}
	return nil
}
