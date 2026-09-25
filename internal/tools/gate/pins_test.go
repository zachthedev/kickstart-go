package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const intactPins = `[tools]
actionlint = "1.7.12"
shellcheck = "0.11.0"
taplo = "0.10.0"
zizmor = "1.30.1"

[tool_config]
locked = true

[settings]
locked = true
lockfile = true
lockfile_platforms = ["linux-x64", "macos-arm64", "windows-x64"]
url_replacements = { "regex:^https://api\\.github\\.com/repos/[^/]+/[^/]+/releases/assets/.*$" = "https://url-api-refused.invalid/" }
locked_verify_provenance = true
provenance_api_failures_fatal = true
github_attestations = true

[settings.aqua]
github_attestations = true
`

// intactEntries is the lockfile the pins describe, every url written out the
// way mise lock writes it, so the check is held to that text and not to its
// own derivation.
var intactEntries = []struct {
	key, version, backend, platform, url, api string
	attested                                  bool
}{
	{"actionlint", "1.7.12", "aqua:rhysd/actionlint", "linux-x64", "https://github.com/rhysd/actionlint/releases/download/v1.7.12/actionlint_1.7.12_linux_amd64.tar.gz", "https://api.github.com/repos/rhysd/actionlint/releases/assets/384924896", true},
	{"actionlint", "1.7.12", "aqua:rhysd/actionlint", "macos-arm64", "https://github.com/rhysd/actionlint/releases/download/v1.7.12/actionlint_1.7.12_darwin_arm64.tar.gz", "https://api.github.com/repos/rhysd/actionlint/releases/assets/384924893", true},
	{"actionlint", "1.7.12", "aqua:rhysd/actionlint", "windows-x64", "https://github.com/rhysd/actionlint/releases/download/v1.7.12/actionlint_1.7.12_windows_amd64.zip", "https://api.github.com/repos/rhysd/actionlint/releases/assets/384924919", true},
	{"shellcheck", "0.11.0", "aqua:koalaman/shellcheck", "linux-x64", "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz", "https://api.github.com/repos/koalaman/shellcheck/releases/assets/279056942", false},
	{"shellcheck", "0.11.0", "aqua:koalaman/shellcheck", "macos-arm64", "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.darwin.aarch64.tar.xz", "https://api.github.com/repos/koalaman/shellcheck/releases/assets/279056932", false},
	{"shellcheck", "0.11.0", "aqua:koalaman/shellcheck", "windows-x64", "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.zip", "https://api.github.com/repos/koalaman/shellcheck/releases/assets/279056944", false},
	{"taplo", "0.10.0", "aqua:tamasfe/taplo", "linux-x64", "https://github.com/tamasfe/taplo/releases/download/0.10.0/taplo-linux-x86_64.gz", "https://api.github.com/repos/tamasfe/taplo/releases/assets/257322600", false},
	{"taplo", "0.10.0", "aqua:tamasfe/taplo", "macos-arm64", "https://github.com/tamasfe/taplo/releases/download/0.10.0/taplo-darwin-aarch64.gz", "https://api.github.com/repos/tamasfe/taplo/releases/assets/257323110", false},
	{"taplo", "0.10.0", "aqua:tamasfe/taplo", "windows-x64", "https://github.com/tamasfe/taplo/releases/download/0.10.0/taplo-windows-x86_64.zip", "https://api.github.com/repos/tamasfe/taplo/releases/assets/257323062", false},
	{"zizmor", "1.30.1", "aqua:zizmorcore/zizmor", "linux-x64", "https://github.com/zizmorcore/zizmor/releases/download/v1.30.1/zizmor-x86_64-unknown-linux-gnu.tar.gz", "https://api.github.com/repos/zizmorcore/zizmor/releases/assets/552067642", true},
	{"zizmor", "1.30.1", "aqua:zizmorcore/zizmor", "macos-arm64", "https://github.com/zizmorcore/zizmor/releases/download/v1.30.1/zizmor-aarch64-apple-darwin.tar.gz", "https://api.github.com/repos/zizmorcore/zizmor/releases/assets/552067640", true},
	{"zizmor", "1.30.1", "aqua:zizmorcore/zizmor", "windows-x64", "https://github.com/zizmorcore/zizmor/releases/download/v1.30.1/zizmor-x86_64-pc-windows-msvc.zip", "https://api.github.com/repos/zizmorcore/zizmor/releases/assets/552067645", true},
}

// noneTracked is the trackedLister for a checkout that tracks nothing.
func noneTracked(string) ([]string, error) { return nil, nil }

