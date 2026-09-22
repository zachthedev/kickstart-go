package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// ///////////////////////////////////////////////
// Test helpers
// ///////////////////////////////////////////////

// newAt builds a logger under a fresh temp dir and registers its Close.
// Every case goes through it, so no case can forget the closer and leak a
// scheduled-rotation goroutine into a later test.
func newAt(t *testing.T, opts Options) (*slog.Logger, string) {
	t.Helper()
	if opts.Path == "" {
		opts.Path = filepath.Join(t.TempDir(), "app.log")
	}
	lg, closer, err := New(opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return lg, opts.Path
}

// readRecord returns the first JSON object written to path.
func readRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is a test temp dir
	require.NoError(t, err)
	line, _, found := strings.Cut(string(data), "\n")
	require.True(t, found, "log file holds no complete line: %q", string(data))

	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line is not JSON: %q", line)
	return rec
}

// backupCount returns how many files sit beside the log other than the log
// itself, which is how a rotation shows up on disk.
func backupCount(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	n := 0
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			n++
		}
	}
	return n
}

// ///////////////////////////////////////////////
// Construction
// ///////////////////////////////////////////////

func TestNew_RequiresPath(t *testing.T) {
	_, _, err := New(Options{})
	require.Error(t, err, "New accepted an empty Path")
	assert.Contains(t, err.Error(), "Path is required")
}

func TestNew_CreatesNestedDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "app.log")
	lg, _ := newAt(t, Options{Path: path})
	lg.Info("written")

	assert.FileExists(t, path)
}

func TestNew_MkdirAllFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	_, _, err := New(Options{Path: filepath.Join(blocker, "sub", "app.log")})
	require.Error(t, err, "New succeeded with a file where a directory must be")
	assert.Contains(t, err.Error(), "create log directory")
}

// ///////////////////////////////////////////////
// Record shape
// ///////////////////////////////////////////////

// The file leg exists to be parsed, so the contract is that every record is
// one JSON object carrying slog's built-in keys with attribute values in
// their own types: 200 must arrive as a number, not as "200".
func TestNew_WritesJSONRecord(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("served request", "method", "GET", "status", 200, "ratio", 0.5, "ok", true)

	rec := readRecord(t, path)
	assert.Equal(t, "INFO", rec["level"])
	assert.Equal(t, "served request", rec["msg"])
	assert.Contains(t, rec, "time")
	assert.Contains(t, rec, "source")

	assert.Equal(t, "GET", rec["method"])
	assert.InDelta(t, 200, rec["status"], 0, "status must decode as a number, not a string")
	assert.InDelta(t, 0.5, rec["ratio"], 0)
	assert.Equal(t, true, rec["ok"])
}

// Source is on so a record names the line that produced it. -trimpath makes
// the recorded path module-relative, so the assertion is that a file and a
// positive line are present rather than any absolute prefix.
func TestNew_RecordsSource(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("located")

	src, ok := readRecord(t, path)["source"].(map[string]any)
	require.True(t, ok, "source is not an object")
	assert.Contains(t, src["file"], "logger_test.go")
	assert.Positive(t, src["line"])
}

// ///////////////////////////////////////////////
// Timestamps
// ///////////////////////////////////////////////

// A local offset renders two instants an hour apart as identical wall-clock
// text across a daylight-saving transition, so the file leg is UTC whatever
// the host is set to.
func TestNew_FileTimestampsAreUTC(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("stamped")

	raw, ok := readRecord(t, path)["time"].(string)
	require.True(t, ok, "time is not a string")

	parsed, err := time.Parse(time.RFC3339Nano, raw)
	require.NoError(t, err)
	_, offset := parsed.Zone()
	assert.Zero(t, offset, "file timestamp %q carries a non-zero UTC offset", raw)
}

// The console leg is read by a person watching it now, so it keeps the
// host's own offset rather than making them convert.
func TestNew_ConsoleTimestampsAreLocal(t *testing.T) {
	var console bytes.Buffer
	lg, _ := newAt(t, Options{Console: &console})
	lg.Info("stamped")

	_, rest, found := strings.Cut(console.String(), "time=")
	require.True(t, found, "console line has no time key: %q", console.String())
	raw, _, _ := strings.Cut(rest, " ")

	parsed, err := time.Parse(time.RFC3339Nano, raw)
	require.NoError(t, err)
	_, got := parsed.Zone()
	_, want := time.Now().Zone()
	assert.Equal(t, want, got, "console timestamp %q is not in local time", raw)
}

