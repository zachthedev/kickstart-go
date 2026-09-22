package migrate

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ///////////////////////////////////////////////
// NewJSON + WithVersionKey
// ///////////////////////////////////////////////

func TestNewJSON_SetsCurrentVersion(t *testing.T) {
	r := NewJSON(3)
	assert.Equal(t, 3, r.CurrentVersion, "CurrentVersion")
}

func TestJSONRegistry_WithVersionKey(t *testing.T) {
	r := NewJSON(1).WithVersionKey("v")
	assert.Equal(t, "v", r.VersionKey, "VersionKey")
}

// ///////////////////////////////////////////////
// JSONRegistry.Register
// ///////////////////////////////////////////////

func TestJSONRegistry_Register_SortsByVersion(t *testing.T) {
	r := NewJSON(3)
	r.Register(JSONMigration{Version: 3})
	r.Register(JSONMigration{Version: 1})
	r.Register(JSONMigration{Version: 2})

	got := make([]int, len(r.Migrations))
	for i, m := range r.Migrations {
		got[i] = m.Version
	}
	assert.Equal(t, []int{1, 2, 3}, got, "Register did not sort by version")
}

func TestJSONRegistry_Register_DuplicatePanics(t *testing.T) {
	r := NewJSON(1)
	r.Register(JSONMigration{Version: 1})
	assertPanics(t, func() {
		r.Register(JSONMigration{Version: 1})
	})
}

// ///////////////////////////////////////////////
// JSONRegistry.NeedsMigration
// ///////////////////////////////////////////////

func TestJSONRegistry_NeedsMigration(t *testing.T) {
	r := NewJSON(2)
	r.Register(JSONMigration{Version: 2, Upgrade: identityJSONUpgrade})

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "empty", data: "", want: true},
		{name: "no version key", data: `{"name": "x"}`, want: true},
		{name: "version behind", data: `{"version": 1}`, want: true},
		{name: "at current", data: `{"version": 2}`, want: false},
		{name: "ahead", data: `{"version": 3}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.NeedsMigration([]byte(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJSONRegistry_NeedsMigration_InvalidJSON(t *testing.T) {
	r := NewJSON(1)
	_, err := r.NeedsMigration([]byte(`{broken`))
	assert.Error(t, err, "NeedsMigration error = nil")
}

// ///////////////////////////////////////////////
// JSONRegistry.Run
// ///////////////////////////////////////////////

func TestJSONRegistry_Run_AppliesAndWritesVersion(t *testing.T) {
	r := NewJSON(2)
	r.Register(JSONMigration{
		Version: 1,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			doc["name"] = "initialized"
			return doc, nil
		},
	})
	r.Register(JSONMigration{
		Version: 2,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			doc["count"] = 5
			return doc, nil
		},
	})

	out, version, err := r.Run(nil)
	require.NoError(t, err)
	assert.Equal(t, 2, version, "Run version")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	gotVersion, ok := parsed["version"].(float64)
	require.True(t, ok)
	assert.Equal(t, 2, int(gotVersion), "output version")
	assert.Equal(t, "initialized", parsed["name"], "output name")
}

func TestJSONRegistry_Run_CustomVersionKey(t *testing.T) {
	r := NewJSON(1).WithVersionKey("v")
	r.Register(JSONMigration{
		Version: 1,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			doc["key"] = "value"
			return doc, nil
		},
	})

	out, _, err := r.Run(nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	assert.Contains(t, parsed, "v")
	assert.NotContains(t, parsed, "version")
}

func TestJSONRegistry_Run_SkipsAppliedMigrations(t *testing.T) {
	r := NewJSON(2)
	r.Register(JSONMigration{
		Version: 1,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			assert.Fail(t, "v1 upgrade ran when version is already 1")
			return doc, nil
		},
	})
	r.Register(JSONMigration{
		Version: 2,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			doc["upgraded"] = true
			return doc, nil
		},
	})

	input := []byte(`{"version": 1, "existing": "x"}`)
	out, version, err := r.Run(input)
	require.NoError(t, err)
	assert.Equal(t, 2, version, "Run version")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	assert.Equal(t, "x", parsed["existing"])
	assert.Equal(t, true, parsed["upgraded"])
}

func TestJSONRegistry_Run_UpgradeError(t *testing.T) {
	r := NewJSON(1)
	r.Register(JSONMigration{
		Version: 1,
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			return nil, fmt.Errorf("upgrade failed")
		},
	})
	_, version, err := r.Run(nil)
	require.Error(t, err, "Run error = nil")
	assert.Equal(t, 0, version, "Run version")
}

func TestJSONRegistry_Run_InvalidVersionType(t *testing.T) {
	r := NewJSON(1)
	_, _, err := r.Run([]byte(`{"version": "not a number"}`))
	assert.Error(t, err, "Run error = nil")
}

// ///////////////////////////////////////////////
// JSONRegistry.RunDev
// ///////////////////////////////////////////////

func TestJSONRegistry_RunDev_AppliesTransforms(t *testing.T) {
	r := NewJSON(1)
	r.RegisterDev(JSONMigration{
		Description: "rename key",
		Upgrade: func(doc map[string]any) (map[string]any, error) {
			if v, ok := doc["old"]; ok {
				doc["new"] = v
				delete(doc, "old")
			}
			return doc, nil
		},
	})

	out, err := r.RunDev([]byte(`{"old": "value"}`))
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))
	assert.Equal(t, "value", parsed["new"], "RunDev output new")
	assert.NotContains(t, parsed, "old")
}

// identityJSONUpgrade is a no-op Upgrade used where the function is required
// but the test does not exercise it.
func identityJSONUpgrade(doc map[string]any) (map[string]any, error) { return doc, nil }

// ///////////////////////////////////////////////
// Run convergence
// ///////////////////////////////////////////////

// Run must leave a document that NeedsMigration reports as done. A ladder
// that does not converge rewrites the operator's file on every load.
func TestJSONRegistry_Run_Converges(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion int
		migrations     []int
		input          string
		wantVersion    int
	}{
		{name: "initial schema carries no migration", currentVersion: 1, input: `{}`, wantVersion: 1},
		{name: "document has no version key", currentVersion: 1, migrations: []int{1}, input: `{"name":"x"}`, wantVersion: 1},
		{name: "CurrentVersion above the highest migration", currentVersion: 5, migrations: []int{1, 3}, input: `{}`, wantVersion: 5},
		{name: "already current", currentVersion: 2, migrations: []int{1, 2}, input: `{"version":2}`, wantVersion: 2},
		{name: "ahead of this build", currentVersion: 3, migrations: []int{1, 2, 3}, input: `{"version":99}`, wantVersion: 99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewJSON(tt.currentVersion).WithLogger(slog.New(slog.DiscardHandler))
			for _, v := range tt.migrations {
				r.Register(JSONMigration{
					Version: v,
					Upgrade: func(doc map[string]any) (map[string]any, error) { return doc, nil },
				})
			}

			out, version, err := r.Run([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version, "Run version")

			needs, err := r.NeedsMigration(out)
			require.NoError(t, err)
			assert.False(t, needs, "Run left work outstanding, so every load repeats it")
		})
	}
}