// intactLock renders intactEntries as mise lock lays out a lockfile.
func intactLock() string {
	var b strings.Builder
	b.WriteString("lockfile_version = 1\n\n")
	current := ""
	for _, e := range intactEntries {
		if e.key != current {
			current = e.key
			fmt.Fprintf(&b, "[[tools.%s]]\nversion = %q\nbackend = %q\n\n", e.key, e.version, e.backend)
		}
		fmt.Fprintf(&b, "[tools.%s.\"platforms.%s\"]\n", e.key, e.platform)
		fmt.Fprintf(&b, "checksum = \"sha256:%s\"\n", strings.Repeat("ab", 32))
		fmt.Fprintf(&b, "url = %q\nurl_api = %q\n", e.url, e.api)
		if e.attested {
			b.WriteString("provenance = \"github-attestations\"\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeFixture writes the two files into a fresh directory, beside the
// intact startup files, and returns it.
func writeFixture(t *testing.T, pins, lock string) string {
	t.Helper()
	dir := t.TempDir()
	writeStartupFiles(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(pins), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), []byte(lock), 0o600))
	return dir
}

func TestPinsFindings_Intact(t *testing.T) {
	dir := writeFixture(t, intactPins, intactLock())
	found, err := pinsFindings(dir, noneTracked)
	require.NoError(t, err)
	assert.Empty(t, found)
}

// Each case changes one thing a pull request can change, and the check must
// refuse it and say which line is wrong.
func TestPinsFindings_Tampered(t *testing.T) {
	taploWindowsURL := "https://github.com/tamasfe/taplo/releases/download/0.10.0/taplo-windows-x86_64.zip"
	taploWindowsAPI := "https://api.github.com/repos/tamasfe/taplo/releases/assets/257323062"
	// A version that walks from taplo's release to ShellCheck's. Written into
	// the pin, the lock and every url, it leaves each url equal to the one
	// built from it, so the version rule is the only check left to refuse it.
	traversal := "0.10.0/../../../../koalaman/shellcheck/releases/download/v0.11.0"
	replaceLock := func(old, replacement string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, replacement, 1) }
	}

	tests := []struct {
		name   string
		pins   func(string) string
		lock   func(string) string
		wantIn string
	}{
		// Checksums, backends and provenance.
		{name: "checksum removed", lock: replaceLock("checksum = \"sha256:"+strings.Repeat("ab", 32)+"\"\n", ""), wantIn: "checksum"},
		{name: "checksum malformed", lock: replaceLock(strings.Repeat("ab", 32), "abc"), wantIn: "64 hex digits"},
		{name: "backend rewritten", lock: replaceLock("aqua:rhysd/actionlint", "github:example/placeholder"), wantIn: "backend"},
		{name: "provenance deleted from an attested tool", lock: replaceLock("provenance = \"github-attestations\"\n", ""), wantIn: "attests its releases"},

		// url, held byte for byte.
		{name: "url host swapped", lock: replaceLock("https://github.com/rhysd", "https://placeholder.invalid/rhysd"), wantIn: "actionlint linux-x64 url is"},
		{name: "url names another release", lock: replaceLock("/download/0.10.0/taplo-windows", "/download/0.9.3/taplo-windows"), wantIn: "taplo windows-x64 url is"},
		{name: "url with an extra segment", lock: replaceLock(taploWindowsURL, taploWindowsURL+"/extra"), wantIn: "taplo windows-x64 url is"},
		{name: "url names an asset the release does not carry", lock: replaceLock("taplo-windows-x86_64.zip", "taplo-missing.zip"), wantIn: "taplo windows-x64 url is"},
		{name: "url names another platform's asset", lock: replaceLock("taplo-windows-x86_64.zip", "taplo-linux-x86_64.gz"), wantIn: "taplo windows-x64 url is"},
		{name: "url scheme in capitals", lock: replaceLock("url = \"https://github.com/tamasfe", "url = \"HTTPS://github.com/tamasfe"), wantIn: "url is"},
		{name: "url with dot segments to another owner", lock: replaceLock(taploWindowsURL, "https://github.com/tamasfe/taplo/releases/download/0.10.0/../../../../koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.zip"), wantIn: "taplo windows-x64 url is"},
		{name: "url carrying a raw tab", lock: replaceLock(taploWindowsURL, "https://github.com/tamasfe/taplo/releases/download/0.10.0/.\t./taplo-windows-x86_64.zip"), wantIn: "taplo windows-x64 url is"},
		{name: "url carrying a query", lock: replaceLock(taploWindowsURL, taploWindowsURL+"?x=1"), wantIn: "taplo windows-x64 url is"},

		// url_api, held to the repository's asset path and an id.
		{name: "url_api host swapped", lock: replaceLock("https://api.github.com/repos/koalaman", "https://api.placeholder.invalid/repos/koalaman"), wantIn: "shellcheck linux-x64 url_api is"},
		{name: "url_api under another repository", lock: replaceLock(taploWindowsAPI, "https://api.github.com/repos/koalaman/shellcheck/releases/assets/279056944"), wantIn: "taplo windows-x64 url_api is"},
		{name: "url_api with an extra segment", lock: replaceLock(taploWindowsAPI, taploWindowsAPI+"/extra"), wantIn: "taplo windows-x64 url_api is"},
		{name: "url_api with dot segments", lock: replaceLock(taploWindowsAPI, "https://api.github.com/repos/tamasfe/taplo/releases/assets/../../../../koalaman/shellcheck/releases/assets/279056944"), wantIn: "taplo windows-x64 url_api is"},
		{name: "url_api with no id", lock: replaceLock(taploWindowsAPI, "https://api.github.com/repos/tamasfe/taplo/releases/assets/"), wantIn: "taplo windows-x64 url_api is"},
		{name: "url_api dropped", lock: replaceLock("url_api = \""+taploWindowsAPI+"\"\n", ""), wantIn: "taplo windows-x64 url_api is"},

		// Platform tables.
		{name: "platform entry removed", lock: replaceLock("[tools.zizmor.\"platforms.windows-x64\"]", "[tools.zizmor.\"platforms.freebsd-x64\"]"), wantIn: `no "windows-x64" entry for zizmor`},
		{
			name: "quoted platform table outside lockfile_platforms",
			lock: func(s string) string {
				return s + "[tools.shellcheck.\"platforms.linux-arm64\"]\nchecksum = \"sha256:" + strings.Repeat("00", 32) +
					"\"\nurl = \"https://placeholder.invalid/shellcheck.tar.xz\"\nurl_api = \"https://api.github.com/repos/koalaman/shellcheck/releases/assets/1\"\n"
			},
			wantIn: "names no such platform",
		},
		{
			name: "nested platforms table beside the quoted one",
			lock: func(s string) string {
				return s + "[tools.taplo.platforms.windows-x64]\nchecksum = \"sha256:" + strings.Repeat("00", 32) +
					"\"\nurl = \"https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.zip\"\n"
			},
			wantIn: "nested platforms table for taplo",
		},

		// The pins.
		{name: "version bumped in the pins alone", pins: replaceLock("zizmor = \"1.30.1\"", "zizmor = \"1.30.2\""), wantIn: "records \"1.30.1\""},
		{name: "version carrying a path", pins: replaceLock("taplo = \"0.10.0\"", "taplo = \"0.10.0/../../../koalaman\""), wantIn: "a taplo release version matches"},
		{
			name: "traversal version in the pin, the lock and every url",
			pins: replaceLock(`taplo = "0.10.0"`, `taplo = "`+traversal+`"`),
			lock: func(s string) string {
				s = strings.Replace(s, `version = "0.10.0"`, `version = "`+traversal+`"`, 1)
				return strings.ReplaceAll(s, "/download/0.10.0/", "/download/"+traversal+"/")
			},
			wantIn: "mise.toml pins taplo at",
		},
		{name: "lock version outside the release shape", lock: replaceLock(`version = "0.10.0"`, `version = "0.10.0-rc1"`), wantIn: "mise.lock records taplo at"},
		{name: "no lockfile_platforms", pins: replaceLock("lockfile_platforms = [\"linux-x64\", \"macos-arm64\", \"windows-x64\"]\n", ""), wantIn: "names no lockfile_platforms"},
		{name: "empty lockfile_platforms", pins: replaceLock("[\"linux-x64\", \"macos-arm64\", \"windows-x64\"]", "[]"), wantIn: "names no lockfile_platforms"},
		{name: "a platform with no asset constant", pins: replaceLock("\"windows-x64\"]", "\"windows-x64\", \"linux-arm64\"]"), wantIn: `holds no asset for actionlint on "linux-arm64"`},
		{name: "an asset constant with no platform", pins: replaceLock("\"macos-arm64\", ", ""), wantIn: "names an asset for actionlint on macos-arm64"},
		{name: "tool pinned that pins.go does not expect", pins: replaceLock("[tools]\n", "[tools]\nhadolint = \"2.14.0\"\n"), wantIn: `pins "hadolint"`},
		{name: "tool expected that the pins do not name", pins: replaceLock("shellcheck = \"0.11.0\"\n", ""), wantIn: "expects shellcheck"},

		// The url_replacements rule, compared with the rest of [settings].
		{name: "rule missing from mise.toml", pins: replaceLock("url_replacements = {", "# url_replacements = {"), wantIn: `url_replacements is absent`},
		{name: "rule lifted in mise.toml", pins: func(s string) string {
			start := strings.Index(s, "url_replacements = {")
			end := strings.Index(s[start:], "\n")
			return s[:start] + "url_replacements = {}" + s[start+end:]
		}, wantIn: "url_replacements is"},
		{name: "a redirect beside the rule", pins: replaceLock("\"https://url-api-refused.invalid/\" }", "\"https://url-api-refused.invalid/\", \"regex:^https://github\\\\.com/.*$\" = \"https://mirror.invalid/\" }"), wantIn: "url_replacements is"},

		// mise.toml holds three tables, two of them compared whole.
		{name: "a hooks table", pins: func(s string) string { return s + "\n[hooks]\npostinstall = \"echo hooked\"\n" }, wantIn: `mise.toml carries "hooks"`},
		{name: "an env table", pins: func(s string) string { return s + "\n[env]\nPROBE = \"1\"\n" }, wantIn: `mise.toml carries "env"`},
		{name: "a vars table", pins: func(s string) string { return s + "\n[vars]\nprobe = \"1\"\n" }, wantIn: `mise.toml carries "vars"`},
		{name: "a tasks table", pins: func(s string) string { return s + "\n[tasks.probe]\nrun = \"echo hooked\"\n" }, wantIn: `mise.toml carries "tasks"`},
		{name: "a setting turned off", pins: replaceLock("locked_verify_provenance = true", "locked_verify_provenance = false"), wantIn: `[settings] locked_verify_provenance is "false", and it must be "true"`},
		{name: "a setting added", pins: replaceLock("lockfile = true\n", "lockfile = true\nparanoid = false\n"), wantIn: `[settings] paranoid is "false", and it must be absent`},
		{name: "an aqua setting turned off", pins: replaceLock("[settings.aqua]\ngithub_attestations = true", "[settings.aqua]\ngithub_attestations = false"), wantIn: "[settings] aqua is"},
		{name: "tool_config unlocked", pins: replaceLock("[tool_config]\nlocked = true", "[tool_config]\nlocked = false"), wantIn: `[tool_config] locked is "false", and it must be "true"`},
		{name: "a tool entry with an install hook", pins: replaceLock(`taplo = "0.10.0"`, `taplo = { version = "0.10.0", postinstall = "echo hooked" }`), wantIn: `[tools] taplo carries "postinstall"`},
		{name: "a tool entry with another prefix", pins: replaceLock(`zizmor = "1.30.1"`, `zizmor = { version = "1.30.1", version_prefix = "release-" }`), wantIn: `[tools] zizmor version_prefix is "release-"`},
		{name: "a tool entry that is neither", pins: replaceLock(`taplo = "0.10.0"`, `taplo = 10`), wantIn: `[tools] taplo is "10"`},

		// mise.lock keys, allow-listed at every level, and the format it is
		// written in.
		{name: "a top-level lock key", lock: func(s string) string { return "hooks = \"echo hooked\"\n" + s }, wantIn: `mise.lock carries "hooks"`},
		{name: "another lockfile_version", lock: replaceLock("lockfile_version = 1\n", "lockfile_version = 99\n"), wantIn: `lockfile_version is "99", and mise lock writes 1`},
		{name: "lockfile_version as a string", lock: replaceLock("lockfile_version = 1\n", "lockfile_version = \"1\"\n"), wantIn: `lockfile_version is "1", and mise lock writes 1`},
		{name: "no lockfile_version", lock: replaceLock("lockfile_version = 1\n", ""), wantIn: "lockfile_version is absent"},
		{
			name: "a tool the lock records that pins.go does not expect",
			lock: func(s string) string {
				return s + "[[tools.cosign]]\nversion = \"2.5.0\"\nbackend = \"aqua:sigstore/cosign\"\n"
			},
			wantIn: `mise.lock records "cosign", and pins.go holds no expectation for it`,
		},
		{name: "an entry key", lock: replaceLock("backend = \"aqua:tamasfe/taplo\"\n", "backend = \"aqua:tamasfe/taplo\"\npostinstall = \"echo hooked\"\n"), wantIn: `[[tools.taplo]] carries "postinstall"`},
		{name: "an options key", lock: replaceLock("backend = \"aqua:tamasfe/taplo\"\n", "backend = \"aqua:tamasfe/taplo\"\noptions = { postinstall = \"echo hooked\" }\n"), wantIn: `[[tools.taplo]] options carries "postinstall"`},
		{name: "options with another prefix", lock: replaceLock("backend = \"aqua:rhysd/actionlint\"\n", "backend = \"aqua:rhysd/actionlint\"\noptions = { version_prefix = \"x\" }\n"), wantIn: `options version_prefix is "x"`},
		{name: "a platform key", lock: replaceLock("url = \""+taploWindowsURL+"\"\n", "url = \""+taploWindowsURL+"\"\nsize = 1\n"), wantIn: `carries "size"`},
		{name: "a control character in a value", lock: replaceLock("aqua:tamasfe/taplo", `aqua:tamasfe/taplo\u001b[2J`), wantIn: `\x1b[2J`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pins, lock := intactPins, intactLock()
			if tt.pins != nil {
				pins = tt.pins(pins)
			}
			if tt.lock != nil {
				lock = tt.lock(lock)
			}
			require.NotEqual(t, intactPins+intactLock(), pins+lock, "the tamper changed nothing")
			dir := writeFixture(t, pins, lock)
			found, err := pinsFindings(dir, noneTracked)
			require.NoError(t, err)
			require.NotEmpty(t, found, "the tamper passed")
			assert.Contains(t, strings.Join(found, "\n"), tt.wantIn)
		})
	}
}

