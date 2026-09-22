# Developing

## Prerequisites

<!-- TODO(kickstart): add what your project needs beyond these, each naming the file that pins its version. -->

- [Go](https://go.dev), at the toolchain `go.mod` names. Every Go tool the gate runs (`task`, `lefthook`,
  `golangci-lint`, `govulncheck`, `go-test-coverage`, `testpair`, `deadcode`) is a `tool` directive in
  `go.mod`, and `go tool <name>` builds each from the module's pin, so nothing else is installed.
- [Bun](https://bun.sh), at the version `packageManager` in `package.json` names. It installs commitlint and
  Prettier.
- [mise](https://mise.jdx.dev). It installs the tools `mise.toml` pins at the versions `mise.lock` records.
- A C compiler on `PATH`, for the race detector. Without one the race row prints that it did not run.
- [gh](https://cli.github.com), optional. When `gh auth token` succeeds, the gate runs zizmor online;
  otherwise zizmor runs offline and no token is needed.

## First run

```sh
mise trust
bun install
go tool lefthook install
go tool task check
```

`mise trust` lets mise read this checkout's `mise.toml`. `bun install` installs commitlint and Prettier under
`node_modules/`. `go tool lefthook install` writes the git hooks. The first gate run builds the Go tools and
installs the mise tools from the lockfile.

## Running it

<!-- TODO(kickstart): say how your binary is run from a build output and from source. -->

```sh
go run ./cmd/example
go tool task build && ./dist/example
```

`go tool task build TAGS=dev` compiles the dev-only code in (below). Every build setting the binary takes
through `-ldflags -X` is declared in `internal/buildenv`; copy `.env.template` to `.env`, which is
gitignored, and uncomment what you need. `Taskfile.yml` loads that file, so a value there reaches every task.

## Generated files

<!-- TODO(kickstart): register your project's generated files in cmd/generate/main.go and list them here. -->

- `.env.template`: from `internal/buildenv`, by `go tool task generate`.
- `MARKERS.md`: from the template markers in the tree, by `go tool task generate`. It goes when the template
  is absorbed ([README.md#template-markers](../README.md#template-markers)).
- `mise.lock`: `mise lock`, after any edit to `mise.toml`.
- `bun.lock`: `bun install`.
- `go.sum`: `go mod tidy`.

The `generate` row regenerates the first two and fails when the tree differs, so a stale committed copy is
caught before it merges.

## Tests that need a real thing

<!-- TODO(kickstart): name the tests that need a real server, machine or network, and how to run them. -->

- The race row (`go tool task test`) needs a C compiler on `PATH`; `scripts/test-race.sh` says so and skips
  without one.
- `internal/tools/version`'s test runs the tool through `go run` and reads `git describe`, so it needs `go`
  and `git` on `PATH` and a git checkout.
- `internal/remote`'s and `internal/version`'s tests run `git` against a repository they create under
  `t.TempDir()`, with the user's git configuration masked, so they need `git` on `PATH` and nothing else.

## Dev-only code

A dev affordance (a synthetic data source, a mock subprocess injector, a "skip auth" toggle) is gated by a
build tag, not by a config flag. A config flag leaves the code compiled into the production binary, so an
operator who misconfigures, or anyone who can write the config, gets a real attack surface. A build tag
means the code is not in the binary at all.

`go tool task build` produces a production binary. `go tool task build TAGS=dev` and
`go tool task test TAGS=dev` add the dev-tagged files. The `TAGS` variable threads through `build`,
`testquick`, `test` and `coverage`.

Two files implement the same symbol with the same signature, and the tag picks exactly one. Callers do not
change.

```text
internal/yourpkg/
├── feature.go            //go:build dev      real implementation
├── feature_stub.go       //go:build !dev     refuses, and says what it refuses
├── feature_test.go       //go:build dev
└── feature_stub_test.go  //go:build !dev
```

The stub returns an error naming the rebuild, or no-ops where that is the useful production behavior (a
metrics injector stub returns a discard sink).

```go
//go:build !dev

package yourpkg

// StartLoopbackListener returns an error in production builds.
func StartLoopbackListener(_ string) (*net.UDPConn, error) {
	return nil, fmt.Errorf("yourpkg: loopback listener not compiled in (rebuild with `-tags dev`)")
}
```

What to check when the first one lands:

- Both files need a test pair; `testpair` enforces the pairing, and the stub's test asserts the documented
  error or no-op.
- `.golangci.yml` needs `run.build-tags: [dev]` once dev files exist, or lint never sees them.
- `.testcoverage.yml` thresholds are set for the production build. Dev files move the number under
  `-tags dev`; give them an override rather than letting the threshold drift.
- A CI lane running `go tool task test TAGS=dev` means something only once dev-tagged tests exist.
- `go list`, `go vet` and godoc honor tags. `go list -tags dev ./...` is the dev view; the default is what
  `pkg.go.dev` shows.

Once a binary has two lineages, the version output says which, and only when the lineage is not the ordinary
one: name the consequence ("refuses a library a released build made"), not the tag. The version string and
the lineage are unrelated facts, so the lineage is a suffix in the `kong.Vars{"version": ...}` value in
`cmd/<binary>`, where every print of the version reads from one expression.

A build tag is the wrong tool when one function has both behaviors keyed off a runtime config (use a config
flag and defensive code), when CI must exercise the dev path by default (a separate package the production
entrypoint does not import), or when the thing is a feature flag that ships to users (a runtime concern).
