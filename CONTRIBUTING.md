# Contributing

## Setup

Install before committing. [docs/dev.md#prerequisites](docs/dev.md#prerequisites) names what the machine needs
and [docs/dev.md#first-run](docs/dev.md#first-run) the commands, which install the dependencies and the git
hooks. The commit hook checks every commit message before it is recorded, and the push hook runs the quick
gate and refuses the push when it fails. The commit hook resolves commitlint from `node_modules/`, so a clone
with nothing installed refuses every commit until `bun install` runs.

## The gate

```sh
go tool task check
```

One command, and it is the whole gate. CI's gate job runs the same command on Linux, macOS and Windows, so a
green run on your machine is a green run there.

```sh
go tool task check:quick
```

The same gate without the race detector and the coverage run, so a push costs seconds and CI pays the
minutes. The push hook runs it.

`go tool task --list` prints every task with what it checks, including the ones `check` never runs: `fmt`
rewrites Go files the way the lint row wants them, `testquick` is the inner loop, `build` and `release`
produce binaries. `go tool task <task>` runs one of them.

The first row reads `mise.toml` and `mise.lock` against the expectations in `internal/tools/gate/pins.go`
and installs from the lockfile only after that read passes. `mise.lock` pins `linux-x64`, `macos-arm64` and
`windows-x64`, and a contributor on another platform relocks in a pull request.

The race row needs cgo, and cgo needs a C compiler. Without one it prints that it did not run and passes on
your machine, so read the skip line: it is the difference between a row that passed and a row that was not
exercised. Under CI the same skip is a failure, so a runner that lost its compiler goes red.

A few checks run only in CI, each because it needs something a working machine does not have. The
`commits` job lints a pull request's commit range and title, which do not exist before the pull request
does. The `dependency-review` job compares the pull request's modules against its base through GitHub's
dependency graph, which reads `go.mod` and `go.sum` in full, direct and indirect modules alike, including
the modules behind the `tool` directives. That is the one leg the review covers here, and it blocks on a
high or critical advisory in what the pull request adds. `govulncheck` runs in the gate beside it and
blocks only on a call the module reaches.

Nothing in the gate asserts the repository's alignment with the handbook. The local `REPOS.md` on the
maintainer's machine is that record.

## Commit messages

Every commit follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). commitlint
checks the message in the commit hook and again in CI, over the pull request's commits and its title.

```text
type(scope): subject

body
```

The type is one of those `@commitlint/config-conventional` accepts. A change to what the binary does is
`feat` or `fix`. A change to the gate, the workflows or the pins is `chore` or `ci`, never `fix`.

The scope is optional. `.github/commit-scopes.json` lists each scope and what it covers, and commitlint
accepts no other. Omit the scope rather than invent one. A new part of the repository earns a scope in
that file, in the change that adds the part.

The header and every body line stay within 72 characters. A squash merge lands the pull request title as
the commit subject with ` (#NNN)` appended, and CI lints that composed subject, so keep the title itself
within 65.

A body carries what the diff cannot show: what was wrong, what the change does now, and what was left
undone. A breaking change carries `!` after the type or scope and explains the break in the body.

## Where code goes

- `cmd/<binary>/`: one directory per binary, holding wiring alone: flag parsing and output. `cmd/generate/`
  is the development tool that writes the generated files and ships in no release.
- `internal/`: the logic, one concern per package, as functions that take data and return data with the
  I/O at the call site. A function another package could import belongs here, never under `cmd/`.
- `internal/tools/`: programs the gate and the build run and nothing ships.
- `scripts/`: the build and test commands that need more than one line, each run by one task.
- `docs/`: the documents [README.md#documentation](README.md#documentation) indexes.

## Tests

- Test files pair 1:1 with source files, and `testpair` holds it: `foo.go` has `foo_test.go` and nothing
  else tests it.
- A test is `TestSymbol_Case`, where `Symbol` is declared in the package, and `testpair` holds that too.
  Table-driven by default, with `tt` as the row, `name`, `input`, `want` and `wantErr` as its fields, and
  `got` beside `want` in the assertion.
- Assertions use testify: `require` for what the rest of the test depends on, `assert` for the checks the
  test exists to make, always `(t, want, got)`. `assert.Equal` compares deeply, so no second comparison
  library. `assert.True` is the fallback for a predicate testify has no name for, and it takes a message.
- A test touches no file outside `t.TempDir()` and no network. Anything the code under test can reach in
  the operating system arrives as a parameter, in every case, whether or not the case reaches it today. A
  real process runs only where the process is the thing under test: `internal/remote` and
  `internal/version` run `git` against a repository they create under `t.TempDir()`, with the user's git
  configuration masked, because what they test is the git call; `internal/tools/version` runs the tool
  through `go run` and reads the checkout's own `git describe`, because a build-time helper has no other
  caller. [docs/dev.md#tests-that-need-a-real-thing](docs/dev.md#tests-that-need-a-real-thing) lists them.
- A helper calls `t.Helper()` first and sits above the test functions.
- Coverage thresholds live in `.testcoverage.yml`. A function that cannot be exercised carries a
  `// coverage:ignore (reason)` comment, and a branch that cannot be reached carries `// coverage:partial
(reason)` on its line. The comments explain; the thresholds enforce.

## Code

- Errors are returned, wrapped with `fmt.Errorf("doing x: %w", err)`, and inspected with `errors.Is` and
  `errors.AsType`. Nothing is swallowed.
- Every top-level declaration sits in the order `type`, `const`, `var`, `func`, with `init` first among
  the functions. `decorder` in `.golangci.yml` holds it. Several `const` or `var` blocks are fine, grouped
  by concern.
- Names read at the call site with their package: `config.Load`, not `config.LoadConfig`. Verbs: `Load`
  reads from disk or a database, `Compute` derives from memory, `Build` constructs, `Decode` parses bytes.
  Suffixes: `Result` for one outcome, `Report` for a collection, `Opts` for an input struct.
- Comments say why, never what, and describe the code as it stands. What changed goes in the commit body.
- `go tool task fmt` applies gofumpt and goimports, the formatters the lint row enforces. Prettier owns
  every file Go does not.
- A dev-only affordance is gated by a build tag, never by a config flag.
  [docs/dev.md#dev-only-code](docs/dev.md#dev-only-code) has the file shape and what to check when the
  first one lands.

## Dependencies

Every dependency is pinned to an exact version and moved by Renovate under a three-day cooldown, from the
presets `.github/renovate.json` extends. Renovate is the only bot that opens pull requests. A security fix
comes from a Dependabot alert and skips the schedule and the cooldown; `go.mod` names every module, direct
and indirect, so Renovate fixes an indirect one too.

Go has no cooldown file of its own, so nothing in this repository gates what `go get` or `go mod tidy`
resolve by hand. The cooldown is Renovate's. Bun's is in `bunfig.toml` as well, so a lock file refresh in
a container observes it.

A hand pin ahead of the cooldown records its audit in the commit body: the release notes read, the
maintainer checked, the diff against the previous version. A waived advisory is an `allow-ghsas` entry in
`ci.yml`'s `dependency-review` job, with a comment naming the advisory, what it blocks, why shipping is
safer and the condition that removes it. A red advisory check is never merged past with `--admin`.

`audit.yml` is the scheduled report. Its one job calls the reusable `workflows` workflow weekly, so
zizmor's online audits of every pinned action run with no pull request open; a red run is a report, never
a check. It carries no advisory reader, because Go's whole-tree reader is `govulncheck`, and it runs in the
gate on every push and pull request rather than on a clock. `bun.lock` holds commitlint and Prettier alone,
tooling that ships in nothing, and the dependency review reads what a pull request adds to it.

## Releases

release-please opens one release pull request from the commits on `main` and keeps it current. Merging
it tags the merge commit `v<version>` and creates a draft release. The same run builds the binaries with
`go tool task release`, attests them, attaches them to the draft, and flips it public after the approval
the `release` environment holds.

The types that appear in the changelog are the keys under `changelog-sections` in
`release-please-config.json`, which is the one place that list lives. release-please owns `CHANGELOG.md`
and `.release-please-manifest.json`.

The first release is `0.1.0`, held by `initial-version` in the same file, because the interface is not
settled. Below `1.0.0` a breaking change is a minor bump, so the changelog carries the break.

## What never happens

- Nobody hand-edits `CHANGELOG.md` or `.release-please-manifest.json`. release-please writes both from the
  commits, and a hand edit is overwritten or, worse, shifts the next version it computes.
- No `mise.lock` line is written outside `mise lock`. The lockfile is what an install fetches and compares,
  and the gate holds it to the expectations in `internal/tools/gate/pins.go`. A hand-written line is a line
  nothing verified.
- Nothing merges past a red gate. The gate is the one check between a change and a release, and a
  `--admin` merge over a red advisory check ships the advisory.
- No version number goes into prose. A version lives in the file that pins it, so a bump is one edit and
  no document goes stale.
- No `Makefile`. `Taskfile.yml` is the runner, and every command CI runs is a task a contributor runs.

## Tool integrity

Each tool the gate runs, and how its bytes are held to their source. Four tiers: provenance, a checksum
in a pinned tree, a checksum recorded by a third party, a version alone.

- actionlint and zizmor: provenance. `mise.lock` records `github-attestations`, mise verifies the
  attestation on every install, and the gate refuses a lockfile that drops the line.
- ShellCheck: a checksum in a pinned tree, `mise.lock`.
- golangci-lint, govulncheck, task, lefthook, go-test-coverage, testpair and deadcode: a checksum in a
  pinned tree, `go.sum`, checked against the checksum database on every build.
- Prettier and commitlint: a checksum in a pinned tree, `bun.lock`.
- Go and Bun themselves: a version alone. The pin file plus the cooldown is the control, because the
  setup actions verify no download.
- mise itself: a publisher signature, which `jdx/mise-action` checks against the release's signed
  checksums.

`MISE_BACKENDS_<TOOL>` overrides a tool's backend from the environment and no setting reports it. The
gate does not close that gap. `mise which` answers from the project's pins only once `mise trust` has run
for this checkout; before that it answers from a global install, if one exists, and the gate does not
check which.

## Claude Code

`.claude/settings.json` carries the read-only git allowlist every `zachthedev` repository shares and
adds nothing to it.
