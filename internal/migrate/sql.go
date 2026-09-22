package migrate

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// SQLMigration represents a schema migration that upgrades a SQLite database
// from one version to the next.
type SQLMigration struct {
	// Version is the schema version this migration produces. Must be >= 2;
	// version 1 is the initial schema produced by [SQLRegistry.Init].
	Version int
	// Description is a short human-readable label for log output.
	Description string
	// Upgrade transforms the database schema within a transaction.
	Upgrade func(tx *sql.Tx) error
}

// SQLRegistry holds the initial schema and migrations for a single SQLite
// database. Version tracking uses PRAGMA user_version: fresh databases have
// user_version = 0 and receive [SQLRegistry.Init]; existing databases apply
// any pending [SQLMigration]s sequentially.
type SQLRegistry struct {
	baseRegistry[SQLMigration]
	// Init creates the initial schema (tables, views, indexes) in a fresh
	// database. Runs inside a transaction. Required.
	Init func(tx *sql.Tx) error
}

// ///////////////////////////////////////////////
// SQLMigration methods
// ///////////////////////////////////////////////

func (m SQLMigration) vsn() int     { return m.Version }
func (m SQLMigration) desc() string { return m.Description }

// ///////////////////////////////////////////////
// SQLRegistry constructors
// ///////////////////////////////////////////////

// NewSQL constructs a [SQLRegistry] targeting schema version currentVersion.
// Chain With* setters for optional fields.
func NewSQL(currentVersion int) *SQLRegistry {
	return &SQLRegistry{
		CurrentVersion: currentVersion,
	}
}

// WithInit sets the initial-schema builder. Required before calling Run.
func (r *SQLRegistry) WithInit(fn func(tx *sql.Tx) error) *SQLRegistry {
	r.Init = fn
	return r
}

// WithLogger sets the logger used for migration progress messages.
func (r *SQLRegistry) WithLogger(l *slog.Logger) *SQLRegistry {
	r.Logger = l
	return r
}

// ///////////////////////////////////////////////
// SQLRegistry methods
// ///////////////////////////////////////////////

// Register appends a SQL migration. Panics on duplicate version or if
// version < 2 (use Init for the initial schema).
func (r *SQLRegistry) Register(m SQLMigration) {
	if m.Version < 2 {
		panic(fmt.Sprintf("migrate: SQL migration version must be >= 2 (got %d); use Init for initial schema", m.Version))
	}
	r.baseRegistry.Register(m)
}

// NeedsMigration reports whether the database needs any migrations applied.
// Returns an error if the version cannot be read (e.g., closed DB).
func (r *SQLRegistry) NeedsMigration(db *sql.DB) (bool, error) {
	var currentVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return false, fmt.Errorf("reading user_version: %w", err)
	}
	return r.checkVersion(currentVersion, false), nil
}

// Run initializes or upgrades the database schema. For a fresh database
// (user_version = 0), it runs Init and sets user_version to 1. For existing
// databases, it applies any pending migrations sequentially.
func (r *SQLRegistry) Run(db *sql.DB) error {
	if r.Init == nil {
		return fmt.Errorf("migrate: Init function is required")
	}

	var currentVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return fmt.Errorf("reading user_version: %w", err)
	}

	if currentVersion == 0 {
		if err := r.runInit(db); err != nil {
			return err
		}
		currentVersion = 1
	}

	for _, m := range r.Migrations {
		if currentVersion >= m.Version {
			continue
		}
		logMigration(r.Logger, m.Version, m.Description)
		if err := applySQLMigration(db, m); err != nil {
			return err
		}
		currentVersion = m.Version
	}
	// The stamped version is what the data now conforms to, which is
	// CurrentVersion once every migration at or below it has run. Stamping
	// the highest applied migration instead leaves a gap whenever no
	// migration carries CurrentVersion's number, and the initial schema is
	// exactly that case: NeedsMigration compares against CurrentVersion, so
	// it would report work on every load forever. Data already ahead keeps
	// its own version.
	if currentVersion < r.CurrentVersion {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", r.CurrentVersion)); err != nil {
			return fmt.Errorf("setting user_version to %d: %w", r.CurrentVersion, err)
		}
	}
	return nil
}

// RunDev applies each Dev migration inside its own transaction. No version
// is advanced; intended for iterating on schema/data during development
// before committing real [SQLMigration]s.
//
// Prefer the "CREATE new + INSERT SELECT + DROP old + RENAME" pattern over
// ALTER TABLE for non-trivial schema changes. SQLite rewrites
// sqlite_master.sql in place when you ALTER, so accumulated ALTERs can
// leave the stored DDL (visible via .schema) looking messy. The
// create-swap-drop pattern leaves sqlite_master clean.
func (r *SQLRegistry) RunDev(db *sql.DB) error {
	for _, m := range r.Dev {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for dev transform %q: %w", m.Description, err)
		}
		if err := m.Upgrade(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("dev transform %q: %w", m.Description, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit dev transform %q: %w", m.Description, err) // coverage:partial (Commit never fails on in-memory SQLite)
		}
	}
	return nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// runInit applies the initial schema in a transaction and sets
// user_version to 1 on success.
func (r *SQLRegistry) runInit(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for init: %w", err) // coverage:partial (Begin never fails on live SQLite)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	if err := r.Init(tx); err != nil {
		return fmt.Errorf("schema init failed: %w", err)
	}
	if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
		return fmt.Errorf("setting user_version to 1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit init: %w", err) // coverage:partial (Commit never fails on in-memory SQLite)
	}
	return nil
}

// applySQLMigration runs one migration inside a transaction. The deferred
// rollback scope is per-call so it runs immediately on error.
func applySQLMigration(db *sql.DB, m SQLMigration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for migration v%d: %w", m.Version, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	if err := m.Upgrade(tx); err != nil {
		return fmt.Errorf("migration to v%d failed: %w", m.Version, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
		return fmt.Errorf("setting user_version to %d: %w", m.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration v%d: %w", m.Version, err) // coverage:partial (Commit never fails on in-memory SQLite)
	}
	return nil
}
