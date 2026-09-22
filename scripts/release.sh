#!/bin/sh
# Build the release binaries, one build per os/arch pair in RELEASE_TARGETS,
# into dist/release/ as one flat directory. Each file carries its pair in its
# name, because publish.yml attaches every file at the root of the artifact
# and nothing below it.
#
# `task release` is the only caller. Every build goes through scripts/build.sh
# with the same inputs `task build` hands it, so a release binary and a local
# one differ in nothing but the target.
set -eu

if [ -z "${RELEASE_TARGETS:-}" ]; then
    echo "release: RELEASE_TARGETS is unset; run this through \`go tool task release\`" >&2
    exit 1
fi

out=dist/release
rm -rf "$out"
mkdir -p "$out"

for target in $RELEASE_TARGETS; do
    case "$target" in
        */*) ;;
        *)
            printf 'release: %s is not an os/arch pair\n' "$target" >&2
            exit 1
            ;;
    esac
    os=${target%/*}
    arch=${target#*/}
    stage="$out/$os-$arch/"
    GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 CC='' OUT="$stage" sh scripts/build.sh
    # go build names each binary after its package, with .exe on windows. The
    # suffix moves behind the pair so a download keeps its extension.
    for built in "$stage"*; do
        name=$(basename "$built")
        case "$name" in
            *.exe) mv "$built" "$out/${name%.exe}-$os-$arch.exe" ;;
            *) mv "$built" "$out/$name-$os-$arch" ;;
        esac
    done
    rmdir "$stage"
done

ls -1 "$out"
