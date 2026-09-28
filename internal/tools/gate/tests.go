package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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
// Constants
// ///////////////////////////////////////////////

// skippedShown is how many skipped tests a finding names before it counts the
// rest.
const skippedShown = 10

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// declaredSkips is how many tests a test row skips on each platform, keyed by
// GOOS, and the row fails on any other count. A platform it does not name
// declares none. Each count is what that platform's CI runner skips: the cases
// for a behavior another platform alone has, and the cases that step aside
// where the runner keeps gh beside git. A skip past the count is a test that
// did not run, and a count short of it is a declaration whose gap closed.
var declaredSkips = map[string]int{"darwin": 4, "linux": 5, "windows": 3}

// ///////////////////////////////////////////////
// The row
// ///////////////////////////////////////////////

// testsRow reads `go test -json` on events, as a test row pipes it in, and
// fails unless at least one test ran and passed, nothing failed, and as many
// tests skipped as declaredSkips declares for goos. go test exits 0 when a
// GOFLAGS -run, -skip or -short from the shell skips every test, and when it
// matched none, so the exit code proves nothing alone. It prints each
// package's result line as it arrives, and the output of each test that
// failed, then one summary line. A line that is not an event fails the row,
// since the gate cannot tell what it hid.
func testsRow(events io.Reader, goos string, stdout, stderr io.Writer) int {
	var counts testCounts
	var findings, skipped []string
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
				skipped = append(skipped, strconv.Quote(event.Package+" "+event.Test))
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
	findings = append(findings, counts.findings(skipped, goos)...)
	if len(findings) > 0 {
		fmt.Fprintln(stderr, summary)
		fmt.Fprintln(stderr, strings.Join(findings, "\n"))
		return 1
	}
	fmt.Fprintln(stdout, summary)
	return 0
}

// findings judges a finished run by its counts: no package, no test, every
// test skipped, a skip count other than declaredSkips declares for goos, or a
// test failed. skipped names each skipped test, quoted.
func (counts testCounts) findings(skipped []string, goos string) []string {
	var found []string
	switch {
	case counts.packages == 0:
		found = append(found, "go test reported no package, so it never ran. Its own error is above")
	case counts.passed+counts.skipped+counts.failed == 0:
		found = append(found, "go test ran no test, so the row checked nothing. A -run or -skip in GOFLAGS does this")
	case counts.passed == 0 && counts.failed == 0:
		found = append(found, "go test skipped every test it ran, so the row checked nothing. A -short or -run in GOFLAGS, or an environment the tests read, does this")
	}
	declared := declaredSkips[goos]
	switch ran := fmt.Sprintf("go test skipped %d %s, and the gate declares %d on %s", counts.skipped, plural(counts.skipped, "test", "tests"), declared, goos); {
	case counts.skipped > declared:
		found = append(found, fmt.Sprintf("%s, so a test that should run did not: %s", ran, named(skipped)))
	case counts.skipped < declared:
		found = append(found, fmt.Sprintf("%s, so a skip it declares no longer happens and the count in declaredSkips is stale. Skipped: %s", ran, named(skipped)))
	}
	if counts.failed > 0 {
		found = append(found, fmt.Sprintf("%d %s failed", counts.failed, plural(counts.failed, "test", "tests")))
	}
	return found
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

// named lists the first skippedShown quoted names and counts the rest, or
// says there are none.
func named(quoted []string) string {
	if len(quoted) == 0 {
		return "none"
	}
	shown := quoted[:min(len(quoted), skippedShown)]
	if extra := len(quoted) - len(shown); extra > 0 {
		return fmt.Sprintf("%s and %d more", strings.Join(shown, ", "), extra)
	}
	return strings.Join(shown, ", ")
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
