// Package migrate applies sequential schema migrations to on-disk data.
// Four specialized registry types cover the common file formats. Each is
// independent and can be used on its own.
//
//   - [BytesRegistry]: format-agnostic. The caller handles reading and
//     writing the file plus extracting the version field.
//
//   - [SQLRegistry]: for SQLite databases. Version tracking uses
//     PRAGMA user_version automatically; migrations run inside transactions.
//
//   - [TOMLRegistry]: for TOML documents with a top-level version key.
//     Migrations edit the document in place and rewrite only the bytes they
//     touch, so an operator's comments, blank lines and key order survive.
//
//   - [JSONRegistry]: for JSON documents with a top-level version key.
//     Migrations operate on a parsed map[string]any. JSON carries no
//     comments, so re-serializing costs only key order and whitespace.
//
// All four registries share the same shape (CurrentVersion, Migrations,
// Dev, Logger) through an embedded generic base, and are constructed via
// factory functions (NewBytes, NewSQL, NewTOML, NewJSON) with fluent
// With* setters for optional fields.
//
// Each schema target gets its own registry instance so version numbers and
// migration lists are fully independent across targets.
//
// # Dev transforms
//
// Every registry carries an optional Dev slice for development-only
// transforms that run without advancing the schema version. Use RunDev to
// apply them; typical flow is to gate RunDev behind a dev-mode flag and
// clear the entries out before committing real [Migration]s.
//
// # Extensibility
//
// To add a format the built-ins do not cover (YAML, protobuf, etc.),
// define a new migration type with vsn/desc methods and a registry type
// that embeds [baseRegistry]. The shared Register, RegisterDev, HasDev
// methods come with the embed; implement format-specific Run, RunDev, and
// NeedsMigration on the new type.
package migrate

