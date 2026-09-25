#!/bin/sh
# Refuse what reaches the gate's own build before `gate pins` can read it.
#
# CI and the push hook run this before `go run ./internal/tools/gate pins`.
# GOWORK=off and -mod=readonly leave go.mod in charge of that build: a replace
# builds a dependency of the gate from wherever it points, the checkout
# included, and a godebug line in go.mod, or a //go:debug line in the gate's
# own package, changes how the gate's process behaves. go mod edit -json
# parses go.mod and builds nothing. The gate refuses the same lines once it
# runs, which proves nothing about its own build, so this is the control.
set -eu

status=0
module=$(go mod edit -json)
if printf '%s\n' "$module" | grep -E '"(Replace|GoDebug)":' >/dev/null; then
    echo "go-mod-check.sh: go.mod carries a replace or a godebug line, which reaches the gate's own build before any check. Remove it" >&2
    status=1
fi
if grep -n '^//go:debug' internal/tools/gate/*.go >&2; then
    echo "go-mod-check.sh: the //go:debug line above sits in the gate's own package and changes how its process behaves. Remove it" >&2
    status=1
fi
exit "$status"
