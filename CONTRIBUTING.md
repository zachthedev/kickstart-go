# Contributing

## Setup

Install before committing. [docs/dev.md#prerequisites](docs/dev.md#prerequisites) names what the machine needs
and [docs/dev.md#first-run](docs/dev.md#first-run) the commands, which install the dependencies and the git
hooks. The commit hook checks every commit message before it is recorded, and the push hook runs the quick
gate and refuses the push when it fails. The commit hook resolves commitlint from `node_modules/`, so a clone
with nothing installed refuses every commit until `bun install` runs.

`.claude/settings.json` allows `git status` alone, the allowlist every `zachthedev` repository shares, with deny
entries for `--output` and `--no-index`, and adds nothing to it.

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

CI and the push hook run `go run ./internal/tools/gate pins` on its own before `go tool task`. Task loads `.env`
into every task before the first command, and a tracked copy could set `GOFLAGS` for the gate's own build. In
CI the same step runs before `bun install`, so no tracked `.npmrc` or `node_modules` path reaches the install. Some
things still run before any check, and the gate refuses each only afterward:

- A local `go tool task` builds Task under a branch's `go.work` and loads the branch's `.env` before its first
  command. The Taskfile sets `GOWORK=off` and `GOFLAGS=-mod=readonly` for every command after that, unless the
  shell exports either.
- A `toolchain` or `go` line in `go.mod` names the compiler, and under `GOTOOLCHAIN=auto` go downloads that
  toolchain before any command runs, the gate's own build included. It comes from `golang.org/toolchain` and is
  checked against the checksum database, and Renovate moves the line on purpose, so the gate refuses neither.
- The commit hook starts commitlint under Bun by its path in `node_modules`, and Bun runs a `bunfig.toml` preload
  first.
- commitlint's config loader, cosmiconfig, runs a `.config/config.*` module whatever config is named, so one can
  run in the commit hook. The gate refuses the root `.config` whole.

Read a branch's diff before running anything from it, and install a branch you have not read with `bun install
--ignore-scripts`. The hooks are a convenience, not a control. CI's `commits` and gate jobs are the control.

CI's gate job and both push-hook jobs set `GOWORK=off` and `GOFLAGS=-mod=readonly`, so every go command reads
`go.mod` alone, never a `go.work`, and builds nothing from a `vendor/` directory. A tracked `go.work` could
otherwise replace a dependency of the gate itself with code from the branch, before `gate pins` refuses the file.
`go.mod` still decides the gate's own build: a `replace` builds a dependency from wherever it points, the checkout
included, and a `godebug` line changes how every binary behaves, the gate among them. So CI's gate job and the
push hook first run `scripts/go-mod-check.sh`, which reads `go.mod` without building anything and refuses a
`replace`, a `godebug` line, or a `//go:debug` line in the gate's own package. One build stays outside those jobs:
the hook scripts lefthook writes start `go tool lefthook` itself, and that build reads a branch's `go.work` before
any job's `GOWORK=off` applies.

Every row that walks the tree prints what it checked and fails when that is nothing, because Prettier and taplo
exit 0 having read no file. The format row counts the files Prettier names. The toml, workflows and scripts rows
hand taplo, actionlint and ShellCheck every tracked file by name and fail unless the tool's own list matches.
zizmor's row fails unless zizmor logged a completed line for every tracked workflow. The packages row prints how
many packages `go list ./...` matches, the count vet, lint, deadcode, testpair and build share, and fails on none.
Before any row lists what git tracks, the gate asks git which work tree it reads and stops unless that is the
checkout itself: an empty `.git` directory at the root makes git list the repository above it without an error.
Every tool a row reads gets `NO_COLOR=1`, and the row strips terminal escape sequences from the tool's output
before it reads a line, since some tools color their output on a CI runner and print it plain locally.

The lint and vet rows run once per os/arch pair `LINT_PLATFORMS` names in `Taskfile.yml`, and once more per build
tag set `LINT_TAGS` names, because a linter reads only the files its build compiles. The pairs are CI's three
hosts and every pair `RELEASE_TARGETS` ships. The packages row refuses the ways code leaves those rows while it
still builds:

- A package of this module that any of those builds, or a test, reaches and `./...` skips. A directory starting
  with `_` or named `testdata` does this, and a build constraint can import one on a single pair alone.
- A tracked Go file under a directory starting with `_`, which `./...` never matches.
- A tracked Go file no pair and tag set compiles, by its build constraint or by a file name naming another
  platform. The first dev-only file needs `dev` in `LINT_TAGS`.
- A pair `RELEASE_TARGETS` ships that `LINT_PLATFORMS` leaves out.
- A tracked Go file whose comments near its package clause say "code generated", "do not edit", "autogenerated
  file" or golangci-lint's swagger marker, in any case, which golangci-lint skips whole under `generated: lax`,
  unless `go run ./cmd/generate list outputs` names the file.
- An inline waiver no linter checks, as [Code](#code) lists.

zizmor's row also holds every job that passes `secrets: inherit` to a call into `zachthedev/.github`'s reusable
workflows. `.github/zizmor.yml` waives the finding by file, and a zizmor waiver binds to a file or a line, never to
what a job calls. So the row reads every such call from a second zizmor pass with no config and no ignores, and
prints how many it held. The hold covers `secrets: inherit` alone. A job that passes named secrets to another
repository's workflow sits outside it, as a run step that prints a secret does, and review is what reads both.

actionlint starts ShellCheck through the gate, which stands in front of it: it reads each `run:` script as
actionlint decoded it from the YAML, refuses any line carrying a ShellCheck directive, and runs the pinned
ShellCheck over the rest. A regex over the workflow file cannot do this, since YAML escapes and folding hide a
directive ShellCheck still reads. Two canaries prove the wiring on every run: ShellCheck reports an unquoted
expansion behind the stand-in, and the stand-in refuses a directive. actionlint hands ShellCheck a script under
`bash` or `sh` alone, so the workflows row refuses a `shell:` value other than `bash`, `sh` or `pwsh`.

Every test row pipes `go test -json` through `go run ./internal/tools/gate tests`, which fails unless a test ran
and passed and none failed. go test exits 0 when a `-run`, `-skip` or `-short` in a `GOFLAGS` the shell exports
skips every test, and when no test matched, so its exit code alone proves nothing.

The gate sets no deadline of its own on a row. CI's gate job carries `timeout-minutes: 30`, and locally Ctrl-C
ends a hung tool. `gh auth token` alone runs under five seconds, past which the zizmor row runs offline. A
program that exits while a process it started still holds its output fails its row five seconds later, when
`exec.Cmd.WaitDelay` closes the pipes, and that process runs on, so end it yourself. No program the gate starts
inherits `SHELLCHECK_OPTS`, which reaches ShellCheck through actionlint, `BUN_OPTIONS`, which hands
Bun flags such as `--preload`, `BUN_INSPECT_PRELOAD`, which runs a module in every Bun start, `BUN_INSPECT` or
`BUN_INSPECT_CONNECT_TO`, which open Bun's inspector, or a GitHub token variable, which gh alone gets back.

`gate pins` stops with git's "detected dubious ownership" when another account owns the checkout: a devcontainer
volume, a network share, or a directory another user created. git's own remedy, a `safe.directory` entry, sits
in the global config the gate never reads. The fix is the directory's owner: clone it as the account that runs
the gate, or have that account take ownership of it.

The race row needs cgo, and cgo needs a C compiler. Without one it prints that it did not run and passes on
your machine, so read the skip line: it is the difference between a row that passed and a row that was not
exercised. Under CI the same skip is a failure, so a runner that lost its compiler goes red.

A few checks run only in CI, each because it needs something a working machine does not have. The
`commits` job lints a pull request's commit range and title, which do not exist before the pull request
does. The `dependency-review` job compares the pull request's modules against its base through GitHub's
dependency graph, which reads `go.mod` and `go.sum` in full, direct and indirect modules alike, including
the modules behind the `tool` directives. That is the one leg the review covers here, and it blocks on a
high or critical advisory in what the pull request adds. `govulncheck` runs in the gate beside it, over the
module and over its `tool` directives, and blocks only on a call either one reaches. Both commands stay in the
gate. A reachable advisory in a tool's tree with no fixed version is held one of two ways until a fix ships:
pin that tool back to a release without the vulnerable module, or take `go tool govulncheck tool` out of the
`vulncheck` task with a dated comment there naming the advisory.

zizmor's online audits (impostor commits, advisories against pinned actions, version comments that name the
wrong tag) run in the `workflows` job on every pull request. They need a GitHub token, and that job is the
one CI job that holds it, so CI's gate job runs zizmor offline. Locally the row runs online when
`gh auth token` answers, and its summary says which mode ran.

Nothing in the gate asserts the repository's alignment with the handbook. The local `REPOS.md` on the
maintainer's machine is that record.

## Commit messages

Every commit follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). commitlint
checks the message in the commit hook and again in CI, over the pull request's commits and its title.

```text
type(scope): subject

body
```

The type is one of those `@commitlint/config-conventional` accepts, and it names the change's effect on the
people who run the binary. `feat`, `fix`, `perf` and `revert` reach the changelog, and every other type stays
out of it, `build` included. `changelog-sections` in `release-please-config.json` holds that split, so a
change to it changes this paragraph in the same commit. A change to the gate, a hook or the tools `mise.toml`
pins is `chore`, and a change to this repository's workflows is `ci`. A dependency linked into the binary is
`fix(deps)`, and one that only builds, checks or tests is `chore(deps)`. A document is `docs`, because readers
reach it from the default branch. A revert is written `revert(<scope>): <what it undoes, in fresh words>`,
with a `Refs: <sha>` footer for each reverted commit. commitlint skips git's `Revert "..."` subject, and
release-please cannot parse it. Repeating the reverted header after `revert: ` can pass the 72-character
limit.

A template's users are the repositories created from it or aligned to it. So in this template a change to a
file a clone copies, `CONTRIBUTING.md`, `docs/dev.md` and the other copied documents included, is `feat` or
`fix`, and the changelog lists what an aligning repository must copy. A pin bump stays `chore`, because each
clone's own Renovate moves its pins. A document no clone copies stays `docs`.

<!-- TODO(kickstart): delete the paragraph above; your repository is not a template. -->

The scope is optional. `.github/commit-scopes.json` lists each scope and what it covers, and commitlint
accepts no other. Omit the scope rather than invent one. A new part of the repository earns a scope in
that file, in the change that adds the part.

The header and every body line stay within 72 characters. The header's limit applies to what lands on `main`,
because github.com cuts a subject at 73. A pull request merges by squash, the one method the repository allows,
under GitHub's default text. A one-commit pull request lands its commit's subject and body. A longer one lands
its title, with each commit as a bullet in the body. Either subject gets ` (#N)` appended, so the title, or a
lone commit's subject, stays within 64 to 67 characters, as the number's digits allow. The commit hook checks
72 as written. CI's `commits` job lints every commit, and the title or a lone commit's subject with ` (#N)`
appended. A Dependabot pull request whose landed header runs past 72 fails that lint and is closed, and the
bump is taken by hand.

A pull request's title takes the type of its most user-facing commit, and `!` when any commit breaks
something users see. A squash of several commits lands the title alone, and release-please reads nothing
else, so a title without the `!` loses the break and its bump. If a merged title hid a user-facing
change, put the corrected headers between `BEGIN_COMMIT_OVERRIDE` and `END_COMMIT_OVERRIDE` in the merged
pull request's description before the release pull request merges. release-please reads them in place of
the landed message.

A body carries what the diff cannot show: what was wrong, what the change does now, and what was left
undone. A breaking change carries `!` after the type or scope and explains the break in the body. `!` marks
a break users see. A break only contributors see, such as a renamed task, carries none, because `!` cuts a
release whatever the type.

Every version heading in `CHANGELOG.md` links GitHub's compare view from the previous tag, which lists every
change in the release, hidden types included. `git log --oneline v<previous>..v<version>` lists the same.

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
  `internal/version` run `git` against a repository they create under `t.TempDir()`, because what they test
  is the git call; `internal/generate` runs the absorbed check it prints through `sh` and `git` in a work tree
  under `t.TempDir()`, because the command's exit status is what it tests; `internal/tools/version` runs the tool
  through `go run` and reads the checkout's own `git describe`, because a build-time helper has no other
  caller. [docs/dev.md#tests-that-need-a-real-thing](docs/dev.md#tests-that-need-a-real-thing) lists them.
- A test that runs `git` calls `internal/gittest.Isolate` first and names its repository with `git -C`. A hook
  exports its own repository through `GIT_DIR` and `GIT_INDEX_FILE`. Without the isolation, a test run from
  that hook would commit, tag and add remotes there.
- A helper calls `t.Helper()` first and sits above the test functions.
- Coverage thresholds live in `.testcoverage.yml`. A block that cannot be exercised carries a
  `// coverage-ignore: reason` comment, and `force-annotation-comment` fails one with no reason after the marker.

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
- `go tool task fmt` applies gofumpt and goimports, the formatters the lint row enforces. taplo owns TOML,
  and the toml row hands it every tracked `.toml` file. Prettier owns every other file Go does not.
- A dev-only affordance is gated by a build tag, never by a config flag.
  [docs/dev.md#dev-only-code](docs/dev.md#dev-only-code) has the file shape and what to check when the
  first one lands.
- An inline waiver names the rule it waives and says why. `//nolint:<linter> // reason` is the form, and
  nolintlint fails one that names no linter, gives no reason, or waives nothing. A gosec waiver names the rule,
  `// #nosec G<nnn> -- reason`, and takes no other form. gosec also reads `//gosec:disable` as that waiver, so the
  packages row refuses `gosec:disable` behind any comment marker, in any case or spacing. It refuses what neither
  linter reads as well: `//nolint:gosec`, which waives every gosec rule; a nolint naming `all` in any case or
  spacing, or naming `nolintlint`; a nolint behind extra slashes; and `//lint:ignore` or `//lint:file-ignore`.
- A ShellCheck directive in a tracked script reads `# shellcheck disable=SCnnnn[,SCnnnn] # reason` as its whole
  line, and the scripts row refuses any other. A workflow script carries none.

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
safer and the condition that removes it. A red advisory check blocks the merge, `--admin` included, because
the required checks sit in a ruleset with no bypass actor.

`audit.yml` is the scheduled report. Its one job calls the reusable `workflows` workflow daily, so
zizmor's online audits of every pinned action run with no pull request open; a red run is a report, never
a check. It carries no advisory reader, because Go's whole-tree reader is `govulncheck`, and it runs in the
gate on every push and pull request rather than on a clock. `bun.lock` holds commitlint, Prettier and the
`yaml` package commitlint's config reads, tooling that ships in nothing, and the dependency review reads what
a pull request adds to it.

### Tool integrity

Each tool the gate runs, and how its bytes are held to their source. Four tiers: provenance, a checksum
in a pinned tree, a checksum recorded by a third party, a version alone.

- actionlint and zizmor: provenance. `mise.lock` records `github-attestations`, mise verifies the
  attestation on every install, and the gate refuses a lockfile that drops the line.
- ShellCheck and taplo: a checksum in a pinned tree, `mise.lock`. taplo's checksums are the sha256 of its
  release artifacts, computed once from a download, as `mise.toml` records.
- golangci-lint, govulncheck, task, lefthook, go-test-coverage, testpair and deadcode: a checksum in a
  pinned tree, `go.sum`, checked against the checksum database on every build.
- Prettier, commitlint and `yaml`: a checksum in a pinned tree, `bun.lock`.
- Go and Bun themselves: a version alone. The pin file plus the cooldown is the control, because the
  setup actions verify no download.
- mise itself: a publisher signature, which `jdx/mise-action` checks against the release's signed
  checksums.

For every mise tool, `internal/tools/gate/pins.go` holds the pin and the lockfile's version to the tool's
release shape, `major.minor.patch` in ASCII digits, before it builds any url from them. It holds each lockfile
`url`, byte for byte, to the release asset it names for that platform. It holds each `url_api` to that
repository's asset path followed by an asset id, and it refuses a nested `platforms` table. mise falls back
to `url_api` when a download fails, and an asset id binds no version. So `mise.toml` carries a
`url_replacements` rule that sends every such fetch to a host that does not exist. `gate mise` sets the same rule in `MISE_URL_REPLACEMENTS`, which no committed
config file can lift.

`mise.toml` holds `[tools]`, `[tool_config]` and `[settings]` alone, because `[hooks]`, `[env]`, `[vars]`,
`[tasks]` or a tool's options can run a command during `mise install`. `pins.go` compares `[tool_config]` and
`[settings]` whole. A tool entry, and a lockfile entry's `options`, carries only `version` and a
`version_prefix` equal to the tag prefix. Every `mise.lock` key sits on an allow-list, each tool name under
`tools` included, and `lockfile_version` must be the format `mise lock` writes.

mise merges every config file it finds, each with its sibling lockfile, so a `mise.local.toml` beside a
`mise.local.lock` would decide what an install fetches. `pins.go` refuses every mise config or lock file
other than `mise.toml` and `mise.lock`, `.tool-versions` and `.miserc.toml` included. It refuses a link at the
root or under `.mise` or `mise`, which mise would follow. It refuses a root `.config` in any case and any form,
committed or not, because mise, the dotnet tool manifest, cosmiconfig's meta config and lefthook all read
configs from it. It refuses a root `vendor` on disk too, which go builds from unless a `-mod` flag says
otherwise, while CI's gate sets `-mod=readonly`. It refuses a root entry named `'`, a single quote: actionlint
looks its whole `-shellcheck` value up as one program path before splitting it, and the gate's value opens with a
quote, so on Linux and macOS a program under that directory would run in place of the stand-in.

`gate pins` also refuses what the programs the gate starts read from the checkout before any check of their own.
Names compare with every default-ignorable code point dropped and case folded, because Windows and macOS open a
tracked `.ENV` as `.env` and HFS+ skips the ignorable ones. Tracked, it
refuses:

- `.env`, which `Taskfile.yml` loads into every task, and the other seven env files Bun loads at startup, in
  any directory. Each belongs to one contributor, so `.gitignore` names all eight, and an untracked one passes.
  The format row and the commit hook start Bun with `--no-env-file`, so Bun loads none of the eight into
  Prettier or commitlint. Task's `.env` still reaches the format row through the environment.
- An `.npmrc` in any directory, whose registry `bun install` fetches from.
- Anything under a `node_modules` directory, which `bun install` keeps and Bun runs.
- Anything under `vendor/`, which go builds from in place of the module cache when no `-mod` flag is set.
- `patchedDependencies` in any `package.json`. `bun install` applies patched dependencies to the code of the
  packages it installs, Prettier's and commitlint's included.
- A `package.json` carrying a duplicated key at any depth. Bun keeps the first copy and Go the last, so the gate
  could not tell what Bun reads.
- A path under a `.git`, `.sl`, `.svn`, `.hg` or `.jj` directory, which Prettier's walk skips without a word.
- A workflow whose extension is anything but `.yml`, the one spelling actionlint's list and zizmor's collection
  both match.
- An inline `zizmor: ignore[...]` comment anywhere under `.github`. A waiver lives in the rules of
  `.github/zizmor.yml`, with the file it covers.
- A `replace`, `godebug` or `ignore` line in `go.mod`. An `ignore` line takes its directories out of every
  `./...` row.

Every program the gate starts that searches for its own config runs with one config named: `.prettierrc` (with
`.prettierignore` and no `.editorconfig`), `.golangci.yml`, `.taplo.toml`, `.github/zizmor.yml` and
`commitlint.config.js`, and the `fmt` task names `.golangci.yml` too. Naming the config stops every other config
read each of those tools makes, measured with a planted file of every name each one searches, so the gate
refuses none of those names. Where a tool has no named form, `gate pins` refuses every other name it searches,
tracked or on disk, so a local gate agrees with CI's. actionlint runs under an empty config of the gate's own,
and a run by hand reads `.github/actionlint.yaml` or `.yml`, so both are refused. lefthook and Task take no name
from the repository, so
`lefthook.yml` and `Taskfile.yml` are the only names of theirs allowed, a `.taskrc` included. A `go.mod` below
the root starts a module every `./...` row skips, so it is refused, and so are `go.work` and `go.work.sum`. A
`tsconfig.json` or `jsconfig.json` is refused, because Bun applies its `paths` to commitlint's
and Prettier's own imports, and nothing here is TypeScript. `internal/tools/gate/configs.go` lists every name.

On disk the check walks the work tree and skips what the tools skip: `node_modules`, which an install fills with
packages' own configs, `.claude/worktrees`, each holding a copy of every file here, and the version control
directories. A `go.mod` counts only where `./...` reaches, never under a directory whose name starts with a dot or
an underscore, or `testdata`. A `tsconfig.json`, `jsconfig.json`, `go.work` or `go.work.sum` on disk counts at the
root alone, and tracked in any directory. lefthook's local configs (`lefthook-local.yml` and `.lefthook-local`
in any of lefthook's forms) belong to one contributor: `.gitignore` names them, and the gate refuses one only when
committed, since one can replace the pins job.

No gate row holds a config file's text against a copy of its own. `.github/CODEOWNERS` names the code owner for
every path, and the `default-branch` ruleset requires the code owner's review, so a change to a config a row
reads, `.prettierignore`, `.golangci.yml`, `.testcoverage.yml` and the two allow lists among them, waits on
that review like a change to the gate's code. `startup.go` refuses any `bunfig.toml` key but
`[install] minimumReleaseAge`, since a preload runs before the program Bun starts, and the value is config
content under that review. A contributor's own untracked env file or `.npmrc` passes. git starts with `GIT_CONFIG_NOSYSTEM=1`,
`GIT_CONFIG_GLOBAL=/dev/null` and, on Windows, `SystemRoot`, and inherits nothing. No `GIT_INDEX_FILE` or `GIT_DIR` from a `.env` changes what it lists.

Every mise command a task runs goes through `go run ./internal/tools/gate mise`. It runs the `gate pins` checks
first and starts mise only when they pass, because mise renders a trusted config's templates as it loads it. It
starts mise under an environment built from an allow-list, never the inherited one. On Windows that is `SystemRoot` and
`LOCALAPPDATA`, read from the system, with `TEMP` and `TMP`. Elsewhere it is `HOME` and `TMPDIR`. The proxy
variables pass on every system. The gate sets every `MISE_` name itself: `MISE_OVERRIDE_CONFIG_FILENAMES=mise.toml`,
`MISE_OVERRIDE_TOOL_VERSIONS_FILENAMES=none`, an empty `MISE_ENV`, `MISE_AUTO_ENV=false`, the url_replacements
rule, and the checkout as the one trusted config path. A `MISE_GLOBAL_CONFIG_FILE` or
`MISE_BACKENDS_<TOOL>` from a `.env`, the shell or CI never reaches mise. mise, git, go and bun resolve from `PATH`
to an absolute path outside the checkout, and `os/exec` already refuses a match in the working directory. Outside
compares file identity rather than spelling, over the path found and over its final path with every link and
junction followed, so an 8.3 name for the checkout, or a junction or symbolic link from elsewhere into a directory
below it, does not pass. A hard link is one file under two names, so a hard link from a `PATH` directory to a
file in the checkout passes, until git replaces that file on the next checkout and breaks the link.

## Releases

release-please opens one release pull request from the commits on `main` and keeps it current. Merging
it tags the merge commit `v<version>` and creates a draft release. The same run builds the binaries and
their `SHA256SUMS` with `go tool task release`, attests every file, attaches them to the draft, and flips
it public after the approval the `release` environment holds.
[docs/install.md#check-the-download](docs/install.md#check-the-download) names both checks a user runs.

[Commit messages](#commit-messages) names the types that reach the changelog. release-please owns
`CHANGELOG.md` and `.release-please-manifest.json`.

## What never happens

- Nobody hand-edits `CHANGELOG.md` or `.release-please-manifest.json`. release-please writes both from the
  commits, and a hand edit is overwritten or, worse, shifts the next version it computes. The one exception
  is the template reset. A repository created from this template deletes `CHANGELOG.md` and writes `{}` into
  the manifest once, before its first release. The directive above `release-pr` in
  `.github/workflows/cd.yml` carries it until the clone absorbs the template. The go release type keeps no
  version file, so those two files are the whole reset.
- No `mise.lock` line is written outside `mise lock`. The lockfile is what an install fetches and compares,
  and the gate holds it to the expectations in `internal/tools/gate/pins.go`. A hand-written line is a line
  nothing verified.
- Nothing merges past a red gate. The gate is the one check between a change and a release. Two rulesets
  guard the default branch. `default-branch` holds the review rules, and `gh pr merge --admin` waives its
  approval alone. `default-branch checks` holds every required check and the code scanning verdict, with no
  bypass actor. A check that cannot report is cleared by setting that ruleset to `disabled`, merging, and
  setting it back to `active`. Each change lands in the audit log.
- No version number goes into prose. A version lives in the file that pins it, so a bump is one edit and
  no document goes stale.
- No `Makefile`. `Taskfile.yml` is the runner, and every command CI runs is a task a contributor runs.
