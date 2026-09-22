package migrate

// Package-level registry instances live here. Each schema target gets
// its own variable so callers reference them by name (e.g., migrate.Config).
// Common shapes:
//
//	var Config = NewTOML(1)
//	var State  = NewJSON(1)
//	var Store  = NewSQL(1)
//
// Chain With* setters for optional configuration:
//
//	var Config = NewTOML(1).WithVersionKey("v")

// TODO(kickstart): delete Example and declare the registries your project
// actually needs (Config, State, Store, etc.).
var Example = NewBytes(1)
