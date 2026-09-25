// Package paths provides centralized path accessors for the project.
// It is a leaf package with zero module-internal imports, safe to import
// from anywhere. Keep it a leaf package: stdlib only, no internal imports.
//
// # Three directories
//
// [ConfigDir], [CacheDir] and [StateDir] resolve through each platform's own
// convention. A cache placed under a config directory gets carried by a
// roaming profile; a config placed under a cache directory gets deleted by a
// disk cleaner.
//
// # Resolving for another platform
//
// Path accessors come in two forms. The plain one uses the host's native
// separator. The *For one takes a [TargetOS] and generates a path for that
// platform instead, so install logic aimed at Windows stays testable on a
// Linux runner.
//
// TODO(kickstart): rewrite this docstring for your project. Extend this
// file with project-specific path constants and accessors.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// Log describes a registered log file.
type Log struct {
	Name string // identifier: "app", "shim", "daemon"
	// RelPath names the file inside the base dir: "app.log",
	// "logs/daemon.log". Path and PathFor refuse one that does not stay
	// there, so the field's promise is kept rather than assumed.
	RelPath string
}

// Resolver returns a project directory, or an error naming why the platform
// could not supply one. A process with no home directory has no correct
// answer, so a resolver reports rather than falling back.
type Resolver func() (string, error)

// TargetOS selects the path-separator style used when joining paths.
// Use it when generating paths destined for a platform other than the
// host (e.g., a config file generated on a Windows dev box that ships
// inside a Linux binary).
type TargetOS int

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// BinaryName is the canonical name of the project binary. It is also the
// directory name under each platform's config, cache and state root.
//
// TODO(kickstart): replace "app" with your project's binary name.
const BinaryName = "app"

// ConfigFileName is the file [ConfigPath] resolves to within a base dir.
//
// TODO(kickstart): rename if your project's config file is not TOML.
const ConfigFileName = "config.toml"

// TargetOS values.
const (
	// Host uses the host operating system's native separator.
	// Equivalent to filepath.Join.
	Host TargetOS = iota
	// Posix always uses forward slashes. Suitable for Linux, macOS,
	// and other Unix-like targets.
	Posix
	// Windows always uses backslashes.
	Windows
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// ErrOutsideBase reports a relative path that does not name something inside
// the base directory it was joined to.
var ErrOutsideBase = errors.New("paths: outside the base directory")

// LogApp is the default application log file.
//
// TODO(kickstart): rename/adjust to fit your project's logging story.
var LogApp = Log{Name: "app", RelPath: BinaryName + ".log"}

// Logs lists all log files the project writes. They resolve against
// [StateDir]: a log is neither configuration nor a regenerable cache.
//
// TODO(kickstart): extend this slice with every log your project adds.
var Logs = []Log{LogApp}

// ConfigDir returns the directory holding this project's configuration.
// It resolves through os.UserConfigDir, which reads $XDG_CONFIG_HOME on
// Linux, ~/Library/Application Support on macOS, and %AppData% on Windows.
//
// Override at runtime from a CLI flag:
//
//	if *flagConfigDir != "" {
//	    paths.ConfigDir = paths.Fixed(*flagConfigDir)
//	}
//
// TODO(kickstart): wrap in EnvOr if your deployment supplies a directory
// through the environment.
var ConfigDir Resolver = UnderUserDir(os.UserConfigDir, BinaryName)

// CacheDir returns the directory holding this project's regenerable data.
// Anything written here must survive being deleted between runs. It
// resolves through os.UserCacheDir, which reads $XDG_CACHE_HOME on Linux,
// ~/Library/Caches on macOS, and %LocalAppData% on Windows.
var CacheDir Resolver = UnderUserDir(os.UserCacheDir, BinaryName)

// StateDir returns the directory holding data that outlives a run without
// being configuration: logs, cursors, partial downloads. Linux keeps a
// separate $XDG_STATE_HOME for exactly this; macOS and Windows do not, so
// state resolves alongside config there.
var StateDir Resolver = UnderUserDir(UserStateDir, BinaryName)

// ///////////////////////////////////////////////
// Path accessors
// ///////////////////////////////////////////////

// Path returns the full path to this log file within a base directory,
// using the host operating system's native separator. It reports
// [ErrOutsideBase] for a RelPath that does not stay inside baseDir.
func (l Log) Path(baseDir string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	return filepath.Join(baseDir, l.RelPath), nil
}

// PathFor returns the full path to this log file within a base directory,
// using the separator style selected by target. Use it when generating
// paths consumed on a platform other than the host. It applies the same
// containment rule [Log.Path] does.
func (l Log) PathFor(target TargetOS, baseDir string) (string, error) {
	if err := l.check(); err != nil {
		return "", err
	}
	return joinFor(target, baseDir, l.RelPath), nil
}

// check reports a RelPath that does not name something inside the base
// directory. A Log is a declaration in this package today, so the guard costs
// nothing; it exists because the field's own doc says "relative to base dir"
// and a bare Join promises that without keeping it.
//
// filepath.IsLocal answers most of it: it refuses an absolute path, a rooted
// one, any path climbing out with "..", and a Windows reserved device name.
// It answers true for ".", which names the base directory rather than
// something in it, so that case is refused here by name.
func (l Log) check() error {
	if l.RelPath == "." || !filepath.IsLocal(l.RelPath) {
		return fmt.Errorf("%w: log %q declares %q", ErrOutsideBase, l.Name, l.RelPath)
	}
	return nil
}

// HomeDir returns the user's home directory.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	return home, nil
}

