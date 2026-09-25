# Contributing

## Setup

<!-- TODO(kickstart): add what your project needs beyond these, each naming the file that pins its version. -->

The machine needs:

- [Go](https://go.dev), at the toolchain `go.mod` names. Every Go tool the gate runs (`task`, `lefthook`,
  `golangci-lint`, `govulncheck`, `go-test-coverage`, `testpair`, `deadcode`) is a `tool` directive in `go.mod`,
  and `go tool <name>` builds each from the module's pin, so nothing else is installed.
- [Bun](https://bun.sh), at the version `packageManager` in `package.json` names. It installs commitlint, Prettier
  and the `yaml` package commitlint's config reads.
- [mise](https://mise.jdx.dev). It installs the tools `mise.toml` pins at the versions `mise.lock` records.
- [git](https://git-scm.com) 2.41 or newer. The absorbed command `MARKERS.md` prints passes `--attr-source`, which
  older git refuses, and the test that runs that command skips on an older git and says why.
- A C compiler on `PATH`, for the race detector. Without one the race row prints that it did not run.
- [gh](https://cli.github.com), optional. When `gh auth token` answers within five seconds, the gate runs zizmor
  online; otherwise zizmor runs offline and no token is needed.

The first run:

```sh
bun install --frozen-lockfile
go tool lefthook install
go tool task check
```

`bun install --frozen-lockfile` installs commitlint, Prettier and `yaml` under `node_modules/`, at the versions
`bun.lock` records. `go tool lefthook install` writes the git hooks. The first gate run builds the Go tools and
installs the mise tools from the lockfile. Before you install a branch you did not write, read [Safety](#safety).

Run `bun install --frozen-lockfile` again after every pull, after every branch switch and in every new worktree,
before you run the gate or commit. The format row and the commit hook start Prettier and commitlint through
`bunx --bun --no-install`, which runs the copy this checkout's `node_modules/.bin` holds. The format row refuses
to start while that copy is missing. The hook does not check, and a missing or stale copy there runs another one
([Troubleshooting](#troubleshooting)). `package.json` carries no install script, so
`bun install --frozen-lockfile --ignore-scripts`, the form the gate's message names for a worktree, installs the
same packages.

The commit hook checks every commit message before it is recorded, and the push hook runs the quick gate and
refuses the push when it fails. The hooks are a convenience, not a control ([Safety](#safety)).

The gate trusts this checkout's `mise.toml` for its own mise commands, and only after the pins checks pass, so the
checkout needs no `mise trust`. A persisted trust would let any mise command here, a shell's `mise activate` hook
included, render a branch's `mise.toml` before any check.

`.claude/settings.json` allows `git status` alone, the allowlist every `zachthedev` repository shares, with deny
entries for `--output` and `--no-index`, and adds nothing to it.

## Safety

A pull request controls its own gate: `Taskfile.yml`, the gate's code under `internal/tools/gate`, `go.mod`, the
hooks and the scripts under `scripts/`. Before you run anything on a branch you did not write, read its diff. Then
install it with `bun install --frozen-lockfile --ignore-scripts`. Some of the branch's code runs before any check
does, and the gate's refusals cannot stop that first local run:

- A local `go tool task` builds Task under a branch's `go.work` and loads the branch's `.env` before its first
  command. The Taskfile sets `GOWORK=off` and `GOFLAGS=-mod=readonly` for every command after that, unless the
  shell exports either.
- A `toolchain` or `go` line in `go.mod` names the compiler, and under `GOTOOLCHAIN=auto` go downloads that
  toolchain before any command runs, the gate's own build included. It comes from `golang.org/toolchain` and is
  checked against the checksum database, and Renovate moves the line on purpose, so the gate refuses neither.
- Bun runs a top-level `preload` from the branch's `bunfig.toml` before Prettier in the format row and before
  commitlint in the commit hook.
- commitlint searches through cosmiconfig, which reads a meta config from the root `.config`, a `cosmiconfig` key
  in the root `package.json` or a root `package.yaml`, whatever config commitlint names. An `$import` there runs a
  module in the commit hook. The gate refuses all three, after the fact.
- The hook scripts lefthook writes start `go tool lefthook` itself, and that build reads a branch's `go.work`
  before any job's `GOWORK=off` applies.

Read the diff before you commit on the branch too, since the commit hook runs the branch's own code.

What reaches the tools from your own environment:

- `BUN_OPTIONS` hands its flags to every direct Bun start. The ones this repository makes, `bun install` and
  `bun run audit`, run no module from a `--preload` in it. The gate withholds it from the processes it starts, and
  a tool started through `bunx --bun --no-install` does not read it. Leave it unset.
- `BUN_INSPECT`, `BUN_INSPECT_CONNECT_TO` and `BUN_INSPECT_PRELOAD`. Leave all three unset. The gate passes them
  to the programs it starts. Bun reads them in a direct start, where `BUN_INSPECT_PRELOAD` runs a module and the
  other two open its inspector. A tool started through `bunx --bun --no-install` ran no such preload.
- A personal env file. `bunx` ignores `--no-env-file`, so Prettier and commitlint load an untracked `.env`,
  `.env.local` or another name Bun loads from the root. `Taskfile.yml` loads `.env` into every task.
  [Troubleshooting](#troubleshooting) says what that can change.

The hooks are not a control:

- A hook runs in your own environment and clears nothing from it.
- lefthook merges a branch's `lefthook-local.*` or `.config/lefthook-local.*` over `lefthook.yml`, and a job there
  with a hook job's name replaces it before any job runs. A hook catches an accident, never a hostile branch.
- A fresh clone runs no hook until `go tool lefthook install` runs.

CI's `commits` job and gate decide the merge.

## Running it

<!-- TODO(kickstart): say how your binary is run from a build output and from source. -->

```sh
go run ./cmd/example
go tool task build && ./dist/example
```

`go tool task build TAGS=dev` compiles the dev-only code in ([Dev-only code](#dev-only-code)). Every build setting
the binary takes through `-ldflags -X` is declared in `internal/buildenv`. Copy `.env.template` to `.env`, which is
gitignored, and uncomment what you need. `Taskfile.yml` loads that file, so a value there reaches every task.

<!-- TODO(kickstart): register your project's generated files in cmd/generate/main.go and list them here. -->

Generated files, and the command that writes each:

- `.env.template`: from `internal/buildenv`, by `go tool task generate`.
- `MARKERS.md`: from the template markers in the tree, by `go tool task generate`. It goes when the template is
  absorbed ([README.md#template-markers](README.md#template-markers)).
- `mise.lock`: `mise lock`, after any edit to `mise.toml`.
- `bun.lock`: `bun install`.
- `go.sum`: `go mod tidy`.

The `generate` row regenerates the first two and fails when the tree differs, so a stale committed copy is caught
before it merges.

## Where code goes

- `cmd/<binary>/`: one directory per binary, holding wiring alone: flag parsing and output. `cmd/generate/` is the
  development tool that writes the generated files and ships in no release.
- `internal/`: the logic, one concern per package, as functions that take data and return data with the I/O at the
  call site. A function another package could import belongs here, never under `cmd/`.
- `internal/tools/`: programs the gate and the build run and nothing ships.
- `scripts/`: the build and test commands that need more than one line, each run by one task.
- `docs/`: the documents [README.md#documentation](README.md#documentation) indexes.

A package lives where `./...` reaches it: never under a directory whose name starts with `_` or `.`, never in
`testdata` unless it is a fixture, and never below a second `go.mod`. `./...` skips each of those, so vet, lint and
the tests never read the code. A reviewer refuses one ([The gate](#the-gate)).

### Dev-only code

A dev affordance, such as a synthetic data source, a mock subprocess injector or a "skip auth" toggle, is gated by
a build tag, not by a config flag. A config flag leaves the code compiled into the production binary, so an
operator who misconfigures, or anyone who can write the config, gets a real attack surface. A build tag means the
code is not in the binary at all.

`go tool task build` produces a production binary. `go tool task build TAGS=dev` and `go tool task test TAGS=dev`
add the dev-tagged files. The `TAGS` variable threads through `build`, `testquick`, `test` and `coverage`.

Two files implement the same symbol with the same signature, and the tag picks exactly one. Callers do not change.

```text
internal/yourpkg/
├── feature.go            //go:build dev      real implementation
├── feature_stub.go       //go:build !dev     refuses, and says what it refuses
├── feature_test.go       //go:build dev
└── feature_stub_test.go  //go:build !dev
```

The stub returns an error naming the rebuild, or no-ops where that is the useful production behavior: a metrics
injector stub returns a discard sink.

```go
//go:build !dev

package yourpkg

// StartLoopbackListener returns an error in production builds.
func StartLoopbackListener(_ string) (*net.UDPConn, error) {
	return nil, fmt.Errorf("yourpkg: loopback listener not compiled in (rebuild with `-tags dev`)")
}
```

What to check when the first one lands:

- Both files need a test pair. `testpair` enforces the pairing, and the stub's test asserts the documented error
  or no-op.
- `LINT_TAGS` in `Taskfile.yml` gains `dev`, so the lint and vet rows read the dev build on every pair. Until it
  does, the packages row refuses the dev-only file, since no build those rows read compiles it.
- `.testcoverage.yml` thresholds are set for the production build. Dev files move the number under `-tags dev`, so
  give them an override rather than letting the threshold drift.
- A CI lane running `go tool task test TAGS=dev` means something only once dev-tagged tests exist.
- `go list`, `go vet` and godoc honor tags. `go list -tags dev ./...` is the dev view, and the default is what
  `pkg.go.dev` shows.

Once a binary has two lineages, the version output says which, and only when the lineage is not the ordinary one.
Name the consequence ("refuses a library a released build made"), not the tag. The version string and the lineage
are unrelated facts, so the lineage is a suffix in the `kong.Vars{"version": ...}` value in `cmd/<binary>`, where
every print of the version reads from one expression.

A build tag is the wrong tool when one function has both behaviors keyed off a runtime config (use a config flag
and defensive code), when CI must exercise the dev path by default (a separate package the production entrypoint
does not import), or when the thing is a feature flag that ships to users (a runtime concern).

## Code

- Errors are returned, wrapped with `fmt.Errorf("doing x: %w", err)`, and inspected with `errors.Is` and
  `errors.AsType`. Nothing is swallowed.
- Every top-level declaration sits in the order `type`, `const`, `var`, `func`, with `init` first among the
  functions. `decorder` in `.golangci.yml` holds it. Several `const` or `var` blocks are fine, grouped by concern.
- Names read at the call site with their package: `config.Load`, not `config.LoadConfig`. Verbs: `Load` reads from
  disk or a database, `Compute` derives from memory, `Build` constructs, `Decode` parses bytes. Suffixes: `Result`
  for one outcome, `Report` for a collection, `Opts` for an input struct.
- Comments say why, never what, and describe the code as it stands. What changed goes in the commit body.
- `go tool task fmt` applies gofumpt and goimports, the formatters the lint row enforces. taplo owns TOML, and the
  toml row hands it every tracked `.toml` file. Prettier owns every other file Go does not.
- A dev-only affordance is gated by a build tag, never by a config flag ([Dev-only code](#dev-only-code)).
- An inline waiver names the rule it waives and says why. `//nolint:<linter> // reason` is the form, and nolintlint
  fails one that names no linter, gives no reason, or waives nothing. A gosec waiver names the rule,
  `// #nosec G<nnn> -- reason`, and takes no other form. gosec also reads `//gosec:disable` as that waiver, so the
  packages row refuses `gosec:disable` behind any comment marker, in any case or spacing. It refuses what neither
  linter reads as well: `//nolint:gosec`, which waives every gosec rule; a nolint naming `all` in any case or
  spacing, or naming `nolintlint`; a nolint behind extra slashes; and `//lint:ignore` or `//lint:file-ignore`.
- A reason is visible text. nolintlint and gosec each accept a reason made of invisible code points alone, such as a
  zero-width space or a soft hyphen, so the packages row refuses a nolint or `#nosec` reason that is empty once
  every default-ignorable code point is dropped.
- A ShellCheck directive in a tracked script reads `# shellcheck disable=SCnnnn[,SCnnnn] # reason` as its whole
  line, with a visible reason, and the scripts row refuses any other. A workflow script carries none.

## Tests

<!-- TODO(kickstart): name the tests that need a real server, machine or network, and how to run them. -->

- Test files pair 1:1 with source files, and `testpair` holds it: `foo.go` has `foo_test.go` and nothing else tests
  it.
- A test is `TestSymbol_Case`, where `Symbol` is declared in the package, and `testpair` holds that too.
  Table-driven by default, with `tt` as the row, `name`, `input`, `want` and `wantErr` as its fields, and `got`
  beside `want` in the assertion.
- Assertions use testify: `require` for what the rest of the test depends on, `assert` for the checks the test
  exists to make, always `(t, want, got)`. `assert.Equal` compares deeply, so no second comparison library.
  `assert.True` is the fallback for a predicate testify has no name for, and it takes a message.
- A test touches no file outside `t.TempDir()` and no network. Anything the code under test can reach in the
  operating system arrives as a parameter, in every case, whether or not the case reaches it today. A real process
  runs only where the process is the thing under test, and the list below names each.
- A test that runs `git` calls `internal/gittest.Isolate` first and names its repository with `git -C`. A hook
  exports its own repository through `GIT_DIR` and `GIT_INDEX_FILE`. Without the isolation, a test run from that
  hook would commit, tag and add remotes there. `Isolate` drops every inherited `GIT_` variable and masks the
  user's git configuration.
- A helper calls `t.Helper()` first and sits above the test functions.
- Coverage thresholds live in `.testcoverage.yml`. A block that cannot be exercised carries a
  `// coverage-ignore: reason` comment, and `force-annotation-comment` fails one with no reason after the marker.

Tests that need a real thing:

- The race row (`go tool task test`) needs a C compiler on `PATH`. `scripts/test-race.sh` says so and skips without
  one.
- `internal/tools/version`'s test runs the tool through `go run` and reads the checkout's own `git describe`, so it
  needs `go` and `git` on `PATH` and a git checkout. A build-time helper has no other caller.
- `internal/remote`'s and `internal/version`'s tests run `git` against a repository they create under
  `t.TempDir()`, so they need `git` on `PATH` and nothing else. What they test is the git call.
- `internal/generate`'s printed-check test runs the absorbed command `MARKERS.md` prints through `sh` in a
  repository it creates the same way, so it needs `sh` and `git` on `PATH`, and it skips without either. The
  command's exit status is what it tests.

## The gate

```sh
go tool task check
```

One command, and it is the whole gate. CI's gate job runs the same command on Linux, macOS and Windows. A local run
can still differ from CI, and [Troubleshooting](#troubleshooting) says how.

```sh
go tool task check:quick
```

The same gate without the race detector and the coverage run, so a push costs seconds and CI pays the minutes. The
push hook runs it.

`go tool task --list` prints every task with what it checks, including the ones `check` never runs: `fmt` rewrites
Go files the way the lint row wants them, `testquick` is the inner loop, `build` and `release` produce binaries.
`go tool task <task>` runs one of them.

The rows meant to run no repository code go first. Then each row that does runs, `generate` and `packages`
through `cmd/generate`, `build` through `scripts/build.sh`, and the test rows, with the tree rules of `gate pins`
after it. Repository code can write any file a later row reads. The tree rules re-run the named refusals such a
row can plant, such as a second Taskfile, a `go.work` or a mise config, and they do not detect a tracked file the
row changed. The first rows can run code too: the format row runs a `.prettierrc` plugin or a `bunfig.toml`
preload, which review holds, and on Linux and macOS the workflows row runs a root entry named `'`, which the
shared `workflows` job refuses.

The first row reads `mise.toml` and `mise.lock` against the expectations in `internal/tools/gate/pins.go` and
installs from the lockfile only after that read passes. `mise.lock` pins `linux-x64`, `macos-arm64` and
`windows-x64`, and a contributor on another platform relocks in a pull request.

CI and the push hook run `go run ./internal/tools/gate pins` on its own before `go tool task`, so the tree rules,
the duplicate-key check included, run before `bun install` reads `package.json`. CI's gate job and both push-hook jobs set `GOWORK=off` and
`GOFLAGS=-mod=readonly`, so every go command reads `go.mod` alone, never a `go.work`, and builds nothing from a
`vendor/` directory. A tracked `go.work` could otherwise replace a dependency of the gate itself with code from the
branch, before `gate pins` refuses the file. `go.mod` still decides the gate's own build: a `replace` builds a
dependency from wherever it points, the checkout included, and a `godebug` line changes how every binary behaves,
the gate among them. So CI's gate job and the push hook first run `scripts/go-mod-check.sh`, which reads `go.mod`
without building anything and refuses a `replace`, a `godebug` line, or a `//go:debug` line in the gate's own
package.

Every row that walks the tree prints what it checked and fails when that is nothing, because Prettier and taplo
exit 0 having read no file. The format row counts the files Prettier names. The toml, workflows and scripts rows
hand taplo, actionlint and ShellCheck every tracked file by name and fail unless the tool's own list matches.
zizmor's row fails unless zizmor logged a completed line for every tracked workflow. The packages row prints how
many packages `go list ./...` matches, the count vet, lint, deadcode, testpair and build share, and fails on none.
A tool that exits with a failure is reported by its exit code, and the row names no cause it did not see. Before
any row lists what git tracks, the gate asks git which work tree it reads and stops unless that is the checkout
itself: an empty `.git` directory at the root makes git list the repository above it without an error. Every tool
a row reads gets `NO_COLOR=1`, and the row strips terminal escape sequences from the tool's output before it reads
a line, since some tools color their output on a CI runner and print it plain locally. A finding quotes the text
it names from a file, so no control character in it reaches the terminal.

The format row starts Prettier through `bunx --bun --no-install`, under the Bun `PATH` names. It first refuses a
checkout whose `node_modules/.bin` holds no Prettier that resolves, through every link, to a regular file, and
names the install to run.

The lint and vet rows run once per os/arch pair `LINT_PLATFORMS` names in `Taskfile.yml`, and once more per build
tag set `LINT_TAGS` names, because a linter reads only the files its build compiles. The pairs are CI's three hosts
and every pair `RELEASE_TARGETS` ships. The packages row refuses the ways code leaves those rows while it still
builds:

- A tracked Go file no pair and tag set compiles, by its build constraint or by a file name naming another
  platform. The first dev-only file needs `dev` in `LINT_TAGS`.
- A pair `RELEASE_TARGETS` ships that `LINT_PLATFORMS` leaves out.
- A tracked Go file whose comments near its package clause say "code generated", "do not edit", "autogenerated
  file" or golangci-lint's swagger marker, in any case, which golangci-lint skips whole under `generated: lax`,
  unless `go run ./cmd/generate list outputs` names the file.
- An inline waiver no linter checks, or one whose reason is invisible, as [Code](#code) lists.

actionlint starts ShellCheck through the gate, which stands in front of it: it reads each `run:` script as
actionlint decoded it from the YAML, refuses any line carrying a ShellCheck directive, and runs the pinned
ShellCheck over the rest. It writes ShellCheck's findings back only once ShellCheck read the whole script and
exited 0 or 1. A regex over the workflow file cannot do this, since YAML escapes and folding hide a directive
ShellCheck still reads. Two canaries prove the wiring on every run: ShellCheck reports an unquoted expansion behind
the stand-in, and the stand-in refuses a directive. actionlint hands ShellCheck a script under `bash` or `sh`
alone, so the workflows row refuses a `shell:` value other than `bash`, `sh` or `pwsh`.

Every test row pipes `go test -json` through `go run ./internal/tools/gate tests`, which fails unless a test ran and
passed and none failed. go test exits 0 when a `-run`, `-skip` or `-short` in a `GOFLAGS` the shell exports skips
every test, and when no test matched, so its exit code alone proves nothing.

The gate sets no deadline of its own on a row. CI's gate job carries `timeout-minutes: 30`, and locally Ctrl-C ends
a hung tool. `gh auth token` alone runs under five seconds, past which the zizmor row runs offline. A program that
exits while a process it started still holds its output fails its row five seconds later, when
`exec.Cmd.WaitDelay` closes the pipes. No program the gate starts inherits `SHELLCHECK_OPTS`, which reaches
ShellCheck through actionlint, `BUN_OPTIONS`, which hands Bun flags such as `--preload`, or a GitHub token
variable, which gh alone gets back.

A few checks run only in CI, each because it needs something a working machine does not have. The `commits` job
lints a pull request's commit range and title, which do not exist before the pull request does. The
`dependency-review` job compares the pull request's modules against its base through GitHub's dependency graph
([Dependencies](#dependencies)). zizmor's online audits (impostor commits, advisories against pinned actions,
version comments that name the wrong tag) run in the `workflows` job on every pull request. They need a GitHub
token, and that job is the one CI job that holds it, so CI's gate job runs zizmor offline. Locally the row runs
online when `gh auth token` answers, and its summary says which mode ran.

The shared `commits` and `workflows` jobs refuse, before a merge, the data files that run code in Bun or bun
install, and the gate does not repeat them: a tracked env file at the root, a tracked `node_modules` path or
`.npmrc`, a `patchedDependencies` key, a `bunfig.toml` key beyond the cooldown, a root file named like a program a
gate starts, a root entry named `'`, and a `secrets: inherit` call into anything but `zachthedev/.github`'s
reusable workflows. A pull request cannot change what either job runs at its pinned commit. It can change
`ci.yml`'s call, and that change waits on the code owner's review like the gate's code.

A reviewer, not the gate, refuses a tracked file no row checks: anything under `dist/`, `coverage/`,
`.claude/worktrees/` or a `.git`, `.sl`, `.svn`, `.hg` or `.jj` directory, a JavaScript or declaration file beyond
the ones a tool needs, such as `commitlint.config.js`, a personal file such as `.claude/settings.local.json`, and a
Go package under an `_` directory or below a second `go.mod`, or a package under `testdata` that a build imports
([Where code goes](#where-code-goes)). Each sits in
the diff and no row reads it. `.prettierrc` holds formatting options alone, and a reviewer refuses a `plugins` key
or a string value, since Prettier loads either as code.

`gate pins` refuses what the programs the gate starts read before any check of their own and no shared job
refuses. Names compare with case folded, because Windows and macOS open a tracked `.ENV` as `.env`. Tracked, it
refuses:

- An env file Bun loads, below the root. The `commits` job refuses one at the root. `.gitignore` names all eight,
  and an untracked one passes.
- A `package.json` carrying a duplicated key at any depth. Bun keeps the first copy, while jq, which the `commits`
  job reads the file with, and Go keep the last, so no check can tell what Bun reads.
- A `cosmiconfig` key in the root `package.json`, which commitlint's config loader reads ([Safety](#safety)).
- Anything under `vendor/`, which go builds from in place of the module cache when no `-mod` flag is set.
- A workflow whose extension is anything but `.yml`, the one spelling actionlint's list and zizmor's collection
  both match.
- An inline `zizmor: ignore[...]` comment anywhere under `.github`. A waiver lives in the rules of
  `.github/zizmor.yml`, with the file it covers. The `workflows` job refuses one too, but its search skips a file
  `.gitattributes` marks binary, and the gate reads every file itself.
- A `replace`, `godebug` or `ignore` line in `go.mod`. An `ignore` line takes its directories out of every `./...`
  row.

It refuses a root `.config` in any case and any form, committed or not, because mise, the dotnet tool manifest,
cosmiconfig's meta config and lefthook all read configs from it. It refuses a root `package.yaml` the same way,
since cosmiconfig reads a meta config there too. It refuses a root `vendor` on disk, which go builds from unless a
`-mod` flag says otherwise, while CI's gate sets `-mod=readonly`.

Every program the gate starts that searches for its own config runs with one config named: `.prettierrc` (with
`.prettierignore` and no `.editorconfig`), `.golangci.yml`, `.taplo.toml`, `.github/zizmor.yml` and
`commitlint.config.js`, and the `fmt` task names `.golangci.yml` too. Naming the config stops every other config
read each of those tools makes, measured with a planted file of every name each one searches, so the gate refuses
none of those names. Where a tool has no named form, `gate pins` refuses every other name it searches, tracked or
on disk, so a local gate agrees with CI's. actionlint runs under an empty config of the gate's own, and a run by
hand reads `.github/actionlint.yaml` or `.yml`, so both are refused. lefthook and Task take no name from the
repository, so `lefthook.yml` and `Taskfile.yml` are the only names of theirs allowed, a `.taskrc` included.
`go.work` and `go.work.sum` are refused. A `tsconfig.json` or `jsconfig.json` is refused, because Bun applies its
`paths` to commitlint's and Prettier's own imports, and nothing here is TypeScript.
`internal/tools/gate/configs.go` lists every name.

On disk the check walks the work tree and skips what the tools skip: `node_modules`, which an install fills with
packages' own configs, `.claude/worktrees`, each holding a copy of every file here, and the version control
directories. A `tsconfig.json`, `jsconfig.json`, `go.work` or `go.work.sum` on disk counts at the root alone, and
tracked in any directory. lefthook's local configs (`lefthook-local.yml` and `.lefthook-local` in any of lefthook's
forms) belong to one contributor: `.gitignore` names them, and the gate refuses one only when committed, since one
can replace the pins job.

No gate row holds a config file's text against a copy of its own. `.github/CODEOWNERS` names the code owner for
every path, and the `default-branch` ruleset requires the code owner's review, so a change to a config a row reads,
`.prettierignore`, `.golangci.yml`, `.testcoverage.yml` and the two allow lists among them, waits on that review
like a change to the gate's code. git starts with `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` and, on
Windows, `SystemRoot`, and inherits nothing. No `GIT_INDEX_FILE` or `GIT_DIR` from a `.env` changes what it lists.

Every mise command a task runs goes through `go run ./internal/tools/gate mise`. It runs the `gate pins` checks
first and starts mise only when they pass, because mise renders a trusted config's templates as it loads it. It
starts mise under an environment built from an allow-list, never the inherited one. On Windows that is
`SystemRoot` and `LOCALAPPDATA`, read from the system, with `TEMP` and `TMP`. Elsewhere it is `HOME` and `TMPDIR`.
The proxy variables pass on every system. The gate sets every `MISE_` name itself:
`MISE_OVERRIDE_CONFIG_FILENAMES=mise.toml`, `MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES=none`, an empty `MISE_ENV`,
`MISE_AUTO_ENV=false`, the url_replacements rule, and the checkout as the one trusted config path. A
`MISE_GLOBAL_CONFIG_FILE` or `MISE_BACKENDS_<TOOL>` from a `.env`, the shell or CI never reaches mise. mise, git, go
and bun resolve from `PATH` to an absolute path outside the checkout, and `os/exec` already refuses a match in the
working directory. Outside compares file identity rather than spelling, over the path found and over its final
path with every link and junction followed, so an 8.3 name for the checkout, or a junction or symbolic link from
elsewhere into a directory below it, does not pass. A hard link is one file under two names, so a hard link from a
`PATH` directory to a file in the checkout passes, until git replaces that file on the next checkout and breaks the
link.

The race row needs cgo, and cgo needs a C compiler. Without one it prints that it did not run and passes on your
machine, so read the skip line: it is the difference between a row that passed and a row that was not exercised.
Under CI the same skip is a failure, so a runner that lost its compiler goes red.

Nothing in the gate asserts the repository's alignment with the set's standard. The maintainer keeps that record
outside the repository.

## Commit messages

Every commit follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). commitlint checks the
message in the commit hook and again in CI, over the pull request's commits and its title.

```text
type(scope): subject

body
```

The type is one of those `@commitlint/config-conventional` accepts, and it names the change's effect on the people
who run the binary. `feat`, `fix`, `perf` and `revert` reach the changelog, and every other type stays out of it,
`build` included. `changelog-sections` in `release-please-config.json` holds that split, so a change to it changes
this paragraph in the same commit. A change to the gate, a hook or the tools `mise.toml` pins is `chore`, and a
change to this repository's workflows is `ci`. A dependency linked into the binary is `fix(deps)`, and one that
only builds, checks or tests is `chore(deps)`. A document is `docs`, because readers reach it from the default
branch. A revert is written `revert(<scope>): <what it undoes, in fresh words>`, with a `Refs: <sha>` footer for
each reverted commit. commitlint skips git's `Revert "..."` subject, and release-please cannot parse it. Repeating
the reverted header after `revert: ` can pass the 72-character limit.

A template's users are the repositories created from it or aligned to it. So in this template a change to a file a
clone copies, `CONTRIBUTING.md` and the other copied documents included, is `feat` or `fix`, and the changelog lists
what an aligning repository must copy. A pin bump stays `chore`, because each clone's own Renovate moves its pins.
A document no clone copies stays `docs`.

<!-- TODO(kickstart): delete the paragraph above; your repository is not a template. -->

The scope is optional. `.github/commit-scopes.json` lists each scope and what it covers, and commitlint accepts no
other. Omit the scope rather than invent one. A new part of the repository earns a scope in that file, in the
change that adds the part. A scope never repeats the type: `docs(docs)`, `ci(ci)` and `test(tests)` take the bare
type, `docs:`, `ci:` and `test:`. A reviewer holds that rule.

The header and every body line stay within 72 characters. The header's limit applies to what lands on `main`,
because github.com cuts a subject at 73. A pull request merges by squash, the one method the repository allows,
under GitHub's default text. A one-commit pull request lands its commit's subject and body. A longer one lands its
title, with each commit as a bullet in the body. Either subject gets ` (#N)` appended, so the title, or a lone
commit's subject, stays within 64 to 67 characters, as the number's digits allow. The commit hook checks 72 as
written. CI's `commits` job lints every commit, and the title or a lone commit's subject with ` (#N)` appended. A
Dependabot pull request whose landed header runs past 72 fails that lint and is closed, and the bump is taken by
hand.

A pull request's title takes the type of its most user-facing commit, and `!` when any commit breaks something
users see. A squash of several commits lands the title alone, and release-please reads nothing else, so a title
without the `!` loses the break and its bump. If a merged title hid a user-facing change, put the corrected headers
between `BEGIN_COMMIT_OVERRIDE` and `END_COMMIT_OVERRIDE` in the merged pull request's description before the
release pull request merges. release-please reads them in place of the landed message.

A body carries what the diff cannot show: what was wrong, what the change does now, and what was left undone. A
breaking change carries `!` after the type or scope and explains the break in the body. `!` marks a break users
see. A break only contributors see, such as a renamed task, carries none, because `!` cuts a release whatever the
type.

Every version heading in `CHANGELOG.md` links GitHub's compare view from the previous tag, which lists every change
in the release, hidden types included. `git log --oneline v<previous>..v<version>` lists the same.

## Dependencies

Every dependency is pinned to an exact version and moved by Renovate under a three-day cooldown, from the presets
`.github/renovate.json` extends. Renovate is the only bot that opens pull requests. A security fix comes from a
Dependabot alert and skips the schedule and the cooldown. `go.mod` names every module, direct and indirect, so
Renovate fixes an indirect one too.

Go has no cooldown file of its own, so nothing in this repository gates what `go get` or `go mod tidy` resolve by
hand. The cooldown is Renovate's. Bun's is in `bunfig.toml` as well, so a lock file refresh in a container observes
it.

A hand pin ahead of the cooldown records its audit in the commit body: the release notes read, the maintainer
checked, the diff against the previous version. A waived advisory is an `allow-ghsas` entry in `ci.yml`'s
`dependency-review` job, with a comment naming the advisory, what it blocks, why shipping is safer and the
condition that removes it. A red advisory check blocks the merge, because the required checks sit in a ruleset
with no bypass actor.

The `dependency-review` job compares the pull request's modules against its base through GitHub's dependency
graph, which reads `go.mod` and `go.sum` in full, direct and indirect modules alike, including the modules behind
the `tool` directives. Under Bun it sees the direct packages `package.json` names alone. It blocks on a high or
critical advisory in what the pull request adds. `govulncheck` runs in the gate beside it, over the module and over its
`tool` directives, and blocks only on a call either one reaches. A reachable advisory in a tool's tree with no fixed
version is held one of two ways until a fix ships: pin that tool back to a release without the vulnerable module,
or take `go tool govulncheck tool` out of the `vulncheck` task with a dated comment there naming the advisory.

`audit.yml` is the scheduled report, on one daily clock, and a red run is a report, never a check. Its `audit` job
runs `bun run audit`, the `package.json` script `bun audit --audit-level=high`, over every package `bun.lock` names,
transitives included. `bun.lock` holds commitlint, Prettier and the `yaml` package commitlint's config reads,
tooling that ships in nothing, and no bot opens a pull request for a transitive advisory there. The script is the
one home for a `--ignore` waiver. Its `workflows` job calls the reusable `workflows` workflow, so zizmor's online
audits of every pinned action run with no pull request open. Go's whole-tree reader is `govulncheck`, and it runs
in the gate on every push and pull request rather than on a clock.

### Tool integrity

Each tool the gate runs, and how its bytes are held to their source. Four tiers: provenance, a checksum in a pinned
tree, a checksum recorded by a third party, a version alone.

- actionlint and zizmor: provenance. `mise.lock` records `github-attestations`, mise verifies the attestation on
  every install, and the gate refuses a lockfile that drops the line.
- ShellCheck and taplo: a checksum in a pinned tree, `mise.lock`. taplo's checksums are the sha256 of its release
  artifacts, computed once from a download, as `mise.toml` records.
- golangci-lint, govulncheck, task, lefthook, go-test-coverage, testpair and deadcode: a checksum in a pinned tree,
  `go.sum`, checked against the checksum database on every build.
- Prettier, commitlint and `yaml`: a checksum in a pinned tree, `bun.lock`.
- Go and Bun themselves: a version alone. The pin file plus the cooldown is the control, because the setup actions
  verify no download.
- mise itself: a publisher signature, which `jdx/mise-action` checks against the release's signed checksums.

For every mise tool, `internal/tools/gate/pins.go` holds the pin and the lockfile's version to the tool's release
shape, `major.minor.patch` in ASCII digits, before it builds any url from them. It holds each lockfile `url`, byte
for byte, to the release asset it names for that platform. It holds each `url_api` to that repository's asset path
followed by an asset id, and it refuses a nested `platforms` table. mise falls back to `url_api` when a download
fails, and an asset id binds no version. So `mise.toml` carries a `url_replacements` rule that sends every such
fetch to a host that does not exist. `gate mise` sets the same rule in `MISE_URL_REPLACEMENTS`, which no committed
config file can lift.

`mise.toml` holds `[tools]`, `[tool_config]` and `[settings]` alone, because `[hooks]`, `[env]`, `[vars]`,
`[tasks]` or a tool's options can run a command during `mise install`. `pins.go` compares `[tool_config]` and
`[settings]` whole. A tool entry, and a lockfile entry's `options`, carries only `version` and a `version_prefix`
equal to the tag prefix. A tool entry in any other form, the `[[tools.<name>]]` array of tables included, is
refused. Every `mise.lock` key sits on an allow-list, each tool name under `tools` included, and `lockfile_version`
must be the format `mise lock` writes.

mise merges every config file it finds, each with its sibling lockfile, so a `mise.local.toml` beside a
`mise.local.lock` would decide what an install fetches. `pins.go` refuses every mise config or lock file other than
`mise.toml` and `mise.lock`, `.tool-versions` and `.miserc.toml` included. It refuses a link at the root or under
`.mise` or `mise`, which mise would follow, and the root `.config` ([The gate](#the-gate)).

## Releases

release-please opens one release pull request from the commits on `main` and keeps it current. Merging it tags the
merge commit `v<version>` and creates a draft release. The same run builds the binaries and their `SHA256SUMS` with
`go tool task release`, attests every file, attaches them to the draft, and flips it public after the approval the
`release` environment holds. [docs/install.md#check-the-download](docs/install.md#check-the-download) names both
checks a user runs.

[Commit messages](#commit-messages) names the types that reach the changelog. release-please owns `CHANGELOG.md`
and `.release-please-manifest.json`.

## Troubleshooting

A local run that fails or disagrees with CI:

- A stale or missing install. The format row refuses to start with no Prettier in `node_modules/.bin` and names
  the install to run. A stale one runs the version it holds, which can disagree with the one `bun.lock` pins, and
  CI installs frozen before its gate. Run `bun install --frozen-lockfile` after every pull, after every branch
  switch and in every worktree ([Setup](#setup)).
- The other copy `bunx` may run. With no copy in this checkout's `node_modules/.bin`, the commit hook's `bunx`
  runs commitlint from a parent directory's `node_modules/.bin`, from `PATH` or from its own cache, none of them the
  version `bun.lock` pins. The hook does not check. Install, and the hook runs the pinned copy again.
- A package `bun.lock` no longer names. `bun install --frozen-lockfile` does not prune it, so a stale
  `node_modules/` keeps a package CI never installs. After a dependency removal, delete `node_modules/` and install
  again.
- An env file. `bunx` ignores `--no-env-file`, so an untracked `.env`, `.env.local` or another name Bun loads from
  the root reaches Prettier and commitlint, and a variable there can change their results. `Taskfile.yml` loads
  `.env` into every task as well. CI has none of them. Move the file aside to run what CI runs
  ([Safety](#safety)).
- zizmor's online audits. They run on your machine when gh answers with a token and never in CI's gate job
  ([The gate](#the-gate)). `ZIZMOR_OFFLINE=1` does not reach zizmor, so take gh off `PATH` to run what CI runs.
- A checkout another account owns: a devcontainer volume, a network share, or a directory another user created.
  git refuses it with "detected dubious ownership", and the gate stops. git's own remedy, a `safe.directory` entry,
  sits in the global config the gate never reads, by design. Clone the checkout as the account that runs the gate,
  or have that account take ownership of it.
- A row that fails five seconds after its tool exits, because a process the tool started still holds its output.
  That process runs on, so find it and end it.
- A race row that prints that it did not run. The machine has no C compiler on `PATH` ([Setup](#setup)).

## What never happens

- Nobody hand-edits `CHANGELOG.md` or `.release-please-manifest.json`. release-please writes both from the commits,
  and a hand edit is overwritten or, worse, shifts the next version it computes. The one exception is the template
  reset. A repository created from this template deletes `CHANGELOG.md` and writes `{}` into the manifest once,
  before its first release. The directive above `release-pr` in `.github/workflows/cd.yml` carries it until the
  clone absorbs the template. The go release type keeps no version file, so those two files are the whole reset.
- No `mise.lock` line is written outside `mise lock`. The lockfile is what an install fetches and compares, and the
  gate holds it to the expectations in `internal/tools/gate/pins.go`. A hand-written line is a line nothing
  verified.
- Nothing merges past a red gate. The gate is the one check between a change and a release. The required checks
  and the code scanning verdict sit in a ruleset with no bypass actor.
- No version number goes into prose. A version lives in the file that pins it, so a bump is one edit and no
  document goes stale.
- No `Makefile`. `Taskfile.yml` is the runner, and every command CI runs is a task a contributor runs.
