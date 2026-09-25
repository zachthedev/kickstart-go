# kickstart-go, a `go-cli` template repository

<!-- TODO(kickstart): name your repository and its handbook kind on the line above. -->

[README.md](README.md) says what it is.

## Read first

Read these before changing anything, in order. They bind an agent as they bind a person.

1. [README.md](README.md)
2. [CONTRIBUTING.md](CONTRIBUTING.md), whole
3. [SECURITY.md](SECURITY.md)
4. [docs/install.md](docs/install.md)
5. [docs/usage.md](docs/usage.md)

## Verify

<!-- TODO(kickstart): keep these four lines as they are unless the clone renames a task. -->

- `go tool task check` is the gate.
- `go tool task check:quick` is the gate without the race detector and the coverage run, which is what the
  push hook runs.
- `go tool task --list` lists every task with what it checks.
- `go tool task <task>` runs one of them.

[CONTRIBUTING.md#the-gate](CONTRIBUTING.md#the-gate) says what the rows cover.

## Never

<!-- TODO(kickstart): add the rules about what an agent runs, reads or changes in a session in your
repository, each with its reason. Keep the ones below that still apply. -->

- Never run `go tool task deadcode ARGS=update` or `go tool task testpair ARGS=update` unless the user asks.
  Each rewrites its allow file wholesale: the category tags come out, the header is replaced, and a note a
  person left there is gone with no diff a reviewer reads as a deletion.
- Never run `bun add` or `bun install` with `--minimum-release-age` below the value in `bunfig.toml`, and
  never pass `--ignore-scripts` to work around a blocked install script in your own install. Installing an
  unread pull request branch passes `--ignore-scripts` on purpose ([Safety](CONTRIBUTING.md#safety)). The
  cooldown is the window in which a malicious release is pulled, and a version installed under a lowered one
  lands in `bun.lock` for every later install, where no cooldown reads it again.
- Never delete the marker machinery while the absorbed check in `MARKERS.md` exits non-zero. A directive
  still in the tree is work a person has not done yet, and the inventory is the one list of it.
- Never hand-edit `CHANGELOG.md` or `.release-please-manifest.json`, except the one template reset
  ([why, and the reset](CONTRIBUTING.md#what-never-happens)).
- Never write a `mise.lock` line outside `mise lock` ([why](CONTRIBUTING.md#what-never-happens)).
- Never merge past a red gate ([why](CONTRIBUTING.md#what-never-happens)).
- Never put a version number in prose ([why](CONTRIBUTING.md#what-never-happens)).

## Deviations

A comment beside a deviating line records a deliberate deviation. It is a decision, not a defect.

## Where the rest is

[README.md#documentation](README.md#documentation)
