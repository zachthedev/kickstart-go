package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// checkstyleReport is ShellCheck's --format=checkstyle output: one file
// element for every file it checked, findings or none, each holding its
// findings.
type checkstyleReport struct {
	Files []struct {
		Name   string `xml:"name,attr"`
		Errors []struct {
			Line     int    `xml:"line,attr"`
			Column   int    `xml:"column,attr"`
			Severity string `xml:"severity,attr"`
			Message  string `xml:"message,attr"`
			Source   string `xml:"source,attr"`
		} `xml:"error"`
	} `xml:"file"`
}

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// scriptDirective is the one form a ShellCheck directive takes in a tracked
// script, as its whole line: a disable naming each code, and the reason after
// a second #. ShellCheck reads several keys in one directive and requires
// neither a code nor a reason, so any other line shellCheckDirective matches
// is refused.
var scriptDirective = regexp.MustCompile(`^[ \t]*# shellcheck disable=SC[0-9]{4}(,SC[0-9]{4})* # \S.*$`)

// ///////////////////////////////////////////////
// The row
// ///////////////////////////////////////////////

// scriptsFindings runs ShellCheck over every tracked .sh file, named on its
// command line with --norc, so no .shellcheckrc applies, and the runner
// withholds SHELLCHECK_OPTS. ShellCheck prints nothing for a clean file in its
// default form, so the row reads the checkstyle form, which names every file
// it checked, and fails unless it names each one handed over. Every line
// carrying a ShellCheck directive must take scriptDirective's form.
func scriptsFindings(run commandRunner, shellcheck, root string, tracked []string) (rowResult, error) {
	var handed []string
	for _, name := range tracked {
		if fold(path.Ext(name)) == fold(".sh") {
			handed = append(handed, name)
		}
	}
	if len(handed) == 0 {
		return rowResult{findings: []string{"git tracks no .sh file, so the scripts row has nothing to hand ShellCheck and checks nothing"}}, nil
	}
	directives, err := directiveFindings(root, handed)
	if err != nil {
		return rowResult{}, err
	}
	args := append([]string{"--norc", "--format=checkstyle", "--"}, handed...)
	out, err := run(shellcheck, nil, args...)
	if err != nil {
		return rowResult{}, fmt.Errorf("running %s: %w", shellcheck, err)
	}
	result := rowResult{
		summary:  fmt.Sprintf("shellcheck checked %s: %s", countFiles(len(handed), "script"), strings.Join(handed, ", ")),
		findings: directives,
		relay:    out.stderr,
	}
	var report checkstyleReport
	if err := xml.Unmarshal(withoutEscapes(out.stdout), &report); err != nil {
		result.findings = append(result.findings, fmt.Sprintf("ShellCheck's checkstyle report cannot be read back: %v", err))
		result.relay = append(result.relay, out.stdout...)
	}
	var checked []string
	for _, file := range report.Files {
		checked = append(checked, file.Name)
		for _, finding := range file.Errors {
			result.relay = fmt.Appendf(result.relay, "%s:%d:%d: %s %s: %s\n", file.Name, finding.Line, finding.Column,
				finding.Severity, strings.TrimPrefix(finding.Source, "ShellCheck."), finding.Message)
		}
	}
	for _, name := range handed {
		if !slices.Contains(checked, name) {
			result.findings = append(result.findings, fmt.Sprintf("ShellCheck did not report checking %s, which the scripts row handed it", name))
		}
	}
	if out.code != 0 {
		result.findings = append(result.findings, fmt.Sprintf("shellcheck exited %d", out.code))
	}
	return result, nil
}

// directiveFindings refuses every line of the scripts named that carries a
// ShellCheck directive in any form but scriptDirective's. A tracked file
// missing from the work tree passes, and ShellCheck's own list check reports
// it.
func directiveFindings(root string, scripts []string) ([]string, error) {
	var found []string
	for _, name := range scripts {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if shellCheckDirective.MatchString(line) && !scriptDirective.MatchString(line) {
				found = append(found, fmt.Sprintf("%s:%d carries %q, and a ShellCheck directive in a script takes one form, as its whole line: # shellcheck disable=SCnnnn[,SCnnnn] # reason",
					name, i+1, strings.TrimSpace(line)))
			}
		}
	}
	return found, nil
}
