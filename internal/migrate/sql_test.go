package migrate

import (
	"database/sql"
	"fmt"
	"log/slog"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// ///////////////////////////////////////////////
// NewSQL + WithInit
// ///////////////////////////////////////////////

func TestNewSQL_SetsCurrentVersion(t *testing.T) {
	r := NewSQL(3)
	assert.Equal(t, 3, r.CurrentVersion, "CurrentVersion")
}

func TestSQLRegistry_WithInit(t *testing.T) {
	fn := func(*sql.Tx) error { return nil }
	r := NewSQL(1).WithInit(fn)
	assert.NotNil(t, r.Init, "WithInit left Init nil")
}

// ///////////////////////////////////////////////
// SQLRegistry.Register
// ///////////////////////////////////////////////

func TestSQLRegistry_Register_SortsByVersion(t *testing.T) {
	r := NewSQL(3)
	r.Register(SQLMigration{Version: 3, Description: "third"})
	r.Register(SQLMigration{Version: 2, Description: "second"})

	got := make([]int, len(r.Migrations))
	for i, m := range r.Migrations {
		got[i] = m.Version
	}
	assert.Equal(t, []int{2, 3}, got, "Register did not sort by version")
}

func TestSQLRegistry_Register_DuplicateVersionPanics(t *testing.T) {
	r := NewSQL(2)
	r.Register(SQLMigration{Version: 2, Description: "first"})
	assertPanics(t, func() {
		r.Register(SQLMigration{Version: 2, Description: "duplicate"})
	})
}

func TestSQLRegistry_Register_Version1Panics(t *testing.T) {
	r := NewSQL(1)
	assertPanics(t, func() {
		r.Register(SQLMigration{Version: 1, Description: "use Init"})
	})
}

// ///////////////////////////////////////////////
// SQLRegistry.NeedsMigration
// ///////////////////////////////////////////////

func TestSQLRegistry_NeedsMigration_FreshDB(t *testing.T) {
	r := NewSQL(2)
	needs, err := r.NeedsMigration(openTestDB(t))
	require.NoError(t, err)
	assert.True(t, needs, "NeedsMigration = false on fresh DB")
}

func TestSQLRegistry_NeedsMigration_UpToDate(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("PRAGMA user_version = 2")
	require.NoError(t, err)
	r := NewSQL(2)
	needs, err := r.NeedsMigration(db)
	require.NoError(t, err)
	assert.False(t, needs, "NeedsMigration = true when at current version")
}

func TestSQLRegistry_NeedsMigration_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.Close()

	r := NewSQL(1)
	_, err = r.NeedsMigration(db)
	assert.Error(t, err, "NeedsMigration error = nil")
}

// ///////////////////////////////////////////////
// SQLRegistry.Run
// ///////////////////////////////////////////////

func TestSQLRegistry_Run_FreshDBRunsInit(t *testing.T) {
	db := openTestDB(t)
	r := NewSQL(1).WithInit(func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY)")
		return err
	})

	require.NoError(t, r.Run(db))

	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, 1, version, "user_version")
	_, err := db.Exec("INSERT INTO items (id) VALUES (1)")
	assert.NoError(t, err)
}

func TestSQLRegistry_Run_AppliesMigrations(t *testing.T) {
	db := openTestDB(t)
	r := NewSQL(2).WithInit(func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)")
		return err
	})
	r.Register(SQLMigration{
		Version:     2,
		Description: "add column",
		Upgrade: func(tx *sql.Tx) error {
			_, err := tx.Exec("ALTER TABLE items ADD COLUMN value TEXT DEFAULT ''")
			return err
		},
	})

	require.NoError(t, r.Run(db))
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, 2, version, "user_version")
	_, err := db.Exec("INSERT INTO items (id, name, value) VALUES (1, 'test', 'val')")
	assert.NoError(t, err)
}

func TestSQLRegistry_Run_SkipsAppliedMigrations(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	initCalled := false
	r := NewSQL(1).WithInit(func(tx *sql.Tx) error {
		initCalled = true
		return nil
	})
	require.NoError(t, r.Run(db))
	assert.False(t, initCalled, "Init ran on an existing DB, should be skipped")
}

