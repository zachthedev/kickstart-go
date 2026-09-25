#!/bin/sh
# Run the suite under the race detector.
#
# `task test` is the only caller, and it owns TEST_TARGET and TEST_TIMEOUT.
# This script refuses an unset value rather than substituting one, so the
# suite a task runs and the suite a bare invocation runs cannot differ.
#
# The detector needs cgo. Without a C compiler this prints that the gate did
# not run and exits 0 on a contributor's machine, so read the skip line rather
# than the exit code. Under CI=true, which GitHub sets on every runner, the
# skip is a failure: a runner image that lost its compiler would otherwise
# report a green race leg that never ran. The test is for that exact value, so
# a contributor exporting CI=false or CI=0 keeps the skip.
set -eu

if [ -z "${TEST_TARGET:-}" ]; then
    echo "test-race: TEST_TARGET is unset; run this through \`go tool task test\`" >&2
    exit 1
fi
if [ -z "${TEST_TIMEOUT:-}" ]; then
    echo "test-race: TEST_TIMEOUT is unset; run this through \`go tool task test\`" >&2
    exit 1
fi

# CC selects among the compilers on PATH; `go env CC` reports the toolchain's
# default otherwise. It does not substitute for PATH: a gcc driver shells out
# to cc1, as and ld beside it and resolves those by bare name, so an absolute
# CC pointing outside PATH compiles nothing.
cc=$(go env CC)
if ! command -v "$cc" >/dev/null 2>&1; then
    echo "SKIP: -race needs cgo and no C compiler ($cc) is on PATH."
    echo "      This gate did not run. CI runs it."
    echo "      To run it here, put a compiler's directory on PATH."
    if [ "${CI:-}" = "true" ]; then
        echo "FAIL: CI=true, and the race detector must run there." >&2
        exit 1
    fi
    exit 0
fi

# go test's -json events go through the gate's tests row, which fails unless a
# test ran and passed and none failed. The row reads every failure from the
# events, so it needs no pipefail, which POSIX sh lacks.
set -x
# shellcheck disable=SC2086 # TAGS and TEST_TARGET are deliberately split.
CGO_ENABLED=1 go test -json -race -count=1 -timeout "$TEST_TIMEOUT" ${TAGS:+-tags "$TAGS"} $TEST_TARGET |
    go run ./internal/tools/gate tests
