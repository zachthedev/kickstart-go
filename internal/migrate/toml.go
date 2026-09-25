package migrate

import (
	"fmt"
	"log/slog"

	"github.com/pelletier/go-toml/v2/unstable/edit"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// TOMLMigration represents a schema migration for a TOML document. Upgrade
// mutates the document in place; each edit rewrites only the bytes that
// express it, so comments, blank lines and key ordering elsewhere in the
// file survive the migration.
//
// # Key paths
//
// Values are addressed by key path, one element per key part:
// []string{"server", "host"} addresses host in [server]. A path element
// that steps into an array is a 0-based index, so
// []string{"targets", "1", "name"} addresses the second [[targets]] entry.
// An index equal to the array's length appends a new element.
//
// # Reading the document
//
// Get and Has answer questions about one path. A migration that must
// discover what is there, to rename a key in every section or to walk an
// array of unknown length, reads the shape through Unmarshal into a map and
// then writes through Set and Delete:
//
//	var shape map[string]any
//	if err := doc.Unmarshal(&shape); err != nil {
//	    return err
//	}
//	for section := range shape {
//	    if v, ok := doc.Get([]string{section, "count"}); ok {
//	        if err := doc.Set([]string{section, "quantity"}, v); err != nil {
//	            return err
//	        }
//	        doc.Delete([]string{section, "count"})
//	    }
//	}
//
// # Replacing a table
//
// Set refuses a path naming an existing table rather than silently
// discarding its contents, so delete it first. Setting a map value renders
// an inline table (`alpha = {x = 1}`); setting the leaf keys one at a time
// produces an `[alpha]` section.
//
// A comment belongs to the expression above or beside it, so deleting a key
// deletes its comment with it.
type TOMLMigration struct {
	// Version is the schema version this migration produces.
	Version int
	// Description is a short human-readable label for log output.
	Description string
	// Upgrade transforms a TOML document in place.
	Upgrade func(doc *edit.Document) error
}

// TOMLRegistry holds the migrations for a single TOML schema target.
// The version is read from and written to a top-level key in the document
// (default "version"); chain [TOMLRegistry.WithVersionKey] to override.
type TOMLRegistry struct {
	baseRegistry[TOMLMigration]
	// VersionKey is the top-level key storing the schema version.
	// Defaults to "version" when empty.
	VersionKey string
}

// ///////////////////////////////////////////////
// TOMLMigration methods
// ///////////////////////////////////////////////

func (m TOMLMigration) vsn() int     { return m.Version }
func (m TOMLMigration) desc() string { return m.Description }

// ///////////////////////////////////////////////
// TOMLRegistry constructors
// ///////////////////////////////////////////////

// NewTOML constructs a [TOMLRegistry] targeting schema version currentVersion.
// Chain With* setters for optional fields.
func NewTOML(currentVersion int) *TOMLRegistry {
	return &TOMLRegistry{
		CurrentVersion: currentVersion,
	}
}

// WithVersionKey overrides the default "version" key used to read/write the
// schema version in the TOML document.
func (r *TOMLRegistry) WithVersionKey(key string) *TOMLRegistry {
	r.VersionKey = key
	return r
}

// WithLogger sets the logger used for migration progress messages.
func (r *TOMLRegistry) WithLogger(l *slog.Logger) *TOMLRegistry {
	r.Logger = l
	return r
}

// ///////////////////////////////////////////////
// TOMLRegistry methods
// ///////////////////////////////////////////////

// NeedsMigration reports whether the TOML document at data needs upgrading.
// An empty input is treated as version 0.
func (r *TOMLRegistry) NeedsMigration(data []byte) (bool, error) {
	version, _, err := r.parse(data)
	if err != nil {
		return false, err
	}
	return r.checkVersion(version, false), nil
}

// Run parses the TOML document, applies pending migrations, writes the new
// version into the document, and returns the edited bytes plus the final
// version reached. Bytes no migration touched are returned verbatim.
func (r *TOMLRegistry) Run(data []byte) ([]byte, int, error) {
	version, doc, err := r.parse(data)
	if err != nil {
		return nil, version, err
	}
	for _, m := range r.Migrations {
		if version >= m.Version {
			continue
		}
		logMigration(r.Logger, m.Version, m.Description)
		if err := m.Upgrade(doc); err != nil {
			return nil, version, fmt.Errorf("migration to v%d failed: %w", m.Version, err)
		}
		version = m.Version
	}
	// The stamped version is what the data now conforms to, which is
	// CurrentVersion once every migration at or below it has run. Stamping
	// the highest applied migration instead leaves a gap whenever no
	// migration carries CurrentVersion's number, and the initial schema is
	// exactly that case: NeedsMigration compares against CurrentVersion, so
	// it would report work on every load forever. Data already ahead keeps
	// its own version.
	version = max(version, r.CurrentVersion)
	if err := doc.Set([]string{r.versionKey()}, int64(version)); err != nil {
		return nil, version, fmt.Errorf("writing %q: %w", r.versionKey(), err)
	}
	return doc.Bytes(), version, nil
}

// RunDev parses the document, applies each dev transform, and returns the
// edited bytes. No version is advanced.
func (r *TOMLRegistry) RunDev(data []byte) ([]byte, error) {
	_, doc, err := r.parse(data)
	if err != nil {
		return nil, err
	}
	for _, m := range r.Dev {
		if err := m.Upgrade(doc); err != nil {
			return nil, fmt.Errorf("dev transform %q: %w", m.Description, err)
		}
	}
	return doc.Bytes(), nil
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// versionKey returns the configured version key, defaulting to "version".
func (r *TOMLRegistry) versionKey() string {
	if r.VersionKey == "" {
		return "version"
	}
	return r.VersionKey
}

// parse opens the TOML document and extracts the version. An empty input
// yields version 0 and an empty document, matching the semantics of a fresh
// (yet-to-be-written) file. Every TOML integer reads back as int64, which
// is the type the version assertion below expects.
func (r *TOMLRegistry) parse(data []byte) (int, *edit.Document, error) {
	doc, err := edit.Parse(data)
	if err != nil {
		return 0, nil, fmt.Errorf("parsing TOML: %w", err)
	}
	raw, ok := doc.Get([]string{r.versionKey()})
	if !ok {
		return 0, doc, nil
	}
	n, ok := raw.(int64)
	if !ok {
		return 0, nil, fmt.Errorf("reading %q: expected integer, got %T", r.versionKey(), raw)
	}
	return int(n), doc, nil
}
