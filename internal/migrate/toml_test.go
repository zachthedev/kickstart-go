package migrate

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2/unstable/edit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commentedConfig is the shape this registry exists for: an operator-owned
// file carrying section banners, field documentation, a trailing note, and a
// deliberate key order.
const commentedConfig = `# app config. Defaults generated; edit this copy freely.
#
# ///////////////////////////////////////////////
# app Configuration
# ///////////////////////////////////////////////

version = 1

# ///// Server /////

# Interface to bind.
# Use 0.0.0.0 for every interface.
host = "127.0.0.1" # lab box, not the deploy target

# Port to listen on.
port = 8080

[alpha]
count = 1

[beta]
count = 2

[[targets]]
name = "alpha"

[[targets]]
name = "beta"
`

// ///////////////////////////////////////////////
// Run convergence
// ///////////////////////////////////////////////

// Run must leave a document that NeedsMigration reports as done, and a
// second Run must change nothing. A ladder that does not converge rewrites
// the operator's file on every load, and this program renders on every
// prompt.
func TestTOMLRegistry_Run_Converges(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion int
		migrations     []int
		input          string
		wantVersion    int
	}{
		{
			name:           "initial schema carries no migration",
			currentVersion: 1,
			input:          "name = 'x'\n",
			wantVersion:    1,
		},
		{
			name:           "document has no version key",
			currentVersion: 1,
			migrations:     []int{1},
			input:          "name = 'x'\n",
			wantVersion:    1,
		},
		{
			name:           "CurrentVersion sits above the highest migration",
			currentVersion: 5,
			migrations:     []int{1, 3},
			input:          "name = 'x'\n",
			wantVersion:    5,
		},
		{
			name:           "document is already current",
			currentVersion: 2,
			migrations:     []int{1, 2},
			input:          "version = 2\nname = 'x'\n",
			wantVersion:    2,
		},
		{
			name:           "document is ahead of this build",
			currentVersion: 3,
			migrations:     []int{1, 2, 3},
			input:          "version = 99\nname = 'x'\n",
			wantVersion:    99,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewTOML(tt.currentVersion).WithLogger(slog.New(slog.DiscardHandler))
			for _, v := range tt.migrations {
				r.Register(TOMLMigration{
					Version: v,
					Upgrade: func(*edit.Document) error { return nil },
				})
			}

			out, version, err := r.Run([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version, "Run version")

			needs, err := r.NeedsMigration(out)
			require.NoError(t, err)
			assert.False(t, needs, "Run left work outstanding, so every load repeats it")

			again, _, err := r.Run(out)
			require.NoError(t, err)
			assert.Equal(t, string(out), string(again), "a second Run rewrote the document")
		})
	}
}

// ///////////////////////////////////////////////
// NewTOML + With* setters
// ///////////////////////////////////////////////

func TestNewTOML_SetsCurrentVersion(t *testing.T) {
	r := NewTOML(3)
	assert.Equal(t, 3, r.CurrentVersion, "CurrentVersion")
}

func TestTOMLRegistry_WithVersionKey(t *testing.T) {
	r := NewTOML(1).WithVersionKey("v")
	assert.Equal(t, "v", r.VersionKey, "VersionKey")
}

func TestTOMLRegistry_WithLogger(t *testing.T) {
	l := slog.New(slog.DiscardHandler)
	r := NewTOML(1).WithLogger(l)
	assert.Equal(t, l, r.Logger, "Logger")
}

// ///////////////////////////////////////////////
// TOMLRegistry.Register
// ///////////////////////////////////////////////

func TestTOMLRegistry_Register_SortsByVersion(t *testing.T) {
	r := NewTOML(3)
	r.Register(TOMLMigration{Version: 3})
	r.Register(TOMLMigration{Version: 1})
	r.Register(TOMLMigration{Version: 2})

	got := make([]int, len(r.Migrations))
	for i, m := range r.Migrations {
		got[i] = m.Version
	}
	assert.Equal(t, []int{1, 2, 3}, got, "Register did not sort by version")
}

func TestTOMLRegistry_Register_DuplicatePanics(t *testing.T) {
	r := NewTOML(1)
	r.Register(TOMLMigration{Version: 1})
	assertPanics(t, func() {
		r.Register(TOMLMigration{Version: 1})
	})
}

// ///////////////////////////////////////////////
// TOMLRegistry.NeedsMigration
// ///////////////////////////////////////////////

