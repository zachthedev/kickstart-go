// Package logger builds the project's [log/slog] logger over a rotating file.
//
// The file leg writes JSON in UTC. The console leg writes logfmt in local
// time to whatever [Options.Console] names; leave it nil to write only the
// file.
//
// Levels are slog's four. This package adds none, so callers hold a plain
// [slog.Logger] that every slog-aware library accepts.
//
// # Path is a trust boundary
//
// The rotating writer renames whatever already occupies [Options.Path] aside
// and creates a fresh log in its place, so a Path naming something other than
// this project's log file destroys it. [New] rejects a Path resolving to a
// directory, a symlink or a device, and one path carries one open logger at a
// time. Neither check tells an old log from an unrelated regular file, so a
// Path assembled from configuration or a command-line flag is the caller's to
// validate.
//
// # Errors after construction are invisible
//
// [slog.Logger] discards the error its handler returns, so nothing a write
// reports reaches the caller. [New] therefore opens the file and validates
// every rotation setting up front, which is where a bad configuration
// surfaces. A write failing later is dropped silently.
//
// TODO(kickstart): adjust the defaults in [Options] to fit your project.
package logger

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DeRuina/timberjack"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// Options configures [New]. Every field except Path has a documented
// default, so Options{Path: p} is a complete configuration.
type Options struct {
	// Path is the log file. Parent directories are created. It must name a
	// regular file or nothing at all; the package doc says why it is a trust
	// boundary.
	Path string

	// Level is the minimum level both handlers emit. The zero value is
	// [slog.LevelInfo].
	Level slog.Level

	// Console receives human-readable logfmt in local time. Nil writes
	// only the JSON file.
	Console io.Writer

	// ///// Rotation /////

	// MaxSizeMB rotates once a write would exceed it. Zero means
	// timberjack's own default of 100. Negative is an error.
	MaxSizeMB int

	// MaxBackups caps retained rotated files. Zero retains every one.
	MaxBackups int

	// MaxAgeDays discards rotated files older than this. Zero keeps them
	// regardless of age.
	MaxAgeDays int

	// RotationInterval rotates once this much time has elapsed since the
	// last rotation, whatever the file size. Zero disables it.
	RotationInterval time.Duration

	// RotateAt rotates at each wall-clock time of day, as "15:04".
	RotateAt []string

	// RotateAtMinutes rotates at each named minute past the hour, 0 to 59.
	RotateAtMinutes []int

	// Compression compresses rotated files: [CompressionNone],
	// [CompressionGzip] or [CompressionZstd]. Empty means none.
	Compression string

	// BackupTimeFormat is the timestamp layout in a rotated file's name.
	// Empty means timberjack's default of "2006-01-02T15-04-05.000".
	BackupTimeFormat string

	// AppendTimeAfterExt names backups "app.log-<stamp>" rather than
	// "app-<stamp>.log", which sorts better under shell completion.
	AppendTimeAfterExt bool

	// LocalTime stamps rotated file names in local time. The default is
	// UTC, matching the timestamps written inside the file.
	LocalTime bool

	// FileMode is the permission for created files. Zero means 0o600. A file
	// already at Path keeps the mode it has, and Windows carries no
	// permission bits to set.
	FileMode os.FileMode
}

// handle closes the rotating writer and releases its claim on the path.
type handle struct {
	rotator *timberjack.Logger
	path    string
	once    sync.Once
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// Compression settings [Options.Compression] accepts. The rotating writer
// warns on the process's own stderr about anything else, so [New] rejects
// the rest before it can.
const (
	CompressionNone = "none"
	CompressionGzip = "gzip"
	CompressionZstd = "zstd"
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// timeOfDay matches the "15:04" shape [Options.RotateAt] accepts.
var timeOfDay = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// openPaths holds every path an unclosed logger owns. The rotating writer
// opens the live file without O_APPEND and writes at its own offset, so two
// loggers on one path overwrite each other's records rather than interleave.
var (
	openPathsMu sync.Mutex
	openPaths   = map[string]struct{}{}
)

// ///////////////////////////////////////////////
// Constructors
// ///////////////////////////////////////////////

// New returns a logger writing JSON to a rotating file at opts.Path, plus
// logfmt to opts.Console when that is set.
//
// The returned [io.Closer] must be closed. Every logger runs a background
// goroutine that only Close stops, whether or not rotation is scheduled.
// Close releases the path for reuse; it does not stop a later write from
// reopening the file.
func New(opts Options) (*slog.Logger, io.Closer, error) {
	if err := opts.validate(); err != nil {
		return nil, nil, err
	}

	path, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("logger: resolving %q: %w", opts.Path, err)
	}
	if err := checkTarget(path); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}

	mode := opts.FileMode
	if mode == 0 {
		mode = 0o600
	}
	// Opening here is what surfaces an unusable path, since every later write
	// goes through slog and its error is discarded. It also creates the file
	// at mode, which the rotating writer's append path then preserves.
	if err := touch(path, mode); err != nil {
		return nil, nil, err
	}
	if err := claimPath(path); err != nil {
		return nil, nil, err
	}

	rotator := &timberjack.Logger{
		Filename:           path,
		MaxSize:            opts.MaxSizeMB,
		MaxBackups:         opts.MaxBackups,
		MaxAge:             opts.MaxAgeDays,
		LocalTime:          opts.LocalTime,
		Compression:        opts.Compression,
		RotationInterval:   opts.RotationInterval,
		RotateAt:           opts.RotateAt,
		RotateAtMinutes:    opts.RotateAtMinutes,
		BackupTimeFormat:   opts.BackupTimeFormat,
		AppendTimeAfterExt: opts.AppendTimeAfterExt,
		FileMode:           mode,
	}

	h := fileHandler(rotator, opts.Level)
	if opts.Console != nil {
		h = slog.NewMultiHandler(h, consoleHandler(opts.Console, opts.Level))
	}
	return slog.New(h), &handle{rotator: rotator, path: path}, nil
}

