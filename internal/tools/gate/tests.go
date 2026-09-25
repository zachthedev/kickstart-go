package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// testEvent is one line of `go test -json`, as cmd/test2json writes it. A
// build failure arrives as build-output and build-fail, naming its package in
// ImportPath.
type testEvent struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Output     string
}

// testCounts is what one go test run reported.
type testCounts struct {
	passed   int
	skipped  int
	failed   int
	packages int
}

// ///////////////////////////////////////////////
// The row
// ///////////////////////////////////////////////

// testsRow reads `go test -json` on events, as a test row pipes it in, and
// fails unless at least one test ran and passed and nothing failed. go test
// exits 0 when a GOFLAGS -run, -skip or -short from the shell skips every
// test, and when it matched none, so the exit code proves nothing alone. It
// prints each package's result line as it arrives, and the output of each test
// that failed, then one summary line. A line that is not an event fails the
// row, since the gate cannot tell what it hid.
func testsRow(events io.Reader, stdout, stderr io.Writer) int {
	var counts testCounts
	var findings []string
	held := map[string][]string{}
	lines := bufio.NewScanner(events)
	lines.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for lines.Scan() {
		var event testEvent
		if err := json.Unmarshal(lines.Bytes(), &event); err != nil {
			findings = append(findings, fmt.Sprintf("go test printed a line that is not a -json event: %s", excerpt(lines.Bytes())))
			continue
		}
		key := event.Package + " " + event.Test
		switch event.Action {
		case "output", "build-output":
			if event.Test != "" {
				held[key] = append(held[key], event.Output)
			} else if packageLine(event.Output) {
				_, _ = io.WriteString(stdout, event.Output)
			}
		case "pass":
			if event.Test != "" {
				counts.passed++
			} else {
				counts.packages++
			}
			delete(held, key)
		case "skip":
			if event.Test != "" {
				counts.skipped++
			} else {
				counts.packages++
			}
			delete(held, key)
		case "fail":
			if event.Test != "" {
				counts.failed++
				_, _ = io.WriteString(stderr, strings.Join(held[key], ""))
			} else {
				counts.packages++
				findings = append(findings, fmt.Sprintf("%s failed", event.Package))
			}
			delete(held, key)
		case "build-fail":
			findings = append(findings, fmt.Sprintf("%s did not build", event.ImportPath))
		}
	}
	if err := lines.Err(); err != nil {
		findings = append(findings, fmt.Sprintf("reading go test's events: %v", err))
	}
	summary := counts.String()
	switch {
	case counts.packages == 0:
		findings = append(findings, "go test reported no package, so it never ran. Its own error is above")
	case counts.passed+counts.skipped+counts.failed == 0:
		findings = append(findings, "go test ran no test, so the row checked nothing. A -run or -skip in GOFLAGS does this")
	case counts.passed == 0 && counts.failed == 0:
		findings = append(findings, "go test skipped every test it ran, so the row checked nothing. A -short or -run in GOFLAGS, or an environment the tests read, does this")
	}
	if counts.failed > 0 {
		findings = append(findings, fmt.Sprintf("%d %s failed", counts.failed, plural(counts.failed, "test", "tests")))
	}
	if len(findings) > 0 {
		fmt.Fprintln(stderr, summary)
		fmt.Fprintln(stderr, strings.Join(findings, "\n"))
		return 1
	}
	fmt.Fprintln(stdout, summary)
	return 0
}

// packageLine reports whether a package-level output line is one go test
// prints without -json: a result line, a build error or a panic, and not the
// framing -json adds.
func packageLine(line string) bool {
	return !strings.HasPrefix(line, "=== ") && !strings.HasPrefix(line, "PASS\n") && strings.TrimSpace(line) != ""
}

// String is the row's summary: how many tests ran in how many packages, and
// how each ended.
func (counts testCounts) String() string {
	ran := counts.passed + counts.skipped + counts.failed
	return fmt.Sprintf("go test ran %d %s in %d %s: %d passed, %d skipped, %d failed",
		ran, plural(ran, "test", "tests"), counts.packages, plural(counts.packages, "package", "packages"),
		counts.passed, counts.skipped, counts.failed)
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