func TestTOMLRegistry_NeedsMigration(t *testing.T) {
	r := NewTOML(2)
	r.Register(TOMLMigration{Version: 2, Upgrade: identityTOMLUpgrade})

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "empty", data: "", want: true},
		{name: "no version key", data: `name = "x"`, want: true},
		{name: "version behind", data: `version = 1`, want: true},
		{name: "at current", data: `version = 2`, want: false},
		{name: "ahead", data: `version = 3`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.NeedsMigration([]byte(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTOMLRegistry_NeedsMigration_InvalidTOML(t *testing.T) {
	r := NewTOML(1)
	_, err := r.NeedsMigration([]byte("this is not = = toml"))
	assert.Error(t, err, "NeedsMigration on invalid TOML returned nil error")
}

// ///////////////////////////////////////////////
// TOMLRegistry.Run
// ///////////////////////////////////////////////

// Every byte the migrations did not address must come back unchanged: the
// banner, the section header, both field comments, the operator's trailing
// note, the blank lines, and the original key order.
func TestTOMLRegistry_Run_PreservesLayout(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version:     2,
		Description: "bind every interface",
		Upgrade: func(doc *edit.Document) error {
			return doc.Set([]string{"host"}, "0.0.0.0")
		},
	})

	out, version, err := r.Run([]byte(commentedConfig))
	require.NoError(t, err)
	require.Equal(t, 2, version, "Run version")
	got := string(out)

	for _, want := range []string{
		"# app config. Defaults generated; edit this copy freely.",
		"# ///// Server /////",
		"# Interface to bind.\n# Use 0.0.0.0 for every interface.",
		"# lab box, not the deploy target",
		"# Port to listen on.\nport = 8080",
		"[[targets]]\nname = \"alpha\"",
		"[[targets]]\nname = \"beta\"",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, `host = "127.0.0.1"`)
	assert.Contains(t, got, "version = 2")
	assert.Less(t, strings.Index(got, "host"), strings.Index(got, "port"),
		"Run reordered host and port")
}

func TestTOMLRegistry_Run_AppliesInVersionOrder(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	var order []int
	r.Register(TOMLMigration{
		Version: 2,
		Upgrade: func(doc *edit.Document) error {
			order = append(order, 2)
			return doc.Set([]string{"count"}, int64(5))
		},
	})
	r.Register(TOMLMigration{
		Version: 1,
		Upgrade: func(doc *edit.Document) error {
			order = append(order, 1)
			return doc.Set([]string{"name"}, "initialized")
		},
	})

	out, version, err := r.Run(nil)
	require.NoError(t, err)
	assert.Equal(t, 2, version, "Run version")
	assert.Equal(t, []int{1, 2}, order, "migrations ran out of version order")
	got := string(out)
	// The encoder writes string values as TOML literal strings.
	for _, want := range []string{`version = 2`, `name = 'initialized'`, `count = 5`} {
		assert.Contains(t, got, want, "Run output")
	}
}

func TestTOMLRegistry_Run_CustomVersionKey(t *testing.T) {
	r := NewTOML(1).WithVersionKey("schema_version").WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{Version: 1, Upgrade: identityTOMLUpgrade})

	out, version, err := r.Run([]byte("schema_version = 0\n"))
	require.NoError(t, err)
	assert.Equal(t, 1, version, "Run version")
	got := string(out)
	assert.Contains(t, got, "schema_version = 1", "Run output")
	// Checked per line rather than as a substring: Run writes the version key
	// into an otherwise empty document at line 1, where no preceding newline
	// exists for a substring match to anchor on.
	for line := range strings.SplitSeq(got, "\n") {
		assert.False(t, strings.HasPrefix(line, "version ="),
			"Run wrote the default version key alongside the custom one: %q", line)
	}
}

func TestTOMLRegistry_Run_SkipsAppliedMigrations(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version: 1,
		Upgrade: func(*edit.Document) error {
			assert.Fail(t, "v1 migration ran against a document already at v1")
			return nil
		},
	})
	r.Register(TOMLMigration{
		Version: 2,
		Upgrade: func(doc *edit.Document) error {
			return doc.Set([]string{"upgraded"}, true)
		},
	})

	out, version, err := r.Run([]byte("version = 1\n# keep me\nexisting = \"x\"\n"))
	require.NoError(t, err)
	assert.Equal(t, 2, version, "Run version")
	got := string(out)
	assert.Contains(t, got, "# keep me\nexisting = \"x\"")
	assert.Contains(t, got, "upgraded = true")
}

// An array of tables is addressed by 0-based index, and an index equal to
// the array's length appends a new element.
func TestTOMLRegistry_Run_IndexesArraysOfTables(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version: 2,
		Upgrade: func(doc *edit.Document) error {
			name, ok := doc.Get([]string{"targets", "1", "name"})
			if !ok {
				return errors.New("targets[1].name is missing")
			}
			if name != "beta" {
				return errors.New("targets[1].name is not beta")
			}
			return doc.Set([]string{"targets", "2", "name"}, "gamma")
		},
	})

	out, _, err := r.Run([]byte(commentedConfig))
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "name = 'gamma'")
	assert.Equal(t, 3, strings.Count(got, "[[targets]]"))
}

