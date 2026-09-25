# Installing

## Requirements

<!-- TODO(kickstart): name the platforms and anything the binary needs on the machine. -->

A 64-bit Linux (`amd64`, `arm64`), macOS (`arm64`) or Windows (`amd64`) machine. The binary is static and
needs no runtime. `RELEASE_TARGETS` in `Taskfile.yml` is the list the release builds.

## Install

<!-- TODO(kickstart): name the release asset for each platform and where it goes. -->

Download the asset for your platform from the newest release on the Releases page and put it on `PATH`. Each
asset is one binary named `<binary>-<os>-<arch>`, with `.exe` on Windows.

```sh
gh release download --repo zachthedev/kickstart-go --pattern 'example-linux-amd64'
chmod +x example-linux-amd64 && mv example-linux-amd64 ~/.local/bin/example
```

## Check the download

Two checks cover a release, and they prove different things. `SHA256SUMS` proves a download matches the list
the release carries. The build attestation proves where a file came from: a build for this repository, signed
by the shared release workflow `zachthedev/.github/.github/workflows/publish.yml`. GitHub records the
attestation through its attestations API rather than attaching it to the release, and one attestation covers
every binary and `SHA256SUMS`. Run both checks before the binary's first run.

Download `SHA256SUMS` from the same release as the binary, and check the one you have:

```sh
gh release download vX.Y.Z --repo zachthedev/kickstart-go --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
```

macOS runs the same check with `shasum -a 256 --check --ignore-missing SHA256SUMS`. On Windows,
`(Get-FileHash example-windows-amd64.exe -Algorithm SHA256).Hash` prints the digest to compare with that
file's line.

The list sits in the release it describes, so a replaced asset can arrive with a replaced list. The attestation
closes that gap. Verify it on `SHA256SUMS` and on the binary:

```sh
gh attestation verify SHA256SUMS --repo zachthedev/kickstart-go --signer-workflow zachthedev/.github/.github/workflows/publish.yml
gh attestation verify example-linux-amd64 --repo zachthedev/kickstart-go --signer-workflow zachthedev/.github/.github/workflows/publish.yml
```

`--repo` checks the repository the build ran for. `--signer-workflow` names the workflow that signed, which is
the shared release workflow and not this repository's `cd.yml`. Without it `gh attestation verify` refuses a
genuine file. Those two prove the source repository and the signer, not which release a file belongs to.

To bind a file to its release tag, resolve the commit the tag names and hold the attestation to it:

```sh
sha=$(gh api repos/zachthedev/kickstart-go/commits/vX.Y.Z --jq .sha)
[ -n "$sha" ] || { echo "vX.Y.Z resolved to no commit" >&2; false; } &&
  gh attestation verify example-linux-amd64 --repo zachthedev/kickstart-go \
    --signer-workflow zachthedev/.github/.github/workflows/publish.yml --source-digest "$sha"
```

The test for an empty `$sha` refuses before the verify, because `gh` reads an empty `--source-digest` as no
digest at all and would check no tag. That binds, because the shared release workflow refuses a tag that does
not name the commit its run built.
`--source-ref refs/tags/vX.Y.Z` is not the way to check it: `cd.yml` runs on a push to `main`, so the
attestation records `refs/heads/main`, and that flag refuses every genuine file.

## Upgrade

<!-- TODO(kickstart): say what a newer release does to settings and data the installed one wrote. -->

Download the newer asset and replace the binary. Read the release notes for what changed.

## Uninstall

<!-- TODO(kickstart): name what the binary writes outside its own file, so a removal is complete. -->

Delete the binary. The placeholder writes nothing else.
