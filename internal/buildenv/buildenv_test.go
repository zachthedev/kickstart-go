package buildenv

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildScript is the path, from the project root, of the script that owns
// the linker flags. The build task is its only caller.
var buildScript = filepath.Join("scripts", "build.sh")

// injected matches the shell variable inside each `-X pkg.sym=${NAME}` the
// build script assembles into its linker flags.
var injected = regexp.MustCompile(`-X \$\{[A-Z0-9_]+\}\.[A-Za-z]+=\$\{([A-Z0-9_]+)\}`)

// repoFile reads a file from the project root. These tests read the build
// script and .gitignore on purpose: the script is this package's consumer,
// and a schema that cannot be checked against its consumer is the
// documentation-drift problem one layer up.
func repoFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	require.NoError(t, err)
	return string(data)
}

// ldflagsBlock returns the lines of the build script that assemble -ldflags.
func ldflagsBlock(t *testing.T, script string) string {
	t.Helper()
	var b strings.Builder
	for line := range strings.SplitSeq(script, "\n") {
		if strings.Contains(line, "ldflags=") && strings.Contains(line, "-X ") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	require.NotEmpty(t, b.String(), "the build script assembles no -ldflags")
	return b.String()
}

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

func TestVariables_Complete(t *testing.T) {
	for _, v := range Variables() {
		if !assert.NotEmpty(t, v.Name, "a variable has no Name") {
			continue
		}
		assert.NotEmpty(t, v.Purpose, "%s has no Purpose", v.Name)
		assert.NotEmpty(t, v.Absent, "%s does not say what an absent build does", v.Name)
		assert.NotEmpty(t, v.Example, "%s has no Example", v.Name)
	}
}

func TestVariables_NamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range Names() {
		assert.False(t, seen[name], "%s is declared twice", name)
		seen[name] = true
	}
}

func TestNames(t *testing.T) {
	vars := Variables()
	names := Names()
	require.Len(t, names, len(vars))
	for i, v := range vars {
		assert.Equal(t, v.Name, names[i])
	}
}

// ///////////////////////////////////////////////
// Drift against the build script
// ///////////////////////////////////////////////

// Every variable the linker injects must be declared here, or the generated
// template documents a build an operator cannot reproduce. Names ending in
// _PKG are excluded by shape rather than by list: they hold the import path
// a value is injected into, which is a fact about the code and not a setting
// anyone chooses.
func TestVariables_CoverEveryInjectedName(t *testing.T) {
	block := ldflagsBlock(t, repoFile(t, buildScript))
	declared := Names()

	matches := injected.FindAllStringSubmatch(block, -1)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		name := m[1]
		if strings.HasSuffix(name, "_PKG") {
			continue
		}
		assert.True(t, slices.Contains(declared, name),
			"the build script injects %s but nothing in buildenv declares it", name)
	}
}

// A value reaches -ldflags unquoted, where the linker splits on any
// whitespace and a later -X wins by last write, so a value carrying one can
// append a flag that overwrites any symbol in the binary. Every injected
// setting is checked, the package paths included: they are interpolated into
// the same string as the rest.
func TestVariables_InjectedValuesAreGuarded(t *testing.T) {
	script := repoFile(t, buildScript)
	for _, m := range injected.FindAllStringSubmatch(ldflagsBlock(t, script), -1) {
		name := m[1]
		assert.Contains(t, script, `reject_unsafe `+name+` "$`+name+`"`,
			"%s reaches -ldflags with nothing checking it", name)
	}
}

// Trimming at the first word is not a guard. The shell's %% operator matches
// a literal space, and the linker splits on tab and newline as readily.
func TestVariables_GuardIsNotAFirstWordTrim(t *testing.T) {
	script := repoFile(t, buildScript)
	assert.NotContains(t, script, `%% *}`,
		"a first-word trim leaves tab and newline through to -ldflags")
}

// ///////////////////////////////////////////////
// Drift against .gitignore
// ///////////////////////////////////////////////

// A real value lands in .env and committing one publishes it. A rule that
// ignored the template alongside it would leave contributors with no
// documentation at all, so both lines have to hold.
func TestVariables_IgnoreRulesHold(t *testing.T) {
	ignore := repoFile(t, ".gitignore")
	for _, want := range []string{"\n.env\n", "\n!.env.template\n"} {
		assert.Contains(t, ignore, want)
	}
	// Git resolves the last matching pattern, so the negation has to come
	// after the rule it re-includes.
	assert.Greater(t, strings.Index(ignore, "\n!.env.template\n"), strings.Index(ignore, "\n.env\n"),
		"!.env.template sits above the .env it re-includes, where it does nothing")
}

// ///////////////////////////////////////////////
// Drift against the generated template
// ///////////////////////////////////////////////

func TestVariables_TemplateIsRegistered(t *testing.T) {
	main := repoFile(t, filepath.Join("cmd", "generate", "main.go"))
	assert.Contains(t, main, `".env.template"`,
		"cmd/generate does not register .env.template, so the generate task cannot keep it current")
}

// The committed template documents settings rather than supplying them, so
// a copy of it taken whole to .env must configure nothing.
func TestVariables_TemplateHasNoLiveAssignments(t *testing.T) {
	tmpl := repoFile(t, ".env.template")
	for line := range strings.SplitSeq(tmpl, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		assert.NotContains(t, trimmed, "=")
	}
}

func TestVariables_TemplateDocumentsEveryName(t *testing.T) {
	tmpl := repoFile(t, ".env.template")
	for _, name := range Names() {
		assert.Contains(t, tmpl, "#"+name+"=")
	}
}
