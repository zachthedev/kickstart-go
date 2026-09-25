# kickstart-go

<!-- TODO(kickstart): rewrite this file for your project. Keep the sections a
user needs: what it is, how to run it, the gate, the license. -->

The Go template every `zachthedev` Go repository starts from. It ships a placeholder command, the gate
that checks it, the workflows that build and release it, and every boilerplate file the
[handbook](https://github.com/zachthedev/.github/blob/main/HANDBOOK.md) names for a `go-cli` repository.
A new repository is created from it as a GitHub template. An existing one is aligned by matching it.

**This template is deliberately overengineered.** Every gate, tool and lint rule ships enabled. If your
clone does not need a piece, trim it and record the deviation at the drift site: a comment in the file
where the change happens, beside the deviating line, so a `git diff` against this template carries the
reason next to the change. The handbook's Files table says which files are copied verbatim and which
have slots.

## Using it

1. Create the repository from this template, or clone it.
2. Find and replace `zach.tools/go/kickstart` with your module path. `git grep -l zach.tools/go/kickstart`
   lists every file.
3. Work through every template marker. `MARKERS.md` lists them and the section below says how.
4. Install and run the gate. [docs/dev.md](docs/dev.md) names what the machine needs and the first-run
   commands.
5. Trim what you do not need (below) and record each deviation at its drift site.

## The gate

```sh
go tool task check
```

That is the one command CI runs, on Linux, macOS and Windows. `go tool task --list` prints every task
with what it checks, and `go tool task check:quick` is the same gate without the test rows, which the
push hook runs. [CONTRIBUTING.md#the-gate](CONTRIBUTING.md#the-gate) says what each needs and what runs in
CI alone.

## Documentation

| Document                           | Holds                                                                                                       |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Setup, the gate, commit messages, where code goes, tests, code, dependencies, releases, what never happens. |
| [docs/dev.md](docs/dev.md)         | Prerequisites, the first run, running it, generated files, tests that need a real thing, dev-only code.     |
| [docs/install.md](docs/install.md) | Requirements, install, checking the download, upgrade, uninstall.                                           |
| [docs/usage.md](docs/usage.md)     | What `--help` does not carry: files, exit status.                                                           |
| [SECURITY.md](SECURITY.md)         | What counts as a vulnerability here, and how to report one privately.                                       |
| [AGENTS.md](AGENTS.md)             | What an agent reads first, runs to verify, and never does in a session. `CLAUDE.md` imports it.             |
| [MARKERS.md](MARKERS.md)           | Generated: every template marker left in the tree, and the command that proves the template is absorbed.    |

## Template markers

Anything a clone must change after cloning carries a `TODO` comment in the `(kickstart)` scope, and a
deliberate divergence from template content carries a `NOTE` in the same scope. `git grep -n -E '(TODO|NOTE)\(kickstart\):' -- ':(top)' ':(top,exclude)MARKERS.md'`
lists every line that carries either one, from any directory in the checkout.

`MARKERS.md` is the generated inventory: every file carrying a marker and how many of each, plus the one
command that proves the template is absorbed. `go tool task generate` keeps it current, so adding or
removing a marker without regenerating fails the `generate` row. Address each directive, delete the
comment, regenerate, and stop when the inventory's command exits 0.

**All of it comes out once absorption is done, and that is the last step.** The markers, `MARKERS.md`,
`internal/markers`, `internal/generate/markers.go` and the `MarkerTable` entry in `cmd/generate` are setup
scaffolding, not a permanent part of your project. A repository that has finished absorbing the template
has no use for a scanner that finds nothing. Delete them in one commit, with their tests and any
`.allow.deadcode` entry naming them, and remove this section and the `MARKERS.md` row from the table above.
Keep the machinery only while a `NOTE` in the `(kickstart)` scope is in use, because a diff against the
template reads better with those annotations in place.

Reserve the `NOTE` form for a heavy modification of template content. A pure addition needs no marker: it
diverges from nothing. An outright removal explains itself in the deletion diff.

## What is included

- The gate: lint, formatting, test pairing, dead code, vulnerabilities, module tidiness, generated-file
  staleness, workflow linting, the race detector, coverage and the build. `go tool task --list` prints
  the list.
- A CLI framework: [kong](https://github.com/alecthomas/kong) parses the command tree `cmd/example/main.go`
  declares. `execute` takes the streams and the exit function as parameters, so a test drives the real
  parser without touching the process.
- Build settings: `internal/buildenv` declares every setting the build injects through `-ldflags -X`, and
  `.env.template` is generated from it. Copy it to `.env`, which is gitignored, and uncomment what you need.
  `Taskfile.yml` loads that file, so a value there reaches every task.
- Dev-only code, gated by build tag. `go tool task build TAGS=dev` compiles it in.
  [docs/dev.md#dev-only-code](docs/dev.md#dev-only-code) has the file shape.
- The workflows: `ci.yml` judges a pull request, `cd.yml` releases after a merge, `codeql.yml` scans, and
  `deps.yml` runs Renovate. Each calls the reusable workflows in
  [zachthedev/.github](https://github.com/zachthedev/.github).
- Releases: release-please keeps one release pull request open. Merging it drafts the release, and the same
  run builds, attests and attaches the binaries `go tool task release` produces.
- Git hooks through lefthook: the commit message, a fast pre-commit pass, and the gate without its test
  rows before a push.

## Project structure

```text
cmd/           CLI entrypoints (wiring only; no business logic)
internal/      Pure logic packages
scripts/       The build and test commands that need more than one line
docs/          The documents the table above indexes
```

## Trim the toolchain

Each tool is optional. Removing one is a deletion diff, so a marker is not needed; a comment at the site
is, where the removal would surprise a future reader.

### Coverage (`go-test-coverage`)

Not needed if you enforce no coverage threshold. Delete `.testcoverage.yml`, drop the `coverage` task from
`Taskfile.yml` and from `check`, remove
`github.com/vladopajic/go-test-coverage/v2` from the `tool` block in `go.mod`, and run `go mod tidy`.

### Test pairing (`testpair`)

A waiver in `.allow.testpair` waits on the code owner's review, as `.github/CODEOWNERS` routes every change.
`go tool task testpair ARGS=update` rewrites the file.

Not needed if you enforce no 1:1 pairing. Delete `.allow.testpair`, drop the `testpair` task and its entry
in `check`, remove
`zach.tools/go/devtools/cmd/testpair` from the `tool` block, and run `go mod tidy`.

### Dead code (`deadcode`)

**Deleting `cmd/example/` turns this gate red before you have written a line.** `deadcode` traces
reachability from `main`, and `cmd/example` calls one path per internal package, which is the only reason
`.allow.deadcode` ships empty. Removing it reports every exported symbol in `internal/` as unreachable.
Those findings are accurate rather than a regression: the clone has not called them yet.

`go tool task deadcode ARGS=update` regenerates the list without category tags, and the next run fails
until each group carries a `# [category]` comment; almost every entry is `[public-api]` until your own
`cmd/` reaches that package. `update` rewrites the file's header too, so a note left in `.allow.deadcode`
does not survive it. A waiver there waits on the code owner's review, as `.github/CODEOWNERS` routes every
change.

Not needed for a small codebase where reachability is obvious. Delete `.allow.deadcode`, drop the
`deadcode` task and its entry in `check`, remove
`zach.tools/go/devtools/cmd/deadcode` from the `tool` block, and run `go mod tidy`.

### Generated files (`generate`)

Not needed once the marker machinery is gone and your project has no `go:generate` directive of its own.
Drop the `generate` task from `Taskfile.yml` and from `check`, the `OUTPUTS` variable the `packages` task
passes the gate, and the `generate` job from `lefthook.yml`.

### CLI framework (`kong`)

Not needed if `flag` covers it. Remove `github.com/alecthomas/kong` from `go.mod`, rewrite
`cmd/<binary>/main.go`, and run `go mod tidy`.

### Everything else

Lint, formatting, module tidiness, the vulnerability scan, the workflow linters, the hooks and the
release flow are handbook rows. Keep them. A rule that produces false positives gets an exclusion in
`.golangci.yml` with the reason beside it, not a disabled linter, and it waits on the code owner's review.

## License

<!-- TODO(kickstart): set the year in LICENSE to the year GitHub created your
repository, and name your license here if it is not MIT. -->

[MIT](LICENSE).
