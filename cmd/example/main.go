// TODO(kickstart): rename this directory to cmd/<binary>/ and rewrite for
// your project. Delete internal/example/ when you do; it exists only so
// the template compiles out of the box.
//
// This binary doubles as a smoke demo of every internal package, so the
// template ships with no dead code. The exercise calls are not load-bearing
// for any real product; rip them out when you swap in your own logic.
//
// Deleting this package makes `task deadcode` report every exported symbol
// in internal/ at once. The README's "Dead code" section says how to
// absorb that.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/pelletier/go-toml/v2/unstable/edit"
	_ "modernc.org/sqlite"

	"zach.tools/go/kickstart/internal/buildenv"
	"zach.tools/go/kickstart/internal/example"
	"zach.tools/go/kickstart/internal/generate"
	"zach.tools/go/kickstart/internal/logger"
	"zach.tools/go/kickstart/internal/migrate"
	"zach.tools/go/kickstart/internal/paths"
	"zach.tools/go/kickstart/internal/remote"
	"zach.tools/go/kickstart/internal/version"
)

// ///////////////////////////////////////////////
// Command tree
// ///////////////////////////////////////////////

// CLI is the root command tree. kong derives the parser, the help text and
// the flag defaults from these struct tags.
//
// TODO(kickstart): replace demoCmd with your project's own commands.
type CLI struct {
	Version kong.VersionFlag `short:"V" help:"Print the build version and exit."`
	Demo    demoCmd          `cmd:"" default:"withargs" help:"Exercise one code path per internal package."`
}

// demoCmd prints the template's smoke report.
type demoCmd struct {
	ConfigDir string `help:"Config directory the reported paths sit under." placeholder:"DIR"`
}

// ///////////////////////////////////////////////
// Entry point
// ///////////////////////////////////////////////