// Each case adds one file mise would read beside mise.toml and mise.lock, and
// the check must refuse it by name, with what refuses it. Anything under
// .config is refused with .config itself. The last adds nothing mise reads.
func TestPinsFindings_StrayConfig(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		wantIn string
	}{
		{name: "local config", file: "mise.local.toml", wantIn: `"mise.local.toml"`},
		{name: "local lockfile", file: "mise.local.lock", wantIn: `"mise.local.lock"`},
		{name: "hidden config", file: ".mise.toml", wantIn: `".mise.toml"`},
		{name: "hidden local config", file: ".mise.local.toml", wantIn: `".mise.local.toml"`},
		{name: "environment config", file: "mise.ci.toml", wantIn: `"mise.ci.toml"`},
		{name: "environment lockfile", file: "mise.ci.lock", wantIn: `"mise.ci.lock"`},
		{name: "hidden environment config", file: ".mise.ci.toml", wantIn: `".mise.ci.toml"`},
		{name: "environment selector", file: ".miserc.toml", wantIn: `".miserc.toml"`},
		{name: "tool-versions file", file: ".tool-versions", wantIn: `".tool-versions"`},
		{name: "config under .config", file: ".config/mise.toml", wantIn: `".config" sits at the root`},
		{name: "conf.d under .config, in capitals", file: ".CONFIG/mise/conf.d/extra.toml", wantIn: `".CONFIG" sits at the root`},
		{name: "hidden mise directory", file: ".mise/config.toml", wantIn: `".mise"`},
		{name: "mise directory", file: "mise/config.toml", wantIn: `"mise"`},
		{name: "local config in another case", file: "Mise.Local.TOML", wantIn: `"Mise.Local.TOML"`},
		{name: "another tool's file under .config", file: ".config/other.toml", wantIn: `".config" sits at the root`},
		{name: "a .config file, not a directory", file: ".config", wantIn: `".config" sits at the root`},
		{name: "a file that only mentions mise", file: "docs/mise-notes.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFixture(t, intactPins, intactLock())
			target := filepath.Join(dir, filepath.FromSlash(tt.file))
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
			require.NoError(t, os.WriteFile(target, []byte("[tools]\ntaplo = \"0.10.0\"\n"), 0o600))
			found, err := pinsFindings(dir, noneTracked)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1, "findings: %q", found)
			assert.Contains(t, found[0], tt.wantIn)
			if !strings.Contains(tt.wantIn, "sits at the root") {
				assert.Contains(t, found[0], " is a mise configuration or lock file")
			}
		})
	}
}

func TestPinsFindings_Errors(t *testing.T) {
	t.Run("missing lockfile", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(intactPins), 0o600))
		_, err := pinsFindings(dir, noneTracked)
		assert.Error(t, err)
	})
	t.Run("malformed pins", func(t *testing.T) {
		dir := writeFixture(t, "[tools\n", intactLock())
		_, err := pinsFindings(dir, noneTracked)
		assert.ErrorContains(t, err, "parsing")
	})
}

func TestAllDigits(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"257323062", true},
		{"0", true},
		{"", false},
		{"12a", false},
		{"１２", false},
		{"12/3", false},
		{" 12", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, allDigits(tt.in))
		})
	}
}