// ///////////////////////////////////////////////
// Levels and fan-out
// ///////////////////////////////////////////////

func TestNew_LevelFiltering(t *testing.T) {
	tests := []struct {
		name    string
		min     slog.Level
		emit    slog.Level
		wantLog bool
	}{
		{"debug suppressed at info", slog.LevelInfo, slog.LevelDebug, false},
		{"info emitted at info", slog.LevelInfo, slog.LevelInfo, true},
		{"warn emitted at info", slog.LevelInfo, slog.LevelWarn, true},
		{"error emitted at info", slog.LevelInfo, slog.LevelError, true},
		{"debug emitted at debug", slog.LevelDebug, slog.LevelDebug, true},
		{"info suppressed at error", slog.LevelError, slog.LevelInfo, false},
		{"error emitted at error", slog.LevelError, slog.LevelError, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var console bytes.Buffer
			lg, path := newAt(t, Options{Level: tt.min, Console: &console})
			lg.Log(t.Context(), tt.emit, "probe")

			// Both legs take the same Level, so both are asserted: a
			// console handler built with its own options would otherwise
			// filter at Info whatever the caller asked for.
			data, err := os.ReadFile(path) //nolint:gosec // path is a test temp dir
			require.NoError(t, err, "New did not create the log file")
			if tt.wantLog {
				assert.Contains(t, string(data), "probe", "file leg")
				assert.Contains(t, console.String(), "probe", "console leg")
				return
			}
			assert.NotContains(t, string(data), "probe", "file leg")
			assert.NotContains(t, console.String(), "probe", "console leg")
		})
	}
}

func TestNew_FansOutToBothHandlers(t *testing.T) {
	var console bytes.Buffer
	lg, path := newAt(t, Options{Console: &console})
	lg.Info("both legs", "k", "v")

	assert.Equal(t, "both legs", readRecord(t, path)["msg"], "file leg missed the record")
	assert.Contains(t, console.String(), "both legs", "console leg missed the record")
	assert.Contains(t, console.String(), "k=v")
}

// ///////////////////////////////////////////////
// Encoding
// ///////////////////////////////////////////////

// A value carrying the delimiters a line-oriented format would use must
// survive intact. Log values routinely hold user input, paths and error
// strings, so a format that cannot escape them corrupts the record.
func TestNew_PreservesDelimiterCharacters(t *testing.T) {
	hostile := `value with , comma and | pipe and "quotes" and =equals`
	lg, path := newAt(t, Options{})
	lg.Info("user input", "note", hostile, "next", "second")

	rec := readRecord(t, path)
	assert.Equal(t, hostile, rec["note"], "delimiters in a value did not round-trip")
	assert.Equal(t, "second", rec["next"], "the attribute after a hostile value was lost")
}

// The repository forces LF through .gitattributes; the log file must agree
// on every platform, since it is read by tools that do not expect CR.
func TestNew_WritesLFOnEveryPlatform(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("first")
	lg.Info("second")

	data, err := os.ReadFile(path) //nolint:gosec // path is a test temp dir
	require.NoError(t, err)
	assert.NotContains(t, string(data), "\r", "log file contains CR")
	assert.Equal(t, 2, strings.Count(string(data), "\n"), "expected one LF per record")
}

// A log is the project's to read, so the default tightens timberjack's own
// 0o640 to 0o600. Windows does not carry Unix permission bits, and Go
// reports a synthetic mode there, so this asserts only where it means
// something.
func TestNew_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits to assert on")
	}
	tests := []struct {
		name string
		set  os.FileMode
		want os.FileMode
	}{
		{"zero value tightens to 0600", 0, 0o600},
		{"explicit mode is honored", 0o640, 0o640},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lg, path := newAt(t, Options{FileMode: tt.set})
			lg.Info("written")

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, info.Mode().Perm())
		})
	}
}

// ///////////////////////////////////////////////
// Rotation
// ///////////////////////////////////////////////

