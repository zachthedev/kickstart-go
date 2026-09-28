package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
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
// a package failed or did not build, and pass a run where a test passed. A
// case runs as freebsd, which declares no skip, unless it names a platform.
func TestTestsRow(t *testing.T) {
	passing := event("run", "ex/a", "TestA", "") +
		event("output", "ex/a", "TestA", "=== RUN   TestA\n") +
		event("pass", "ex/a", "TestA", "") +
		event("output", "ex/a", "", "PASS\n") +
		event("output", "ex/a", "", "ok  \tex/a\t0.1s\n") +
		event("pass", "ex/a", "", "")
	tests := []struct {
		name       string
		goos       string
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
			name: "some skipped as the platform declares, the rest passed", goos: "windows",
			stream: passing +
				event("skip", "ex/b", "TestB", "") + event("skip", "ex/b", "TestD", "") + event("skip", "ex/b", "TestE", "") +
				event("pass", "ex/b", "TestC/sub", "") + event("pass", "ex/b", "TestC", "") + event("pass", "ex/b", "", ""),
			wantStdout: []string{"go test ran 6 tests in 2 packages: 3 passed, 3 skipped, 0 failed"},
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
			goos := tt.goos
			if goos == "" {
				goos = "freebsd"
			}
			code := testsRow(strings.NewReader(tt.stream), goos, &stdout, &stderr)
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

// skips is a run of package ex/s where one test passed and n were skipped,
// TestS0 onward.
func skips(n int) string {
	var b strings.Builder
	b.WriteString(event("pass", "ex/s", "TestPass", ""))
	for i := range n {
		b.WriteString(event("skip", "ex/s", fmt.Sprintf("TestS%d", i), ""))
	}
	b.WriteString(event("pass", "ex/s", "", ""))
	return b.String()
}

// Each case is a run on one platform that skipped some tests, and the row
// must pass at the count that platform declares and fail one skip past it or
// one short of it, naming what skipped. A platform the map does not name
// declares none, and every platform CI runs has a declaration, 0 included.
func TestTestsRow_DeclaredSkips(t *testing.T) {
	type platform struct {
		goos     string
		declared int
	}
	platforms := []platform{{"linux", declaredSkips["linux"]}, {"darwin", declaredSkips["darwin"]}, {"windows", declaredSkips["windows"]}, {"freebsd", 0}}
	for _, p := range platforms {
		t.Run(p.goos+" at its declared count", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			assert.Equal(t, 0, testsRow(strings.NewReader(skips(p.declared)), p.goos, &stdout, &stderr), "stderr: %s", stderr.String())
			assert.Contains(t, stdout.String(), fmt.Sprintf("1 passed, %d skipped, 0 failed", p.declared))
		})
		t.Run(p.goos+" one skip past its declared count", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			assert.Equal(t, 1, testsRow(strings.NewReader(skips(p.declared+1)), p.goos, &stdout, &stderr))
			want := fmt.Sprintf("go test skipped %d %s, and the gate declares %d on %s, so a test that should run did not: ",
				p.declared+1, plural(p.declared+1, "test", "tests"), p.declared, p.goos)
			assert.Contains(t, stderr.String(), want)
			assert.Contains(t, stderr.String(), fmt.Sprintf(`"ex/s TestS%d"`, p.declared))
			assert.Empty(t, stdout.String())
		})
		if p.declared == 0 {
			continue
		}
		t.Run(p.goos+" one skip short of its declared count", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			assert.Equal(t, 1, testsRow(strings.NewReader(skips(p.declared-1)), p.goos, &stdout, &stderr))
			want := fmt.Sprintf("go test skipped %d %s, and the gate declares %d on %s, so a skip it declares no longer happens and the count in declaredSkips is stale. Skipped: ",
				p.declared-1, plural(p.declared-1, "test", "tests"), p.declared, p.goos)
			assert.Contains(t, stderr.String(), want)
			assert.Empty(t, stdout.String())
		})
	}
	t.Run("the platforms CI runs each declare a count, zero included", func(t *testing.T) {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			count, ok := declaredSkips[goos]
			assert.True(t, ok, "declaredSkips names %s, a platform CI runs, even when it skips nothing", goos)
			assert.GreaterOrEqual(t, count, 0, "%s declares a count of skips", goos)
		}
	})
	t.Run("a stale count with nothing skipped says so", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		assert.Equal(t, 1, testsRow(strings.NewReader(skips(0)), "windows", &stdout, &stderr))
		assert.Contains(t, stderr.String(), "is stale. Skipped: none")
	})
	t.Run("a long list names the first skipped tests and counts the rest", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		assert.Equal(t, 1, testsRow(strings.NewReader(skips(skippedShown+2)), "freebsd", &stdout, &stderr))
		assert.Contains(t, stderr.String(), `"ex/s TestS0", "ex/s TestS1"`)
		assert.Contains(t, stderr.String(), fmt.Sprintf(`"ex/s TestS%d" and 2 more`, skippedShown-1))
		assert.NotContains(t, stderr.String(), fmt.Sprintf(`"ex/s TestS%d"`, skippedShown))
	})
	t.Run("a subtest's name reaches the finding quoted", func(t *testing.T) {
		stream := event("pass", "ex/s", "TestPass", "") + event("skip", "ex/s", "TestRun/a\"quoted\"_name", "") + event("pass", "ex/s", "", "")
		var stdout, stderr bytes.Buffer
		assert.Equal(t, 1, testsRow(strings.NewReader(stream), "freebsd", &stdout, &stderr))
		assert.Contains(t, stderr.String(), strconv.Quote(`ex/s TestRun/a"quoted"_name`))
	})
}
