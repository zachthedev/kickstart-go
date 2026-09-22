// Command version is a build-time helper. It prints the SemVer 2.0.0
// string for the current git state to stdout. scripts/build.sh calls it
// when VERSION arrives empty and feeds the result to the linker:
//
//	-X <module>/internal/version.semver=$VERSION
//
// Not shipped in release artifacts. End-user version display happens at
// runtime via internal/version.Info() on the shipped binary.
package main

import (
	"fmt"

	"zach.tools/go/kickstart/internal/version"
)

func main() {
	fmt.Print(version.FromGit())
}
