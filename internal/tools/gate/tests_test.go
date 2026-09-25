package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// event is one line of go test -json output, as cmd/test2json writes it.
func event(action, pkg, test, out string) string {
	line, err := json.Marshal(testEvent{Action: action, Package: pkg, Test: test, Output: out})
	if err != nil {
		panic(err)
	}
	return string(line) + "\n"
}

// Each case is one stream of go test -json events, and the row must fail
// when no test ran, when every test it ran skipped, when one failed, and when
// a package failed or did not build, and pass a run where a test passed.
func TestTestsRow(t *testing.T) {
	passing := event("run", "ex/a", "TestA", "") +
		event("output", "ex/a", "TestA", "=== RUN   TestA\n") +
		event("pass", "ex/a", "TestA", "") +
		event("output", "ex/a", "", "PASS\n") +
		event("output", "ex/a", "", "ok  \tex/a\t0.1s\n") +
		event("pass", "ex/a", "", "")
	tests := []struct {
		name       string
		stream     string
		wantCode   int
		wantStdout []string
		wantStderr []string
	}{
		{
			name:       "a test passes",
			stream:     passing,
			wantStdout: []string{"ok  \tex/a\t0.1s\n", "go test ran 1 test in 1 package: 1 passed, 0 skipped, 0 failed"},
		},
		{
			name: "some skipped, one passed",
			stream: passing +
				event("skip", "ex/b", "TestB", "") + event("pass", "ex/b", "TestC/sub", "") + event("pass", "ex/b", "TestC", "") + event("pass", "ex/b", "", ""),
			wantStdout: []string{"go test ran 4 tests in 2 packages: 3 passed, 1 skipped, 0 failed"},
		},
		{
			name: "every test skipped, as -short or a GOFLAGS -run can make it",
			stream: event("skip", "ex/a", "TestA", "") + event("skip", "ex/a", "TestB", "") + event("pass", "ex/a", "", "") +
				event("output", "ex/b", "", "?   \tex/b\t[no test files]\n") + event("skip", "ex/b", "", ""),
			wantCode:   1,
			wantStderr: []string{"go test ran 2 tests in 2 packages: 0 passed, 2 skipped, 0 failed", "go test skipped every test it ran, so the row checked nothing"},
		},
		{
			name:       "no test ran, as a GOFLAGS -run or -skip matching nothing makes it",
			stream:     event("output", "ex/a", "", "ok  \tex/a\t0.1s [no tests to run]\n") + event("pass", "ex/a", "", ""),
			wantCode:   1,
			wantStdout: []string{"[no tests to run]"},
			wantStderr: []string{"go test ran no test, so the row checked nothing"},
		},
		{
			name:       "no package at all, as a go test that never started leaves it",
			stream:     "",
			wantCode:   1,
			wantStderr: []string{"go test reported no package, so it never ran"},
		},
		{
			name: "a test fails, and its output reaches stderr",
			stream: passing +
				event("output", "ex/b", "TestB", "    b_test.go:9: want 1, got 2\n") + event("fail", "ex/b", "TestB", "") +
				event("output", "ex/b", "", "FAIL\tex/b\t0.1s\n") + event("fail", "ex/b", "", ""),
			wantCode:   1,
			wantStdout: []string{"FAIL\tex/b\t0.1s\n"},
			wantStderr: []string{"    b_test.go:9: want 1, got 2\n", "ex/b failed", "1 test failed"},
		},
		{
			name:       "a package that did not build",
			stream:     passing + `{"ImportPath":"ex/c [ex/c.test]","Action":"build-output","Output":"c.go:3:1: syntax error\n"}` + "\n" + `{"ImportPath":"ex/c [ex/c.test]","Action":"build-fail"}` + "\n" + event("fail", "ex/c", "", ""),
			wantCode:   1,
			wantStderr: []string{"ex/c [ex/c.test] did not build", "ex/c failed"},
		},
		{
			name:       "a line that is no event",
			stream:     passing + "panic: something printed straight to stdout\n",
			wantCode:   1,
			wantStderr: []string{`go test printed a line that is not a -json event: "panic: something printed straight to stdout"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := testsRow(strings.NewReader(tt.stream), &stdout, &stderr)
			assert.Equal(t, tt.wantCode, code, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
			for _, want := range tt.wantStdout {
				assert.Contains(t, stdout.String(), want)
			}
			for _, want := range tt.wantStderr {
				assert.Contains(t, stderr.String(), want)
			}
			assert.NotContains(t, stdout.String(), "=== RUN", "the framing -json adds stays out")
		})
	}
}