func TestNew_RotatesBySize(t *testing.T) {
	lg, path := newAt(t, Options{MaxSizeMB: 1})
	payload := strings.Repeat("x", 600*1024)

	lg.Info("first", "payload", payload)
	require.Zero(t, backupCount(t, path), "rotated before the size limit was reached")

	lg.Info("second", "payload", payload)
	assert.Equal(t, 1, backupCount(t, path), "the write crossing the size limit did not rotate")
}

func TestNew_CompressesRotatedFiles(t *testing.T) {
	lg, path := newAt(t, Options{MaxSizeMB: 1, Compression: "zstd"})
	payload := strings.Repeat("x", 600*1024)
	lg.Info("first", "payload", payload)
	lg.Info("second", "payload", payload)

	// Compression runs in a goroutine after the rotation returns.
	require.Eventually(t, func() bool {
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			return false
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".zst") {
				return true
			}
		}
		return false
	}, 10*time.Second, 25*time.Millisecond, "no .zst backup appeared after rotation")
}

// Scheduled rotation runs a goroutine that only Close stops, so a caller
// that drops the closer leaks it for the life of the process.
func TestNew_CloseStopsScheduledRotation(t *testing.T) {
	defer goleak.VerifyNone(t)

	lg, closer, err := New(Options{
		Path:            filepath.Join(t.TempDir(), "app.log"),
		RotateAtMinutes: []int{0, 30},
	})
	require.NoError(t, err)
	lg.Info("scheduled rotation armed")

	require.NoError(t, closer.Close())
}

// ///////////////////////////////////////////////
// Attribute rewriting
// ///////////////////////////////////////////////

// The rewrite is guarded on position and kind. A caller attribute named
// "time" holding anything other than a time.Time is left alone.
func TestUtcTime_LeavesANonTimeValueAlone(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("shadowed", "time", "not-a-timestamp")

	assert.Equal(t, "not-a-timestamp", readRecord(t, path)["time"],
		"a string attribute named time was rewritten")
}

// slog gives a ReplaceAttr no way to tell its own time attribute from a
// caller's, so a top-level time.Time named "time" is rewritten too. Asserted
// because it is the documented behaviour, not because it is desirable.
func TestUtcTime_RewritesACallerTimeValue(t *testing.T) {
	lg, path := newAt(t, Options{})
	lg.Info("shadowed", "time", time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("+0530", 5*3600+1800)))

	raw, ok := readRecord(t, path)["time"].(string)
	require.True(t, ok, "time is not a string")
	assert.Equal(t, "2026-01-01T21:34:05Z", raw,
		"a top-level time.Time named time must be converted, matching the record's own")
}

// Nothing inside a group is touched. Without the len(groups) guard this
// attribute would be rewritten, and no other case in the suite notices.
func TestUtcTime_LeavesGroupedAttributesAlone(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("+0530", 5*3600+1800))
	lg, path := newAt(t, Options{})
	lg.WithGroup("req").Info("grouped", "time", stamp)

	group, ok := readRecord(t, path)["req"].(map[string]any)
	require.True(t, ok, "the group is not an object")
	assert.Equal(t, stamp.Format(time.RFC3339Nano), group["time"],
		"a grouped time attribute was rewritten")
}

// ///////////////////////////////////////////////
// Path is a trust boundary
// ///////////////////////////////////////////////

// The rotating writer renames whatever occupies Path aside and writes a
// fresh log in its place, so a directory there is destroyed along with
// everything under it.
func TestNew_RefusesADirectoryAtPath(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "app.log")
	require.NoError(t, os.MkdirAll(victim, 0o750))
	keep := filepath.Join(victim, "important.txt")
	require.NoError(t, os.WriteFile(keep, []byte("do not lose me"), 0o600))

	_, _, err := New(Options{Path: victim})
	require.Error(t, err, "New accepted a directory as its log file")
	assert.Contains(t, err.Error(), "rotation would rename it aside")
	assert.FileExists(t, keep, "the directory's contents were moved aside")
}

func TestNew_RefusesASymlinkAtPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o600))

	link := filepath.Join(dir, "app.log")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable to this process: %v", err)
	}

	_, _, err := New(Options{Path: link})
	require.Error(t, err, "New accepted a symlink as its log file")
	assert.Contains(t, err.Error(), "orphan its target")
}

