package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// homeEnvKeys are every variable os.UserHomeDir, os.UserConfigDir and
// os.UserCacheDir consult. Clearing all of them is how a test reaches the
// error path without touching the real environment beyond the test.
var homeEnvKeys = []string{
	"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME",
	"AppData", "LocalAppData",
}

// clearHomeEnv blanks every variable a platform root reads, so the resolver
// under test fails the way it would for a service account with no home.
func clearHomeEnv(t *testing.T) {
	t.Helper()
	for _, key := range homeEnvKeys {
		t.Setenv(key, "")
	}
}

// ///////////////////////////////////////////////
// Package-level vars
// ///////////////////////////////////////////////

func TestLogs_Valid(t *testing.T) {
	require.NotEmpty(t, Logs, "Logs is empty; expected at least one registered log file")
	for _, l := range Logs {
		assert.NotEmpty(t, l.Name, "a registered log has no Name")
		assert.NotEmpty(t, l.RelPath, "log %q has no RelPath", l.Name)
	}
}

// Each directory resolves under the platform's own root and ends in the
// binary name, and the three do not collapse onto one path.
func TestConfigDir(t *testing.T) {
	tests := []struct {
		name     string
		resolver Resolver
		root     func() (string, error)
	}{
		{name: "config", resolver: ConfigDir, root: os.UserConfigDir},
		{name: "cache", resolver: CacheDir, root: os.UserCacheDir},
		{name: "state", resolver: StateDir, root: UserStateDir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.resolver()
			require.NoError(t, err)
			root, err := tt.root()
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(root, BinaryName), got)
		})
	}
}

// Cache is the one the platforms always separate. Config and state share a
// root off Linux, which is what those platforms do.
func TestCacheDir_SeparateFromConfig(t *testing.T) {
	config, err := ConfigDir()
	require.NoError(t, err)
	cache, err := CacheDir()
	require.NoError(t, err)
	assert.NotEqual(t, cache, config)
}

func TestStateDir_ReportsWhenTheHomeIsMissing(t *testing.T) {
	clearHomeEnv(t)
	for _, tt := range []struct {
		name     string
		resolver Resolver
	}{
		{name: "config", resolver: ConfigDir},
		{name: "cache", resolver: CacheDir},
		{name: "state", resolver: StateDir},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.resolver()
			require.Error(t, err)
			assert.Equal(t, "", got)
			assert.Contains(t, err.Error(), BinaryName)
		})
	}
}

// ///////////////////////////////////////////////
// (Log).Path
// ///////////////////////////////////////////////

func TestLog_Path(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name       string
		log        Log
		wantSuffix string
	}{
		{
			name:       "default log",
			log:        LogApp,
			wantSuffix: BinaryName + ".log",
		},
		{
			name:       "nested log",
			log:        Log{Name: "daemon", RelPath: filepath.Join("logs", "daemon.log")},
			wantSuffix: "daemon.log",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.log.Path(base)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(got, base),
				"Path(%q) = %q, want it under the base", base, got)
			assert.True(t, strings.HasSuffix(got, tt.wantSuffix),
				"Path(%q) = %q, want suffix %q", base, got, tt.wantSuffix)
		})
	}
}

func TestLog_Path_NestedContainsSubdir(t *testing.T) {
	base := t.TempDir()
	l := Log{Name: "daemon", RelPath: filepath.Join("logs", "daemon.log")}
	got, err := l.Path(base)
	require.NoError(t, err)
	assert.Contains(t, got, "logs", "LogPath with nested RelPath")
}

// TestLog_Path_RefusesARelPathThatLeavesTheBase is the guard behind RelPath's
// own promise. Each case is a spelling that a bare filepath.Join resolves to
// something outside the base directory, or to the base directory itself.
func TestLog_Path_RefusesARelPathThatLeavesTheBase(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name  string
		input string
	}{
		{name: "climbs out", input: filepath.Join("..", "escaped.log")},
		{name: "climbs out from a subdirectory", input: "logs/../../escaped.log"},
		{name: "posix absolute", input: "/etc/passwd"},
		{name: "the base directory itself", input: "."},
		{name: "empty", input: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Log{Name: "daemon", RelPath: tt.input}

			got, err := l.Path(base)
			require.ErrorIs(t, err, ErrOutsideBase, "Path(%q)", tt.input)
			assert.Empty(t, got, "a refused path must come back empty")
			assert.Contains(t, err.Error(), strconv.Quote(tt.input),
				"the refusal must name what it refused, in a form a console cannot read as anything else")

			gotFor, errFor := l.PathFor(Posix, "/data/app")
			require.ErrorIs(t, errFor, ErrOutsideBase, "PathFor(%q)", tt.input)
			assert.Empty(t, gotFor, "a refused path must come back empty")
		})
	}
}

// ///////////////////////////////////////////////
// (Log).PathFor
// ///////////////////////////////////////////////

