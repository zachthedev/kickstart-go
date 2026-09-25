#!/bin/sh
# Build the project's binaries into the directory OUT names.
#
# `task build` and scripts/release.sh are the only callers. It owns every default; this script refuses
# a value it was not given rather than substituting one, so a task and a
# bare invocation cannot build different things.
#
# VERSION, REPO_OWNER and REPO_NAME are the exception, and they are discovered
# rather than defaulted: reading them from git is logic, and it lives with the
# script that needs it.
set -eu

require() {
    if [ -z "${2:-}" ]; then
        printf "build: %s is unset; run this through \`go tool task build\`, which owns every default\n" "$1" >&2
        exit 1
    fi
}

require CGO_ENABLED "${CGO_ENABLED:-}"
require BUILD_TARGET "${BUILD_TARGET:-}"
require OUT "${OUT:-}"

require VERSION_PKG "${VERSION_PKG:-}"
require REMOTE_PKG "${REMOTE_PKG:-}"

# A failed version read is a failed build: a binary stamped with an empty
# version would report nothing to a user and nothing would have said so.
if [ -z "${VERSION:-}" ]; then
    VERSION=$(go run ./internal/tools/version)
fi
# The repo expression takes everything after the owner and then drops one
# trailing .git, which is what internal/remote's githubRemoteRe does with a
# non-greedy group and an optional suffix. A class stopping at the first dot
# stamps "vue" for a repository named "vue.js", and the two of them then name
# different repositories for one origin.
if [ -z "${REPO_OWNER:-}" ] || [ -z "${REPO_NAME:-}" ]; then
    origin=$(git remote get-url origin 2>/dev/null || true)
    : "${REPO_OWNER:=$(printf '%s' "$origin" | sed -n 's|.*github\.com[:/]\([^/]*\)/.*|\1|p')}"
    : "${REPO_NAME:=$(printf '%s' "$origin" |
        sed -n 's|.*github\.com[:/][^/]*/\(.*\)|\1|p' | sed 's|\.git$||')}"
fi

# Every value below lands unquoted in -ldflags, where the linker splits on any
# whitespace and a later -X wins by last write. One tab inside a git config
# value is therefore enough to append a -X of its own and overwrite any symbol
# in the binary, so each value is refused outright rather than trimmed: a
# trim at the first space leaves tab and newline through.
reject_unsafe() {
    case "$2" in
        *[!A-Za-z0-9._/+-]*)
            printf 'build: %s carries a character that cannot go in -ldflags: %s\n' "$1" "$2" >&2
            exit 1
            ;;
    esac
}

reject_unsafe VERSION "$VERSION"
reject_unsafe REPO_OWNER "$REPO_OWNER"
reject_unsafe REPO_NAME "$REPO_NAME"
reject_unsafe VERSION_PKG "$VERSION_PKG"
reject_unsafe REMOTE_PKG "$REMOTE_PKG"

# reject_not_a_name refuses what internal/remote's identRe refuses: a first
# character outside [A-Za-z0-9], or any later character outside [A-Za-z0-9._-].
# reject_unsafe is wider than that on both counts, so a name it passes can
# still be one Load will not take.
#
# An empty value fails the first test, which is what turns a build carrying one
# name and not the other into a failed build. Load refuses such a binary for
# good, at run time, on a user's machine, and this is the one moment the build
# itself can say so.
reject_not_a_name() {
    name_ok=yes
    case "$2" in
        [A-Za-z0-9]*) ;;
        *) name_ok=no ;;
    esac
    case "$2" in
        *[!A-Za-z0-9._-]*) name_ok=no ;;
    esac
    if [ "$name_ok" = no ]; then
        printf 'build: %s is not a GitHub name, so the binary would refuse itself: %s\n' "$1" "$2" >&2
        exit 1
    fi
}

# A checkout with no GitHub origin stamps neither name and builds. Anything
# else has to satisfy the rule the binary will apply to itself.
if [ -n "$REPO_OWNER" ] || [ -n "$REPO_NAME" ]; then
    reject_not_a_name REPO_OWNER "$REPO_OWNER"
    reject_not_a_name REPO_NAME "$REPO_NAME"
fi

# BUILD_TARGET reaches the go build argv word-split, so it cannot go through
# reject_unsafe whole: that charset forbids the space separating two targets.
# Each word is checked on its own, and a word opening with a dash is refused
# outright. go build takes the last -o it is given, and this script passes its
# own -o "$OUT" ahead of the target, so a smuggled second one silently writes
# the binaries somewhere else.
#
# TAGS needs none of this. It expands inside its own quotes and reaches -tags
# as a single argument whatever it holds, so it can contribute no separate word
# for go build to read as a flag. A charset check there would refuse an
# ordinary comma-separated tag list.
# shellcheck disable=SC2086 # deliberate: the split is what is being checked.
for target in $BUILD_TARGET; do
    case "$target" in
        -*)
            printf 'build: BUILD_TARGET word %s opens with a dash, which go build reads as a flag\n' "$target" >&2
            exit 1
            ;;
    esac
    reject_unsafe BUILD_TARGET "$target"
done

ldflags="-s -w"
ldflags="$ldflags -X ${VERSION_PKG}.semver=${VERSION}"
ldflags="$ldflags -X ${REMOTE_PKG}.ldOwner=${REPO_OWNER}"
ldflags="$ldflags -X ${REMOTE_PKG}.ldRepo=${REPO_NAME}"

set -x
# shellcheck disable=SC2086 # TAGS and BUILD_TARGET are deliberately split.
CGO_ENABLED="$CGO_ENABLED" go build -trimpath ${TAGS:+-tags "$TAGS"} \
    -ldflags "$ldflags" -o "$OUT" $BUILD_TARGET