// UserStateDir returns the platform's per-user state root, the directory
// holding data that outlives a run without being configuration. Linux reads
// $XDG_STATE_HOME, defaulting to ~/.local/state. Every other platform
// delegates to os.UserConfigDir, which is where those platforms keep it.
func UserStateDir() (string, error) {
	if runtime.GOOS != "linux" {
		return os.UserConfigDir()
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

// ConfigPath returns the path to the config file within a base directory,
// using the host operating system's native separator.
func ConfigPath(baseDir string) string {
	return filepath.Join(baseDir, ConfigFileName)
}

// ConfigPathFor returns the path to the config file within a base directory,
// using the separator style selected by target.
func ConfigPathFor(target TargetOS, baseDir string) string {
	return joinFor(target, baseDir, ConfigFileName)
}

// ///////////////////////////////////////////////
// Resolver helpers
// ///////////////////////////////////////////////

// UnderUserDir returns a resolver that joins a platform root with rel. Pass
// os.UserConfigDir, os.UserCacheDir or [UserStateDir] as root.
//
//	var ConfigDir = UnderUserDir(os.UserConfigDir, "myapp")
func UnderUserDir(root func() (string, error), rel string) Resolver {
	return func() (string, error) {
		dir, err := root()
		if err != nil {
			return "", fmt.Errorf("resolving the platform directory for %q: %w", rel, err)
		}
		return filepath.Join(dir, rel), nil
	}
}

// HomeRelative returns a resolver that joins the home directory with rel.
// Prefer [UnderUserDir] unless the project genuinely wants a dotfile
// directory rather than the platform's own location.
//
//	var ConfigDir = HomeRelative(".myapp") // ~/.myapp
func HomeRelative(rel string) Resolver {
	return func() (string, error) {
		home, err := HomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, rel), nil
	}
}

// Fixed returns a resolver that always returns dir.
//
//	var ConfigDir = Fixed("/etc/myapp")
func Fixed(dir string) Resolver {
	return func() (string, error) { return dir, nil }
}

// EnvOr returns a resolver that reads envKey, falling back to fallback.
// An empty or unset variable falls through, so exporting the variable empty
// is the same as not setting it.
//
//	var ConfigDir = EnvOr("MYAPP_CONFIG", UnderUserDir(os.UserConfigDir, "myapp"))
func EnvOr(envKey string, fallback Resolver) Resolver {
	return func() (string, error) {
		if v := os.Getenv(envKey); v != "" {
			return v, nil
		}
		return fallback()
	}
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// joinFor joins parts using the separator style selected by target.
// Posix always uses forward slashes; Windows always uses backslashes;
// Host delegates to filepath.Join.
func joinFor(target TargetOS, parts ...string) string {
	switch target {
	case Posix:
		return path.Join(parts...)
	case Windows:
		return strings.ReplaceAll(path.Join(parts...), "/", `\`)
	default:
		return filepath.Join(parts...)
	}
}