// ///////////////////////////////////////////////
// handle methods
// ///////////////////////////////////////////////

// Close stops the rotating writer's goroutine and releases the path. Calling
// it more than once is safe and does nothing after the first.
func (h *handle) Close() error {
	var err error
	h.once.Do(func() {
		err = h.rotator.Close()
		releasePath(h.path)
	})
	return err
}

// ///////////////////////////////////////////////
// Options methods
// ///////////////////////////////////////////////

// validate reports the first unusable field. The rotating writer takes a bad
// value, prints a warning on the process's own stderr and carries on with the
// feature disabled, which for a command-line program corrupts the output it
// was writing.
func (o Options) validate() error {
	if o.Path == "" {
		return errors.New("logger: Options.Path is required")
	}
	for _, f := range []struct {
		name string
		n    int
	}{
		{"MaxSizeMB", o.MaxSizeMB},
		{"MaxBackups", o.MaxBackups},
		{"MaxAgeDays", o.MaxAgeDays},
	} {
		if f.n < 0 {
			return fmt.Errorf("logger: Options.%s is %d, want zero or more", f.name, f.n)
		}
	}
	if o.RotationInterval < 0 {
		return fmt.Errorf("logger: Options.RotationInterval is %s, want zero or more", o.RotationInterval)
	}
	switch o.Compression {
	case "", CompressionNone, CompressionGzip, CompressionZstd:
	default:
		return fmt.Errorf("logger: Options.Compression is %q, want %q, %q or %q",
			o.Compression, CompressionNone, CompressionGzip, CompressionZstd)
	}
	for _, at := range o.RotateAt {
		if !timeOfDay.MatchString(at) {
			return fmt.Errorf("logger: Options.RotateAt has %q, want a time of day like %q", at, "15:04")
		}
	}
	for _, m := range o.RotateAtMinutes {
		if m < 0 || m > 59 {
			return fmt.Errorf("logger: Options.RotateAtMinutes has %d, want 0 to 59", m)
		}
	}
	return nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// checkTarget refuses a path the rotating writer would rename aside. Nothing
// there is fine, since the log has to start somewhere.
func checkTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logger: inspecting %s: %w", path, err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("logger: %s is a symlink, and rotation would replace it and orphan its target", path)
	case info.IsDir():
		return fmt.Errorf("logger: %s is a directory, and rotation would rename it aside", path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("logger: %s is not a regular file (%s)", path, info.Mode().Type())
	}
	return nil
}

// touch creates the log file if it is absent and proves it can be written.
func touch(path string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return fmt.Errorf("logger: opening %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("logger: closing %s: %w", path, err)
	}
	return nil
}

// claimPath records that a logger owns path, or names the conflict.
func claimPath(path string) error {
	key := claimKey(path)
	openPathsMu.Lock()
	defer openPathsMu.Unlock()
	if _, held := openPaths[key]; held {
		return fmt.Errorf("logger: %s already has an open logger, so close that one first", path)
	}
	openPaths[key] = struct{}{}
	return nil
}

// releasePath gives up the claim taken by claimPath.
func releasePath(path string) {
	openPathsMu.Lock()
	defer openPathsMu.Unlock()
	delete(openPaths, claimKey(path))
}

// claimKey folds case where the platform's filesystem does, so two spellings
// of one path make one claim.
func claimKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(clean)
	}
	return clean
}

// fileHandler builds the JSON handler for the rotating file. scripts/build.sh
// passes -trimpath, which reduces the source path AddSource records to a
// module-relative one; a build without that flag records an absolute path.
func fileHandler(w io.Writer, level slog.Level) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: utcTime,
	})
}

// consoleHandler builds the logfmt handler for terminal output, keeping the
// record's local timestamp and omitting source.
func consoleHandler(w io.Writer, level slog.Level) slog.Handler {
	return slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
}

// utcTime rewrites the record's own timestamp to UTC. The guard is position
// and kind rather than provenance: a top-level attribute a caller names
// "time" holding a [time.Time] is rewritten too, which slog gives no way to
// distinguish. Inside a group nothing is touched.
func utcTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.Time(slog.TimeKey, a.Value.Time().UTC())
	}
	return a
}
