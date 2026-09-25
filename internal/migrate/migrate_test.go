package migrate

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ///////////////////////////////////////////////
// Helpers
// ///////////////////////////////////////////////

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	assert.Panics(t, fn)
}

// ///////////////////////////////////////////////
// NewBytes + WithLogger
// ///////////////////////////////////////////////

func TestNewBytes_SetsCurrentVersion(t *testing.T) {
	r := NewBytes(5)
	assert.Equal(t, 5, r.CurrentVersion, "CurrentVersion")
}

func TestBytesRegistry_WithLogger(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, nil))
	r := NewBytes(1).WithLogger(l)
	assert.Equal(t, l, r.Logger, "WithLogger did not assign the logger")
}

// ///////////////////////////////////////////////
// BytesRegistry.Register
// ///////////////////////////////////////////////

func TestBytesRegistry_Register_SortsByVersion(t *testing.T) {
	r := NewBytes(3)
	r.Register(BytesMigration{Version: 3, Description: "third"})
	r.Register(BytesMigration{Version: 1, Description: "first"})
	r.Register(BytesMigration{Version: 2, Description: "second"})

	got := make([]int, len(r.Migrations))
	for i, m := range r.Migrations {
		got[i] = m.Version
	}
	assert.Equal(t, []int{1, 2, 3}, got, "Register did not sort by version")
}

func TestBytesRegistry_Register_DuplicatePanics(t *testing.T) {
	r := NewBytes(1)
	r.Register(BytesMigration{Version: 1, Description: "first"})
	assertPanics(t, func() {
		r.Register(BytesMigration{Version: 1, Description: "duplicate"})
	})
}

// ///////////////////////////////////////////////
// BytesRegistry.NeedsMigration
// ///////////////////////////////////////////////

func TestBytesRegistry_NeedsMigration(t *testing.T) {
	r := NewBytes(2)
	r.Register(BytesMigration{Version: 2, Upgrade: identityUpgrade})

	tests := []struct {
		name    string
		version int
		force   bool
		want    bool
	}{
		{name: "behind", version: 0, want: true},
		{name: "one behind", version: 1, want: true},
		{name: "current", version: 2, want: false},
		{name: "ahead", version: 3, want: false},
		{name: "current with force", version: 2, force: true, want: true},
		{name: "ahead with force", version: 3, force: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, r.NeedsMigration(tt.version, tt.force))
		})
	}
}

func TestBytesRegistry_NeedsMigration_ForceWithNoMigrations(t *testing.T) {
	r := NewBytes(1)
	assert.False(t, r.NeedsMigration(1, true), "NeedsMigration with force but no migrations = true")
}

// ///////////////////////////////////////////////
// BytesRegistry.Run
// ///////////////////////////////////////////////

func TestBytesRegistry_Run_AppliesPendingMigrations(t *testing.T) {
	r := NewBytes(2)
	r.Register(BytesMigration{
		Version:     1,
		Description: "init",
		Upgrade: func(data []byte) ([]byte, error) {
			return append(data, []byte(":v1")...), nil
		},
	})
	r.Register(BytesMigration{
		Version:     2,
		Description: "extend",
		Upgrade: func(data []byte) ([]byte, error) {
			return append(data, []byte(":v2")...), nil
		},
	})

	out, version, err := r.Run([]byte("data"), 0)
	require.NoError(t, err)
	assert.Equal(t, "data:v1:v2", string(out), "Run data")
	assert.Equal(t, 2, version, "Run version")
}

func TestBytesRegistry_Run_SkipsAppliedVersions(t *testing.T) {
	r := NewBytes(2)
	r.Register(BytesMigration{
		Version:     1,
		Description: "should be skipped",
		Upgrade: func(data []byte) ([]byte, error) {
			assert.Fail(t, "v1 upgrade should not run when fromVersion >= 1")
			return data, nil
		},
	})
	r.Register(BytesMigration{
		Version:     2,
		Description: "runs",
		Upgrade: func(data []byte) ([]byte, error) {
			return []byte("done"), nil
		},
	})

	out, version, err := r.Run([]byte("start"), 1)
	require.NoError(t, err)
	assert.Equal(t, "done", string(out), "Run data")
	assert.Equal(t, 2, version, "Run version")
}

