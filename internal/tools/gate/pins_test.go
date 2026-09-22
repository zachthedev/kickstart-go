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
zizmor = "1.30.1"

[settings]
lockfile_platforms = ["linux-x64", "windows-x64"]
`

// intactLock renders a lockfile that agrees with intactPins and tools.
func intactLock() string {
	var b strings.Builder
	for _, t := range tools {
		version := map[string]string{"actionlint": "1.7.12", "shellcheck": "0.11.0", "zizmor": "1.30.1"}[t.key]
		fmt.Fprintf(&b, "[[tools.%s]]\nversion = %q\nbackend = \"aqua:%s/%s\"\n\n", t.key, version, t.owner, t.repository)
		for _, platform := range []string{"linux-x64", "windows-x64"} {
			fmt.Fprintf(&b, "[tools.%s.\"platforms.%s\"]\n", t.key, platform)
			fmt.Fprintf(&b, "checksum = \"sha256:%s\"\n", strings.Repeat("ab", 32))
			fmt.Fprintf(&b, "url = \"https://github.com/%s/%s/releases/download/%s%s/%s-%s.tar.gz\"\n", t.owner, t.repository, t.tagPrefix, version, t.key, platform)
			fmt.Fprintf(&b, "url_api = \"https://api.github.com/repos/%s/%s/releases/assets/1\"\n", t.owner, t.repository)
			if t.attested {
				b.WriteString("provenance = \"github-attestations\"\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// writeFixture writes the two files into a fresh directory and returns it.
func writeFixture(t *testing.T, pins, lock string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(pins), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, lockPath), []byte(lock), 0o600))
	return dir
}

func TestPinsFindings_Intact(t *testing.T) {
	dir := writeFixture(t, intactPins, intactLock())
	found, err := pinsFindings(dir)
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestPinsFindings_Tampered(t *testing.T) {
	tests := []struct {
		name   string
		pins   func(string) string
		lock   func(string) string
		wantIn string
	}{
		{
			name: "checksum removed",
			lock: func(s string) string {
				return strings.Replace(s, "checksum = \"sha256:"+strings.Repeat("ab", 32)+"\"\n", "", 1)
			},
			wantIn: "checksum",
		},
		{
			name:   "checksum malformed",
			lock:   func(s string) string { return strings.Replace(s, strings.Repeat("ab", 32), "abc", 1) },
			wantIn: "64 hex digits",
		},
		{
			name: "backend rewritten",
			lock: func(s string) string {
				return strings.Replace(s, "aqua:rhysd/actionlint", "github:example/placeholder", 1)
			},
			wantIn: "backend",
		},
		{
			name: "url host swapped",
			lock: func(s string) string {
				return strings.Replace(s, "https://github.com/rhysd", "https://placeholder.invalid/rhysd", 1)
			},
			wantIn: "host is placeholder.invalid",
		},
		{
			name: "url path outside the release",
			lock: func(s string) string {
				return strings.Replace(s, "/rhysd/actionlint/releases/download/v1.7.12/", "/rhysd/actionlint/releases/download/v1.7.11/", 1)
			},
			wantIn: "must sit under /rhysd/actionlint/releases/download/v1.7.12/",
		},
		{
			name: "url carries a query",
			lock: func(s string) string {
				return strings.Replace(s, "actionlint-linux-x64.tar.gz\"", "actionlint-linux-x64.tar.gz?x=1\"", 1)
			},
			wantIn: "query",
		},
		{
			name: "url with dot segments under the release prefix",
			lock: func(s string) string {
				return strings.Replace(s, "v0.11.0/shellcheck-linux-x64.tar.gz", "v0.11.0/../../../../../attacker/attacker/releases/download/v1.0.0/x.tar.xz", 1)
			},
			wantIn: "url path is not canonical",
		},
		{
			name: "url with the traversal appended after the genuine filename",
			lock: func(s string) string {
				return strings.Replace(s, "shellcheck-linux-x64.tar.gz\"", "shellcheck-linux-x64.tar.gz/../../../../../attacker/attacker/releases/download/v1.0.0/x.tar.xz\"", 1)
			},
			wantIn: "url path is not canonical",
		},
		{
			name: "url_api with dot segments",
			lock: func(s string) string {
				return strings.Replace(s, "/repos/koalaman/shellcheck/releases/assets/1", "/repos/koalaman/shellcheck/releases/../../../attacker/attacker/releases/assets/1", 1)
			},
			wantIn: "url_api path is not canonical",
		},
		{
			name: "url_api host swapped",
			lock: func(s string) string {
				return strings.Replace(s, "https://api.github.com/repos/koalaman", "https://api.placeholder.invalid/repos/koalaman", 1)
			},
			wantIn: "url_api host is api.placeholder.invalid",
		},
		{
			name:   "provenance deleted from an attested tool",
			lock:   func(s string) string { return strings.Replace(s, "provenance = \"github-attestations\"\n", "", 1) },
			wantIn: "attests its releases",
		},
		{
			name: "platform entry removed",
			lock: func(s string) string {
				return strings.Replace(s, "[tools.zizmor.\"platforms.windows-x64\"]", "[tools.zizmor.\"platforms.freebsd-x64\"]", 1)
			},
			wantIn: "no windows-x64 entry for zizmor",
		},
		{
			name: "platform table outside lockfile_platforms",
			lock: func(s string) string {
				extra := []string{
					`[tools.shellcheck."platforms.linux-arm64"]`,
					`checksum = "sha256:` + strings.Repeat("00", 32) + `"`,
					`url = "https://placeholder.invalid/shellcheck.tar.xz"`,
					`url_api = "https://api.github.com/repos/koalaman/shellcheck/releases/assets/1"`,
				}
				return s + strings.Join(extra, "\n") + "\n"
			},
			wantIn: "names no such platform",
		},
		{
			name:   "version bumped in the pins alone",
			pins:   func(s string) string { return strings.Replace(s, "zizmor = \"1.30.1\"", "zizmor = \"1.30.2\"", 1) },
			wantIn: "records \"1.30.1\"",
		},
		{
			name: "no lockfile_platforms",
			pins: func(s string) string {
				return strings.Replace(s, "[settings]\nlockfile_platforms = [\"linux-x64\", \"windows-x64\"]\n", "", 1)
			},
			wantIn: "names no lockfile_platforms",
		},
		{
			name:   "empty lockfile_platforms",
			pins:   func(s string) string { return strings.Replace(s, "[\"linux-x64\", \"windows-x64\"]", "[]", 1) },
			wantIn: "names no lockfile_platforms",
		},
		{
			name:   "tool pinned that pins.go does not expect",
			pins:   func(s string) string { return strings.Replace(s, "[tools]\n", "[tools]\ntaplo = \"0.10.0\"\n", 1) },
			wantIn: "pins taplo",
		},
		{
			name:   "tool expected that the pins do not name",
			pins:   func(s string) string { return strings.Replace(s, "shellcheck = \"0.11.0\"\n", "", 1) },
			wantIn: "expects shellcheck",
		},
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
			found, err := pinsFindings(dir)
			require.NoError(t, err)
			require.NotEmpty(t, found, "the tamper passed")
			assert.Contains(t, strings.Join(found, "\n"), tt.wantIn)
		})
	}
}

func TestPinsFindings_Errors(t *testing.T) {
	t.Run("missing lockfile", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, pinsPath), []byte(intactPins), 0o600))
		_, err := pinsFindings(dir)
		assert.Error(t, err)
	})
	t.Run("malformed pins", func(t *testing.T) {
		dir := writeFixture(t, "[tools\n", intactLock())
		_, err := pinsFindings(dir)
		assert.ErrorContains(t, err, "parsing")
	})
}

func TestAddressFinding_Shapes(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		wantIn string
	}{
		{name: "absent", value: nil, wantIn: "not a url"},
		{name: "http", value: "http://github.com/o/r/releases/download/v1/x", wantIn: "not https"},
		{name: "credentials", value: "https://user@github.com/o/r/releases/download/v1/x", wantIn: "credentials"},
		{name: "port", value: "https://github.com:8443/o/r/releases/download/v1/x", wantIn: "credentials, a port"},
		{name: "fragment", value: "https://github.com/o/r/releases/download/v1/x#f", wantIn: "fragment"},
		{name: "percent escape", value: "https://github.com/o/r/releases/download/v1/%2e%2e/x", wantIn: "percent"},
		{name: "dot dot segment", value: "https://github.com/o/r/releases/download/v1/../../../a/b/releases/download/v2/x", wantIn: "not canonical"},
		{name: "dot segment", value: "https://github.com/o/r/releases/download/v1/./x", wantIn: "not canonical"},
		{name: "empty segment", value: "https://github.com/o/r/releases/download/v1//x", wantIn: "not canonical"},
		{name: "trailing slash", value: "https://github.com/o/r/releases/download/v1/x/", wantIn: "not canonical"},
		{name: "traversal appended after the file", value: "https://github.com/o/r/releases/download/v1/x/../../../../a/b/releases/download/v2/y", wantIn: "not canonical"},
		{name: "dot dot inside a filename", value: "https://github.com/o/r/releases/download/v1/x..tar.gz", wantIn: ""},
		{name: "intact", value: "https://github.com/o/r/releases/download/v1/x", wantIn: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressFinding("label", tt.value, "github.com", "/o/r/releases/download/v1/")
			if tt.wantIn == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tt.wantIn)
		})
	}
}