import (
	"cmp"
	"fmt"
	"log/slog"
	"slices"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// versioned is the minimal interface every migration type satisfies so the
// generic base can read its metadata. Unexported: users never see it.
type versioned interface {
	vsn() int
	desc() string
}

// baseRegistry holds the fields and methods shared by every format-specific
// registry. Each concrete registry embeds baseRegistry[M] with its own
// migration type M.
type baseRegistry[M versioned] struct {
	// CurrentVersion is the latest schema version this registry targets.
	CurrentVersion int
	// Migrations is the ordered list of versioned upgrades.
	Migrations []M
	// Dev holds development-only transforms that run without advancing the
	// schema version. Use [baseRegistry.RegisterDev] to populate.
	Dev []M
	// Logger for migration progress. Uses slog.Default() if nil.
	Logger *slog.Logger
}

// BytesMigration upgrades serialized data from one version to the next.
// Format-agnostic: the caller handles parsing and serializing.
type BytesMigration struct {
	// Version is the schema version this migration produces.
	Version int
	// Description is a short human-readable label for log output.
	Description string
	// Upgrade transforms data from the prior version to [BytesMigration.Version].
	Upgrade func(data []byte) ([]byte, error)
}

// BytesRegistry holds the version and migrations for a single byte-based
// schema target. The caller is responsible for reading/writing the file and
// for extracting the current version from the serialized data.
type BytesRegistry struct {
	baseRegistry[BytesMigration]
}

// ///////////////////////////////////////////////
// baseRegistry methods (shared across all registries)
// ///////////////////////////////////////////////

// Register appends a migration and maintains sorted order by version.
// Panics on duplicate version.
func (b *baseRegistry[M]) Register(m M) {
	for _, existing := range b.Migrations {
		if existing.vsn() == m.vsn() {
			panic(fmt.Sprintf("migrate: duplicate migration version %d (description: %q)", m.vsn(), m.desc()))
		}
	}
	b.Migrations = append(b.Migrations, m)
	slices.SortFunc(b.Migrations, func(a, c M) int {
		return cmp.Compare(a.vsn(), c.vsn())
	})
}

// RegisterDev appends a dev-only transform. Panics on duplicate description.
func (b *baseRegistry[M]) RegisterDev(m M) {
	for _, existing := range b.Dev {
		if existing.desc() == m.desc() {
			panic(fmt.Sprintf("migrate: duplicate dev transform %q", m.desc()))
		}
	}
	b.Dev = append(b.Dev, m)
}

// HasDev reports whether any dev transforms are registered.
func (b *baseRegistry[M]) HasDev() bool {
	return len(b.Dev) > 0
}

// checkVersion reports whether fileVersion is behind CurrentVersion or any
// registered migration. force=true reports true whenever migrations exist.
// Concrete registries call this from their format-specific NeedsMigration.
func (b *baseRegistry[M]) checkVersion(fileVersion int, force bool) bool {
	if fileVersion < b.CurrentVersion {
		return true
	}
	if force && len(b.Migrations) > 0 {
		return true
	}
	for _, m := range b.Migrations {
		if fileVersion < m.vsn() {
			return true
		}
	}
	return false
}

// ///////////////////////////////////////////////
// BytesMigration methods
// ///////////////////////////////////////////////

func (m BytesMigration) vsn() int     { return m.Version }
func (m BytesMigration) desc() string { return m.Description }

// ///////////////////////////////////////////////
// BytesRegistry constructors
// ///////////////////////////////////////////////

// NewBytes constructs a [BytesRegistry] targeting schema version currentVersion.
// Chain With* setters for optional fields.
func NewBytes(currentVersion int) *BytesRegistry {
	return &BytesRegistry{
		CurrentVersion: currentVersion,
	}
}

// WithLogger sets the logger used for migration progress messages.
func (r *BytesRegistry) WithLogger(l *slog.Logger) *BytesRegistry {
	r.Logger = l
	return r
}

// ///////////////////////////////////////////////
// BytesRegistry methods
// ///////////////////////////////////////////////

// NeedsMigration reports whether a file at fileVersion needs upgrading.
// Pass force = true to report true whenever any migration is registered,
// regardless of version (used by CLI flags like --force).
func (r *BytesRegistry) NeedsMigration(fileVersion int, force bool) bool {
	return r.checkVersion(fileVersion, force)
}

// Run applies registered migrations sequentially where fromVersion < m.Version.
// Returns the transformed data and the final version reached.
func (r *BytesRegistry) Run(data []byte, fromVersion int) ([]byte, int, error) {
	version := fromVersion
	for _, m := range r.Migrations {
		if version < m.Version {
			logMigration(r.Logger, m.Version, m.Description)
			var err error
			data, err = m.Upgrade(data)
			if err != nil {
				return nil, version, fmt.Errorf("migration to v%d failed: %w", m.Version, err)
			}
			version = m.Version
		}
	}
	// The stamped version is what the data now conforms to, which is
	// CurrentVersion once every migration at or below it has run. Stamping
	// the highest applied migration instead leaves a gap whenever no
	// migration carries CurrentVersion's number, and the initial schema is
	// exactly that case: NeedsMigration compares against CurrentVersion, so
	// it would report work on every load forever. Data already ahead keeps
	// its own version.
	version = max(version, r.CurrentVersion)
	return data, version, nil
}

// RunDev applies dev transforms in the order they were registered. No
// version is advanced; callers use this for local data fixes during
// development.
func (r *BytesRegistry) RunDev(data []byte) ([]byte, error) {
	for _, m := range r.Dev {
		var err error
		data, err = m.Upgrade(data)
		if err != nil {
			return nil, fmt.Errorf("dev transform %q: %w", m.Description, err)
		}
	}
	return data, nil
}

// ///////////////////////////////////////////////
// Shared helpers
// ///////////////////////////////////////////////

// logMigration logs a migration step. Shared across all registry types so
// the log format stays consistent. If logger is nil, slog.Default() is used.
func logMigration(logger *slog.Logger, version int, description string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("applying migration", slog.Int("version", version), slog.String("description", description))
}