func TestSQLRegistry_Run_RollsBackOnFailure(t *testing.T) {
	db := openTestDB(t)
	r := NewSQL(2).WithInit(func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY)")
		return err
	})
	r.Register(SQLMigration{
		Version:     2,
		Description: "fails",
		Upgrade: func(tx *sql.Tx) error {
			return fmt.Errorf("deliberate failure")
		},
	})

	err := r.Run(db)
	require.Error(t, err, "Run error = nil")
	assert.Contains(t, err.Error(), "deliberate failure", "Run error")
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, 1, version, "user_version")
}

func TestSQLRegistry_Run_NilInitErrors(t *testing.T) {
	db := openTestDB(t)
	r := NewSQL(1)
	assert.Error(t, r.Run(db), "Run error = nil")
}

func TestSQLRegistry_Run_InitErrorLeavesVersionZero(t *testing.T) {
	db := openTestDB(t)
	r := NewSQL(1).WithInit(func(tx *sql.Tx) error {
		return fmt.Errorf("init failed")
	})
	require.Error(t, r.Run(db), "Run returned nil for a failing Init")
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, 0, version, "user_version")
}

func TestSQLRegistry_Run_ClosedDBErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.Close()

	r := NewSQL(1).WithInit(func(*sql.Tx) error { return nil })
	assert.Error(t, r.Run(db), "Run error = nil")
}

// ///////////////////////////////////////////////
// SQLRegistry.RunDev + Dev
// ///////////////////////////////////////////////

func TestSQLRegistry_RunDev_AppliesInRegistrationOrder(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("CREATE TABLE counter (n INTEGER DEFAULT 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO counter (n) VALUES (0)")
	require.NoError(t, err)

	r := NewSQL(1)
	r.RegisterDev(SQLMigration{
		Description: "add 1",
		Upgrade: func(tx *sql.Tx) error {
			_, err := tx.Exec("UPDATE counter SET n = n + 1")
			return err
		},
	})
	r.RegisterDev(SQLMigration{
		Description: "multiply 10",
		Upgrade: func(tx *sql.Tx) error {
			_, err := tx.Exec("UPDATE counter SET n = n * 10")
			return err
		},
	})

	require.NoError(t, r.RunDev(db))
	var n int
	require.NoError(t, db.QueryRow("SELECT n FROM counter").Scan(&n))
	assert.Equal(t, 10, n, "counter")

	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, 0, version, "user_version")
}

func TestSQLRegistry_RunDev_RollsBackOnError(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("CREATE TABLE counter (n INTEGER DEFAULT 0)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO counter (n) VALUES (5)")
	require.NoError(t, err)

	r := NewSQL(1)
	r.RegisterDev(SQLMigration{
		Description: "boom",
		Upgrade: func(tx *sql.Tx) error {
			if _, err := tx.Exec("UPDATE counter SET n = 99"); err != nil {
				return err
			}
			return fmt.Errorf("deliberate failure")
		},
	})

	require.Error(t, r.RunDev(db), "RunDev returned nil for a failing transform")
	var n int
	require.NoError(t, db.QueryRow("SELECT n FROM counter").Scan(&n))
	assert.Equal(t, 5, n, "counter")
}

// ///////////////////////////////////////////////
// Run convergence
// ///////////////////////////////////////////////

// Run must leave a database that NeedsMigration reports as done. The version
// lives in PRAGMA user_version, and Init sets it to 1, so a CurrentVersion
// above the highest migration otherwise reports work on every startup.
func TestSQLRegistry_Run_Converges(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion int
		migrations     []int
		wantVersion    int
	}{
		{name: "initial schema carries no migration", currentVersion: 1, wantVersion: 1},
		{name: "one migration covers it", currentVersion: 2, migrations: []int{2}, wantVersion: 2},
		{name: "CurrentVersion above the highest migration", currentVersion: 5, migrations: []int{2, 3}, wantVersion: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			r := NewSQL(tt.currentVersion).
				WithLogger(slog.New(slog.DiscardHandler)).
				WithInit(func(*sql.Tx) error { return nil })
			for _, v := range tt.migrations {
				r.Register(SQLMigration{Version: v, Upgrade: func(*sql.Tx) error { return nil }})
			}

			require.NoError(t, r.Run(db))

			var got int
			require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&got))
			assert.Equal(t, tt.wantVersion, got, "user_version after Run")

			needs, err := r.NeedsMigration(db)
			require.NoError(t, err)
			assert.False(t, needs, "Run left work outstanding, so every startup repeats it")
		})
	}
}