func TestBytesRegistry_Run_ErrorStopsProgress(t *testing.T) {
	r := NewBytes(3)
	r.Register(BytesMigration{
		Version: 2,
		Upgrade: func(data []byte) ([]byte, error) {
			return nil, fmt.Errorf("v2 exploded")
		},
	})
	r.Register(BytesMigration{
		Version: 3,
		Upgrade: func(data []byte) ([]byte, error) {
			assert.Fail(t, "v3 should not run after v2 failure")
			return data, nil
		},
	})

	_, version, err := r.Run([]byte("data"), 1)
	require.Error(t, err, "Run error = nil")
	assert.Contains(t, err.Error(), "v2 exploded", "Run error")
	assert.Equal(t, 1, version, "Run version")
}

// ///////////////////////////////////////////////
// BytesRegistry.RegisterDev, RunDev, HasDev
// ///////////////////////////////////////////////

func TestBytesRegistry_RegisterDev_DuplicatePanics(t *testing.T) {
	r := NewBytes(1)
	r.RegisterDev(BytesMigration{Description: "fix timestamps"})
	assertPanics(t, func() {
		r.RegisterDev(BytesMigration{Description: "fix timestamps"})
	})
}

func TestBytesRegistry_RunDev_AppliesInRegistrationOrder(t *testing.T) {
	r := NewBytes(1)
	r.RegisterDev(BytesMigration{
		Description: "first",
		Upgrade: func(data []byte) ([]byte, error) {
			return append(data, '1'), nil
		},
	})
	r.RegisterDev(BytesMigration{
		Description: "second",
		Upgrade: func(data []byte) ([]byte, error) {
			return append(data, '2'), nil
		},
	})

	out, err := r.RunDev([]byte("x"))
	require.NoError(t, err)
	assert.Equal(t, "x12", string(out), "RunDev data")
}

func TestBytesRegistry_RunDev_NoTransforms(t *testing.T) {
	r := NewBytes(1)
	out, err := r.RunDev([]byte("unchanged"))
	require.NoError(t, err)
	assert.Equal(t, "unchanged", string(out), "RunDev data")
}

func TestBytesRegistry_RunDev_Error(t *testing.T) {
	r := NewBytes(1)
	r.RegisterDev(BytesMigration{
		Description: "boom",
		Upgrade: func(data []byte) ([]byte, error) {
			return nil, fmt.Errorf("kaboom")
		},
	})
	_, err := r.RunDev([]byte("x"))
	require.Error(t, err, "RunDev error = nil")
	assert.Contains(t, err.Error(), "boom", "RunDev error")
}

func TestBytesRegistry_HasDev(t *testing.T) {
	r := NewBytes(1)
	assert.False(t, r.HasDev(), "HasDev = true on fresh registry")
	r.RegisterDev(BytesMigration{Description: "x", Upgrade: identityUpgrade})
	assert.True(t, r.HasDev(), "HasDev = false after RegisterDev")
}

// identityUpgrade is a no-op Upgrade used where the function is required
// but the test does not exercise it.
func identityUpgrade(data []byte) ([]byte, error) { return data, nil }

// ///////////////////////////////////////////////
// Run convergence
// ///////////////////////////////////////////////

// Run must leave data that NeedsMigration reports as done. A ladder that
// does not converge rewrites the operator's file on every load.
func TestBytesRegistry_Run_Converges(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion int
		migrations     []int
		from           int
		wantVersion    int
	}{
		{name: "initial schema carries no migration", currentVersion: 1, from: 0, wantVersion: 1},
		{name: "one migration covers it", currentVersion: 1, migrations: []int{1}, from: 0, wantVersion: 1},
		{name: "CurrentVersion above the highest migration", currentVersion: 5, migrations: []int{1, 3}, from: 0, wantVersion: 5},
		{name: "already current", currentVersion: 2, migrations: []int{1, 2}, from: 2, wantVersion: 2},
		{name: "ahead of this build", currentVersion: 3, migrations: []int{1, 2, 3}, from: 99, wantVersion: 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewBytes(tt.currentVersion).WithLogger(slog.New(slog.DiscardHandler))
			for _, v := range tt.migrations {
				r.Register(BytesMigration{Version: v, Upgrade: identityUpgrade})
			}

			_, version, err := r.Run(nil, tt.from)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version, "Run version")
			assert.False(t, r.NeedsMigration(version, false),
				"Run left work outstanding, so every load repeats it")
		})
	}
}