// A migration that must discover the document's shape reads it through
// Unmarshal and writes through Set and Delete. This is the structural
// migration a map-based registry would express as one tree transformation.
func TestTOMLRegistry_Run_RenamesAKeyInEverySection(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version:     2,
		Description: "count becomes quantity",
		Upgrade: func(doc *edit.Document) error {
			var shape map[string]any
			if err := doc.Unmarshal(&shape); err != nil {
				return err
			}
			for section := range shape {
				v, ok := doc.Get([]string{section, "count"})
				if !ok {
					continue
				}
				if err := doc.Set([]string{section, "quantity"}, v); err != nil {
					return err
				}
				doc.Delete([]string{section, "count"})
			}
			return nil
		},
	})

	out, _, err := r.Run([]byte(commentedConfig))
	require.NoError(t, err)
	got := string(out)
	assert.NotContains(t, got, "count = ")
	for _, want := range []string{"quantity = 1", "quantity = 2"} {
		assert.Contains(t, got, want)
	}
	assert.Contains(t, got, "# ///// Server /////")
}

// Set refuses to overwrite a table, so replacing one wholesale means
// deleting it first. Setting the leaf keys one at a time keeps the section
// header form rather than producing an inline table.
func TestTOMLRegistry_Run_ReplacesATableWholesale(t *testing.T) {
	r := NewTOML(2).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version: 2,
		Upgrade: func(doc *edit.Document) error {
			if err := doc.Set([]string{"alpha"}, map[string]any{"x": 1}); err == nil {
				return errors.New("Set overwrote a table instead of refusing")
			}
			if !doc.Delete([]string{"alpha"}) {
				return errors.New("alpha was not deleted")
			}
			return doc.Set([]string{"alpha", "x"}, int64(1))
		},
	})

	out, _, err := r.Run([]byte(commentedConfig))
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "[alpha]\nx = 1")
	assert.NotContains(t, got, "alpha = {")
	assert.Contains(t, got, "[beta]\ncount = 2")
}

func TestTOMLRegistry_Run_UpgradeError(t *testing.T) {
	sentinel := errors.New("boom")
	r := NewTOML(1).WithLogger(slog.New(slog.DiscardHandler))
	r.Register(TOMLMigration{
		Version:     1,
		Description: "fails",
		Upgrade:     func(*edit.Document) error { return sentinel },
	})

	out, version, err := r.Run([]byte("version = 0\n"))
	require.Error(t, err, "Run returned nil error for a failing migration")
	assert.ErrorIs(t, err, sentinel, "Run error")
	assert.Contains(t, err.Error(), "v1", "Run error")
	assert.Nil(t, out)
	assert.Equal(t, 0, version, "Run version")
}

func TestTOMLRegistry_Run_InvalidVersionType(t *testing.T) {
	r := NewTOML(1)
	_, _, err := r.Run([]byte(`version = "three"`))
	require.Error(t, err, "Run returned nil error for a string version key")
	assert.Contains(t, err.Error(), "expected integer", "Run error")
}

func TestTOMLRegistry_Run_InvalidTOML(t *testing.T) {
	r := NewTOML(1)
	_, _, err := r.Run([]byte("this is not = = toml"))
	assert.Error(t, err, "Run on invalid TOML returned nil error")
}

func TestTOMLRegistry_Run_EmptyInput(t *testing.T) {
	r := NewTOML(0)
	out, version, err := r.Run(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, version, "Run version")
	assert.Equal(t, "version = 0", strings.TrimSpace(string(out)), "Run on empty input")
}

// ///////////////////////////////////////////////
// TOMLRegistry.RunDev
// ///////////////////////////////////////////////

func TestTOMLRegistry_RunDev_AppliesTransforms(t *testing.T) {
	r := NewTOML(1)
	r.RegisterDev(TOMLMigration{
		Description: "rename old to new",
		Upgrade: func(doc *edit.Document) error {
			if !doc.Delete([]string{"old"}) {
				return errors.New("old key is missing")
			}
			return doc.Set([]string{"new"}, "value")
		},
	})

	out, err := r.RunDev([]byte("# schema version\nversion = 1\n\n# the old spelling\nold = \"value\"\n"))
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, `new = 'value'`, "RunDev output")
	assert.NotContains(t, got, "old = ")
	assert.Contains(t, got, "# schema version\nversion = 1")
	// A comment annotates one expression, so deleting the key deletes it too.
	assert.NotContains(t, got, "# the old spelling")
}

func TestTOMLRegistry_RunDev_TransformError(t *testing.T) {
	sentinel := errors.New("boom")
	r := NewTOML(1)
	r.RegisterDev(TOMLMigration{
		Description: "fails",
		Upgrade:     func(*edit.Document) error { return sentinel },
	})

	out, err := r.RunDev(nil)
	require.Error(t, err, "RunDev returned nil error for a failing transform")
	assert.ErrorIs(t, err, sentinel, "RunDev error")
	assert.Contains(t, err.Error(), "fails", "RunDev error")
	assert.Nil(t, out)
}

func TestTOMLRegistry_RunDev_InvalidTOML(t *testing.T) {
	r := NewTOML(1)
	_, err := r.RunDev([]byte("this is not = = toml"))
	assert.Error(t, err, "RunDev on invalid TOML returned nil error")
}

// ///////////////////////////////////////////////
// Helpers
// ///////////////////////////////////////////////

func identityTOMLUpgrade(*edit.Document) error { return nil }