func main() {
	if err := execute(os.Args[1:], os.Stdout, os.Stderr, os.Exit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// execute parses args and runs the selected command. Every handle the
// parser can reach is a parameter: kong writes help and version output to
// the supplied streams and terminates through the supplied exit function,
// so a test drives the real parser without touching the process.
func execute(args []string, stdout, stderr io.Writer, exit func(int)) error {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name(paths.BinaryName),
		kong.Description("Smoke demo for the kickstart template's internal packages."),
		kong.Writers(stdout, stderr),
		kong.Exit(exit),
		kong.BindTo(stdout, (*io.Writer)(nil)),
		kong.Vars{"version": version.Info()},
	)
	if err != nil {
		return fmt.Errorf("building the parser: %w", err)
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		return fmt.Errorf("parsing arguments: %w", err)
	}
	return ctx.Run()
}

// ///////////////////////////////////////////////
// Commands
// ///////////////////////////////////////////////

// Run writes the demo report. kong calls it for the selected command and
// supplies stdout through kong.Bind.
func (c demoCmd) Run(stdout io.Writer) error {
	out, err := run(c.ConfigDir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, out)
	return err
}

// run assembles a multi-line report that exercises one primary code path
// per internal package. An empty baseDir resolves through paths.ConfigDir.
// Errors short-circuit so the template never ships with a half-broken demo.
func run(baseDir string) (string, error) {
	var b strings.Builder

	if baseDir == "" {
		resolved, err := paths.ConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolving the config directory: %w", err)
		}
		baseDir = resolved
	}

	writeGreeting(&b)
	if err := writePaths(&b, baseDir); err != nil {
		return "", fmt.Errorf("paths demo: %w", err)
	}
	writeVersion(&b)
	writeRemote(&b)
	if err := writeGenerate(&b); err != nil {
		return "", fmt.Errorf("generate demo: %w", err)
	}
	if err := writeMigrate(&b); err != nil {
		return "", fmt.Errorf("migrate demo: %w", err)
	}
	if err := writeLogger(&b); err != nil {
		return "", fmt.Errorf("logger demo: %w", err)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// writeGreeting demonstrates the trivial internal/example package.
func writeGreeting(b *strings.Builder) {
	fmt.Fprintln(b, example.Greet("world"))
}

// writePaths exercises every public path resolver and accessor so none
// of them go unused in production builds.
func writePaths(b *strings.Builder, baseDir string) error {
	envBase, err := paths.EnvOr("KICKSTART_CONFIG", paths.Fixed(baseDir))()
	if err != nil {
		return fmt.Errorf("resolving the override directory: %w", err)
	}
	cacheDir, err := paths.CacheDir()
	if err != nil {
		return fmt.Errorf("resolving the cache directory: %w", err)
	}
	stateDir, err := paths.StateDir()
	if err != nil {
		return fmt.Errorf("resolving the state directory: %w", err)
	}
	home, err := paths.HomeRelative("." + paths.BinaryName)()
	if err != nil {
		return fmt.Errorf("resolving the dotfile directory: %w", err)
	}
	stateRoot, err := paths.UserStateDir()
	if err != nil {
		return fmt.Errorf("resolving the state root: %w", err)
	}
	logPath, err := paths.LogApp.Path(stateDir)
	if err != nil {
		return fmt.Errorf("resolving the log path: %w", err)
	}
	winLogPath, err := paths.LogApp.PathFor(paths.Windows, envBase)
	if err != nil {
		return fmt.Errorf("resolving the windows log path: %w", err)
	}

	fmt.Fprintf(b, "config: %s\n", paths.ConfigPath(baseDir))
	fmt.Fprintf(b, "config (posix): %s\n", paths.ConfigPathFor(paths.Posix, envBase))
	fmt.Fprintf(b, "cache:  %s\n", cacheDir)
	fmt.Fprintf(b, "state:  %s (root %s)\n", stateDir, stateRoot)
	fmt.Fprintf(b, "log:    %s\n", logPath)
	fmt.Fprintf(b, "log (windows): %s\n", winLogPath)
	fmt.Fprintf(b, "dotfile dir (HomeRelative): %s\n", home)
	return nil
}

// writeVersion exercises the linker-injected build version helpers and
// names the build settings that feed them.
func writeVersion(b *strings.Builder) {
	fmt.Fprintf(b, "version: %s\n", version.Info())
	fmt.Fprintf(b, "docker tag: %s\n", version.DockerTag())
	fmt.Fprintf(b, "build settings: %s\n", strings.Join(buildenv.Names(), ", "))
}

// writeRemote exercises the GitHub owner/repo metadata helpers. Values
// fall back to empty strings when no git remote is configured, which is
// fine for a smoke demo.
func writeRemote(b *strings.Builder) {
	fmt.Fprintf(b, "remote: %s/%s\n", remote.Owner(), remote.Repo())
	fmt.Fprintf(b, "raw README: %s\n", remote.RawURL("README.md"))
}

// writeGenerate registers a no-op OutputEntry so the registry's Register
// path is reachable in production, then runs one generator through
// StripLeadingBanner. The entries are never written to disk because
// cmd/example does not call Registry.Run.
func writeGenerate(b *strings.Builder) error {
	type sample struct {
		Name string `json:"name"`
	}

	tomlGen := generate.TOMLConfig{
		ProjectName: "kickstart",
		Defaults:    sample{Name: "demo"},
		Docs: map[string]generate.FieldDoc{
			"name": {Comment: "Project display name."},
		},
	}.Generate
	jsonGen := generate.JSONConfig{ProjectName: "kickstart", Defaults: sample{Name: "demo"}}.Generate
	schemaGen := generate.JSONSchema{Target: &sample{}, Title: "Sample"}.Generate

	// A local registry keeps the demo repeatable. generate.Default is a
	// process-wide singleton and Register panics on a duplicate path, so
	// registering into it here would fail the second call.
	var reg generate.Registry
	reg.Register(generate.OutputEntry{
		Path:     "demo.toml",
		Inputs:   []string{"cmd/example/*.go"},
		Generate: tomlGen,
	})
	reg.Register(generate.OutputEntry{
		Path:     "demo.json",
		Generate: jsonGen,
	})
	reg.Register(generate.OutputEntry{
		Path:     "demo.schema.json",
		Generate: schemaGen,
	})

	// A Template output carries an operator-facing banner in the repo copy.
	// StripLeadingBanner produces the bytes that belong on their disk.
	withBanner, err := tomlGen(generate.OutputEntry{Path: "demo.toml", Template: true})
	if err != nil {
		return fmt.Errorf("generating the template output: %w", err)
	}
	stripped := generate.StripLeadingBanner(withBanner)

	fmt.Fprintf(b, "generators: %d registered\n", len(reg.Outputs()))
	fmt.Fprintf(b, "banner strip: %d bytes -> %d bytes\n", len(withBanner), len(stripped))
	return nil
}

// writeMigrate constructs one of every registry shape, registers a no-op
// migration on each, and either runs it (Bytes/TOML/JSON) or queries it
// (SQL) so every public API is exercised.
func writeMigrate(b *strings.Builder) error {
	migLog := slog.Default()

	// ///// Bytes /////
	bytesReg := migrate.NewBytes(1).WithLogger(migLog)
	bytesReg.Register(migrate.BytesMigration{
		Version:     1,
		Description: "noop bytes",
		Upgrade:     func(data []byte) ([]byte, error) { return data, nil },
	})
	bytesReg.RegisterDev(migrate.BytesMigration{
		Description: "noop bytes dev",
		Upgrade:     func(data []byte) ([]byte, error) { return data, nil },
	})
	bytesNeeds := bytesReg.NeedsMigration(0, false)
	if _, err := bytesReg.RunDev(nil); err != nil {
		return fmt.Errorf("bytes RunDev: %w", err)
	}
	if _, _, err := bytesReg.Run(nil, 0); err != nil {
		return fmt.Errorf("bytes Run: %w", err)
	}

	// ///// TOML /////
	tomlReg := migrate.NewTOML(1).WithLogger(migLog).WithVersionKey("schema_version")
	tomlReg.Register(migrate.TOMLMigration{
		Version:     1,
		Description: "noop toml",
		Upgrade:     func(*edit.Document) error { return nil },
	})
	tomlReg.RegisterDev(migrate.TOMLMigration{
		Description: "noop toml dev",
		Upgrade:     func(*edit.Document) error { return nil },
	})
	tomlNeeds, err := tomlReg.NeedsMigration(nil)
	if err != nil {
		return fmt.Errorf("toml NeedsMigration: %w", err)
	}
	if _, err := tomlReg.RunDev(nil); err != nil {
		return fmt.Errorf("toml RunDev: %w", err)
	}
	if _, _, err := tomlReg.Run(nil); err != nil {
		return fmt.Errorf("toml Run: %w", err)
	}

	// ///// JSON /////
	jsonReg := migrate.NewJSON(1).WithLogger(migLog).WithVersionKey("schema_version")
	jsonReg.Register(migrate.JSONMigration{
		Version:     1,
		Description: "noop json",
		Upgrade:     func(doc map[string]any) (map[string]any, error) { return doc, nil },
	})
	jsonReg.RegisterDev(migrate.JSONMigration{
		Description: "noop json dev",
		Upgrade:     func(doc map[string]any) (map[string]any, error) { return doc, nil },
	})
	jsonNeeds, err := jsonReg.NeedsMigration(nil)
	if err != nil {
		return fmt.Errorf("json NeedsMigration: %w", err)
	}
	if _, err := jsonReg.RunDev(nil); err != nil {
		return fmt.Errorf("json RunDev: %w", err)
	}
	if _, _, err := jsonReg.Run(nil); err != nil {
		return fmt.Errorf("json Run: %w", err)
	}

	// ///// SQL /////
	sqlReg := migrate.NewSQL(1).
		WithLogger(migLog).
		WithInit(func(*sql.Tx) error { return nil })
	sqlReg.Register(migrate.SQLMigration{
		Version:     2,
		Description: "noop sql",
		Upgrade:     func(*sql.Tx) error { return nil },
	})
	sqlReg.RegisterDev(migrate.SQLMigration{
		Description: "noop sql dev",
		Upgrade:     func(*sql.Tx) error { return nil },
	})

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("opening sqlite: %w", err)
	}
	defer db.Close()
	sqlNeeds, err := sqlReg.NeedsMigration(db)
	if err != nil {
		return fmt.Errorf("sql NeedsMigration: %w", err)
	}
	if err := sqlReg.Run(db); err != nil {
		return fmt.Errorf("sql Run: %w", err)
	}
	if err := sqlReg.RunDev(db); err != nil {
		return fmt.Errorf("sql RunDev: %w", err)
	}

	// Reference the package-level registry from internal/migrate so the
	// example's import is not purely transitive.
	_ = migrate.Example.HasDev()

	fmt.Fprintf(b, "migrate (bytes/toml/json/sql needed): %t/%t/%t/%t\n",
		bytesNeeds, tomlNeeds, jsonNeeds, sqlNeeds)
	return nil
}

// writeLogger exercises internal/logger so the template ships with no dead
// code in the logging layer. It runs against a temp dir, so the demo never
// pollutes the workspace, and forces one size rotation so the rotating
// writer is demonstrated rather than only configured.
func writeLogger(b *strings.Builder) error {
	tmpDir, err := os.MkdirTemp("", "kickstart-logger-")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "demo.log")
	log, closer, err := logger.New(logger.Options{
		Path:        logPath,
		Level:       slog.LevelDebug,
		Console:     io.Discard,
		MaxSizeMB:   1,
		MaxBackups:  2,
		MaxAgeDays:  28,
		Compression: "zstd",
	})
	if err != nil {
		return fmt.Errorf("new logger: %w", err)
	}
	defer closer.Close()

	log.Debug("debug via New")
	log.Info("info via New", "k", "v")
	log.With("svc", "demo").Warn("warn with attribute")
	log.WithGroup("req").Error("error within a group", "method", "GET")

	// Two writes of 600 KiB cross the 1 MB limit, so the second rotates.
	payload := strings.Repeat("x", 600*1024)
	log.Info("first", "payload", payload)
	log.Info("second", "payload", payload)

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("reading the log directory: %w", err)
	}

	fmt.Fprintf(b, "logger: json to %s, %d file(s) after rotation\n",
		filepath.Base(logPath), len(entries))
	return nil
}
