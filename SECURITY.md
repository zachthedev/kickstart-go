# Security

<!-- TODO(kickstart): rewrite this paragraph for what your project holds and touches. -->

This repository is a template. It ships a placeholder command, the gate that checks it, and the workflows
that build and release it. What a clone holds is the clone's to describe here: the data its binary reads,
the credentials it stores, the hosts it talks to. This file says what counts as a vulnerability in that
arrangement, and how to report one without publishing it first.

## Reporting

<!-- TODO(kickstart): point the advisory link at your repository. -->

Open a private advisory from the repository's Security tab:
<https://github.com/zachthedev/kickstart-go/security/advisories/new>. Where the Security tab offers no
private report, email [hey@zachthe.dev](mailto:hey@zachthe.dev).

Never open a public issue for a vulnerability. Everything else belongs in the issue tracker.

A report is most useful with the release or commit it was read at, the command or input that reaches the
hole, and what an attacker gains. Keep a proof of concept inert: output that shows what could be reached
proves it as well as an action that reaches it.

## What is supported

The newest release on the Releases page. A fix lands on `main` and ships in the next release. Older
releases get no patch, so upgrading is the remedy.

## In scope

- The shipped binary: an input that makes it read, write or run something [docs/usage.md](docs/usage.md)
  does not say it will.
- The build and release path: a workflow, a script under `scripts/` or a pin that lets a change reach a
  release without the checks this repository runs.
- The gate, where it runs on a contributor's machine: a check that reads a file it must not, or a tool
  resolved from somewhere other than its pin.

## Out of scope

- GitHub itself: Actions, Dependabot, code scanning and the rulesets. Report those to GitHub.
- Go, the modules `go.mod` names, and the tools `mise.toml` and `package.json` pin. Report those
  upstream; a pin bump lands here once a fix ships.
- A clone of this template with settings the handbook does not describe. Its own `SECURITY.md` covers it.

## After a report

One person maintains this repository, and a first reply takes up to a week. There is no bounty. A report
gets an acknowledgment, a fix on `main`, and a credit in the advisory unless you ask to stay anonymous.
