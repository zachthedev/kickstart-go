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

Every asset carries a build provenance attestation signed by the release workflow. Check it before running
the binary:

```sh
gh attestation verify example-linux-amd64 --repo zachthedev/kickstart-go
```

The check passes only for a file built by this repository's `cd.yml` at a tagged commit. There is no separate
checksum file; the attestation binds the file's digest to the workflow run that produced it.

## Upgrade

<!-- TODO(kickstart): say what a newer release does to settings and data the installed one wrote. -->

Download the newer asset and replace the binary. `0.x` promises no compatibility between releases, so read
the release notes for what changed.

## Uninstall

<!-- TODO(kickstart): name what the binary writes outside its own file, so a removal is complete. -->

Delete the binary. The placeholder writes nothing else.