// One path carries one open logger. The rotating writer opens the live file
// without O_APPEND, so a second logger writes at its own offset and the two
// overwrite each other rather than interleaving.
func TestNew_RefusesASecondLoggerOnOnePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	_, closer, err := New(Options{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	_, _, err = New(Options{Path: path})
	require.Error(t, err, "New opened a second logger on one path")
	assert.Contains(t, err.Error(), "already has an open logger")

	// Closing the first hands the path back.
	require.NoError(t, closer.Close())
	_, second, err := New(Options{Path: path})
	require.NoError(t, err, "the path was not released by Close")
	require.NoError(t, second.Close())
}

// A path spelled two ways is one claim wherever the filesystem folds case.
func TestClaimPath_FoldsCaseWhereThePlatformDoes(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("this filesystem is case-sensitive, so the two spellings are two paths")
	}
	dir := t.TempDir()
	lower := filepath.Join(dir, "app.log")
	_, closer, err := New(Options{Path: lower})
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	_, _, err = New(Options{Path: filepath.Join(dir, "APP.LOG")})
	require.Error(t, err, "a differently-cased spelling opened a second logger")
}

// ///////////////////////////////////////////////
// Configuration is validated up front
// ///////////////////////////////////////////////

// slog discards the error a handler returns, so a setting the rotating
// writer would reject at write time has to fail here instead. Left to it,
// every one of these prints a warning on the process's own stderr and
// carries on with the feature silently disabled.
func TestOptions_Validate(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"negative size", Options{MaxSizeMB: -1}, "MaxSizeMB"},
		{"negative backups", Options{MaxBackups: -1}, "MaxBackups"},
		{"negative age", Options{MaxAgeDays: -1}, "MaxAgeDays"},
		{"negative interval", Options{RotationInterval: -time.Second}, "RotationInterval"},
		{"unknown compression", Options{Compression: "gz"}, "Compression"},
		{"hour out of range", Options{RotateAt: []string{"25:00"}}, "RotateAt"},
		{"minute out of range", Options{RotateAt: []string{"12:99"}}, "RotateAt"},
		{"not a time at all", Options{RotateAt: []string{"not a time"}}, "RotateAt"},
		{"minute below zero", Options{RotateAtMinutes: []int{-1}}, "RotateAtMinutes"},
		{"minute above range", Options{RotateAtMinutes: []int{60}}, "RotateAtMinutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Path = filepath.Join(t.TempDir(), "app.log")
			_, _, err := New(tt.opts)
			require.Error(t, err, "New accepted %s", tt.name)
			assert.Contains(t, err.Error(), tt.want)
			assert.NoFileExists(t, tt.opts.Path, "a rejected configuration created the log file")
		})
	}
}

func TestOptions_ValidateAcceptsEveryCompression(t *testing.T) {
	for _, c := range []string{"", CompressionNone, CompressionGzip, CompressionZstd} {
		t.Run("compression "+c, func(t *testing.T) {
			_, closer, err := New(Options{Path: filepath.Join(t.TempDir(), "app.log"), Compression: c})
			require.NoError(t, err)
			require.NoError(t, closer.Close())
		})
	}
}

// A boundary value on each side of every accepted range.
func TestOptions_ValidateAcceptsBoundaries(t *testing.T) {
	_, closer, err := New(Options{
		Path:            filepath.Join(t.TempDir(), "app.log"),
		RotateAt:        []string{"00:00", "23:59"},
		RotateAtMinutes: []int{0, 59},
	})
	require.NoError(t, err)
	require.NoError(t, closer.Close())
}

// ///////////////////////////////////////////////
// Close
// ///////////////////////////////////////////////

// Every logger runs a rotating-writer goroutine, not only one with rotation
// scheduled, so the returned closer always has work to do.
func TestNew_CloseStopsTheGoroutineWithNoRotationConfigured(t *testing.T) {
	defer goleak.VerifyNone(t)

	lg, closer, err := New(Options{Path: filepath.Join(t.TempDir(), "app.log")})
	require.NoError(t, err)
	lg.Info("one record is enough to start the mill")

	require.NoError(t, closer.Close())
}

func TestHandle_CloseIsIdempotent(t *testing.T) {
	_, closer, err := New(Options{Path: filepath.Join(t.TempDir(), "app.log")})
	require.NoError(t, err)

	require.NoError(t, closer.Close())
	assert.NoError(t, closer.Close(), "a second Close reported an error")
}