func TestLog_PathFor(t *testing.T) {
	l := Log{Name: "daemon", RelPath: "logs/daemon.log"}
	tests := []struct {
		name   string
		target TargetOS
		base   string
		want   string
	}{
		{name: "posix flat base", target: Posix, base: "/data/app", want: "/data/app/logs/daemon.log"},
		{name: "windows flat base", target: Windows, base: `C:\data\app`, want: `C:\data\app\logs\daemon.log`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := l.PathFor(tt.target, tt.base)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLog_PathFor_HostMatchesPath(t *testing.T) {
	base := t.TempDir()
	l := Log{Name: "daemon", RelPath: filepath.Join("logs", "daemon.log")}

	native, err := l.Path(base)
	require.NoError(t, err)
	host, err := l.PathFor(Host, base)
	require.NoError(t, err)
	assert.Equal(t, native, host, "Log.PathFor(Host)")
}

// ///////////////////////////////////////////////
// HomeDir
// ///////////////////////////////////////////////

func TestHomeDir(t *testing.T) {
	got, err := HomeDir()
	require.NoError(t, err)
	assert.NotEmpty(t, got, "HomeDir() returned an empty string and no error")
}

func TestHomeDir_ReportsWhenUnset(t *testing.T) {
	clearHomeEnv(t)
	got, err := HomeDir()
	require.Error(t, err, "HomeDir()")
	assert.Equal(t, "", got, "HomeDir()")
}

// ///////////////////////////////////////////////
// UserStateDir
// ///////////////////////////////////////////////

func TestUserStateDir_HonorsXDGOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_STATE_HOME is a Linux convention")
	}
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	got, err := UserStateDir()
	require.NoError(t, err)
	assert.Equal(t, "/xdg/state", got, "UserStateDir()")
}

func TestUserStateDir_DefaultsUnderHomeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_STATE_HOME is a Linux convention")
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/tester")
	got, err := UserStateDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/tester", ".local", "state"), got, "UserStateDir()")
}

func TestUserStateDir_UsesConfigRootElsewhere(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux has its own state root")
	}
	got, err := UserStateDir()
	require.NoError(t, err)
	want, err := os.UserConfigDir()
	require.NoError(t, err)
	assert.Equal(t, want, got, "UserStateDir()")
}

// ///////////////////////////////////////////////
// ConfigPath / ConfigPathFor
// ///////////////////////////////////////////////

func TestConfigPath(t *testing.T) {
	base := t.TempDir()
	got := ConfigPath(base)
	assert.True(t, strings.HasPrefix(got, base),
		"ConfigPath(%q) = %q, want it under the base", base, got)
	assert.True(t, strings.HasSuffix(got, ConfigFileName),
		"ConfigPath(%q) = %q, want suffix %q", base, got, ConfigFileName)
}

func TestConfigPathFor(t *testing.T) {
	tests := []struct {
		name   string
		target TargetOS
		base   string
		want   string
	}{
		{name: "posix", target: Posix, base: "/data/app", want: "/data/app/" + ConfigFileName},
		{name: "windows", target: Windows, base: `C:\data\app`, want: `C:\data\app\` + ConfigFileName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConfigPathFor(tt.target, tt.base)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ///////////////////////////////////////////////
// UnderUserDir
// ///////////////////////////////////////////////

func TestUnderUserDir(t *testing.T) {
	resolve := UnderUserDir(func() (string, error) { return filepath.Join("/roots", "cfg"), nil }, "myapp")
	got, err := resolve()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/roots", "cfg", "myapp"), got, "UnderUserDir()()")
}

func TestUnderUserDir_WrapsTheRootError(t *testing.T) {
	sentinel := errors.New("no home")
	resolve := UnderUserDir(func() (string, error) { return "", sentinel }, "myapp")
	got, err := resolve()
	require.Error(t, err, "UnderUserDir()()")
	assert.Empty(t, got, "a failing resolver must return no path")
	assert.ErrorIs(t, err, sentinel, "error")
	assert.Contains(t, err.Error(), "myapp", "error")
}

// ///////////////////////////////////////////////
// HomeRelative
// ///////////////////////////////////////////////

func TestHomeRelative(t *testing.T) {
	got, err := HomeRelative(".testapp")()
	require.NoError(t, err)
	home, err := HomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".testapp"), got, "HomeRelative(.testapp)()")
}

func TestHomeRelative_ReportsWhenTheHomeIsMissing(t *testing.T) {
	clearHomeEnv(t)
	got, err := HomeRelative(".testapp")()
	assert.Error(t, err, "HomeRelative with no home")
	assert.Empty(t, got)
}

// ///////////////////////////////////////////////
// Fixed
// ///////////////////////////////////////////////

func TestFixed(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{name: "absolute unix", dir: "/data/myapp"},
		{name: "absolute windows", dir: `C:\data\myapp`},
		{name: "relative", dir: "relative/path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Fixed(tt.dir)()
			require.NoError(t, err)
			assert.Equal(t, tt.dir, got)
		})
	}
}

// ///////////////////////////////////////////////
// EnvOr
// ///////////////////////////////////////////////

func TestEnvOr(t *testing.T) {
	const envKey = "TEST_PATHS_ENVVAR"
	fallback := Fixed("/fallback")

	t.Run("uses env when set", func(t *testing.T) {
		t.Setenv(envKey, "/from-env")
		got, err := EnvOr(envKey, fallback)()
		require.NoError(t, err)
		assert.Equal(t, "/from-env", got, "EnvOr with env set")
	})

	t.Run("uses fallback when unset", func(t *testing.T) {
		got, err := EnvOr(envKey, fallback)()
		require.NoError(t, err)
		assert.Equal(t, "/fallback", got, "EnvOr with env unset")
	})

	t.Run("uses fallback when empty", func(t *testing.T) {
		t.Setenv(envKey, "")
		got, err := EnvOr(envKey, fallback)()
		require.NoError(t, err)
		assert.Equal(t, "/fallback", got, "EnvOr with env empty")
	})

	t.Run("propagates the fallback error", func(t *testing.T) {
		sentinel := errors.New("no home")
		failing := Resolver(func() (string, error) { return "", sentinel })
		_, err := EnvOr(envKey, failing)()
		assert.ErrorIs(t, err, sentinel, "EnvOr should propagate the fallback error")
	})
}
