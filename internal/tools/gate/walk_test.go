package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prettierArgs is the command line the format row hands bun: bunx under the
// pinned Bun, then Prettier's own arguments.
var prettierArgs = []string{
	"x", "--bun", "--no-install", "prettier", "--check", "--debug-check",
	"--no-editorconfig", "--config", ".prettierrc", "--ignore-path", ".prettierignore", ".",
}

// installPrettier writes the command bun install puts in node_modules/.bin for
// Prettier on this platform under root.
func installPrettier(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(root, "node_modules", ".bin")
	require.NoError(t, os.MkdirAll(bin, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bin, programName("prettier")), nil, 0o600))
}

// cutArgs splits args around the first sep, as a program reading "--" does.
func cutArgs(args []string, sep string) (before, after []string, found bool) {
	i := slices.Index(args, sep)
	if i < 0 {
		return args, nil, false
	}
	return args[:i], args[i+1:], true
}

// writeFiles creates each named file under root.
func writeFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		target := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, []byte("x = 1\n"), 0o600))
	}
}

// taploLog is taplo 0.10.0's found files line over the named files under
// root, spelled as taplo spells them.
func taploLog(root string, names ...string) string {
	var quoted []string
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(filepath.ToSlash(filepath.Join(root, filepath.FromSlash(name)))))
	}
	return ` INFO taplo:format_files:load_config: found configuration file path=".taplo.toml"` + "\n" +
		` INFO taplo:format_files:collect_files: found files total=` + strconv.Itoa(len(names)) +
		` excluded=0 files=[` + strings.Join(quoted, ", ") + `] cwd="."` + "\n"
}

// fakeTaploRunner plays taplo: it checks the command line and environment the
// row builds and answers with out. handed receives the files after --.
func fakeTaploRunner(t *testing.T, out output, handed *[]string) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "taplo-path", name)
		assert.Equal(t, []string{"RUST_LOG=info"}, env, "an inherited RUST_LOG cannot silence the found files line")
		before, after, found := cutArgs(args, "--")
		require.True(t, found, "the files follow --")
		assert.Equal(t, []string{"fmt", "--check", "--colors", "never", "--config", ".taplo.toml"}, before)
		*handed = after
		return out, nil
	}
}

func TestTomlFindings(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, ".taplo.toml", "sub/b.toml", "README.md", "stray.toml")
	tracked := []string{".taplo.toml", "README.md", "sub/b.toml", "sub/Deleted.TOML"}

	tests := []struct {
		name        string
		tracked     []string
		out         output
		wantHanded  []string
		wantSummary string
		wantIn      []string
		wantRelay   bool
	}{
		{
			name: "taplo lists every file the row handed it", tracked: []string{".taplo.toml", "README.md", "sub/b.toml"},
			out:        output{stderr: []byte(taploLog(root, ".taplo.toml", "sub/b.toml"))},
			wantHanded: []string{".taplo.toml", "sub/b.toml"}, wantSummary: "taplo checked 2 TOML files: .taplo.toml, sub/b.toml",
		},
		{
			name: "a found files line carrying escape sequences, as a runner that forces color prints it", tracked: []string{".taplo.toml", "README.md", "sub/b.toml"},
			out: output{stderr: []byte(strings.NewReplacer(" INFO ", " \x1b[32mINFO\x1b[0m ", "files=[", "\x1b[3mfiles\x1b[0m\x1b[2m=\x1b[0m[").
				Replace(taploLog(root, ".taplo.toml", "sub/b.toml")))},
			wantHanded: []string{".taplo.toml", "sub/b.toml"}, wantSummary: "taplo checked 2 TOML files: .taplo.toml, sub/b.toml",
		},
		{
			name: "a tracked file the work tree deleted", tracked: tracked,
			out:        output{stderr: []byte(taploLog(root, ".taplo.toml", "sub/b.toml"))},
			wantHanded: []string{".taplo.toml", "sub/b.toml", "sub/Deleted.TOML"},
			wantIn:     []string{"taplo did not check sub/Deleted.TOML, which the toml row handed it"}, wantRelay: true,
		},
		{
			name: "a config that excludes every file", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte(` INFO taplo:format_files:load_config: found configuration file path=".taplo.toml"` + "\n")},
			wantIn: []string{"taplo printed no found files line, so it checked none of the 1 TOML files the row handed it"}, wantRelay: true,
		},
		{
			name: "a file taplo checked that the row did not hand it", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte(taploLog(root, ".taplo.toml", "stray.toml"))},
			wantIn: []string{`stray.toml", which the toml row did not hand it`}, wantRelay: true,
		},
		{
			name: "a listed file the gate cannot open", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte(taploLog(root, ".taplo.toml", "gone.toml"))},
			wantIn: []string{"which the gate cannot open"}, wantRelay: true,
		},
		{
			name: "a misformatted file", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte(taploLog(root, ".taplo.toml") + "ERROR operation failed error=some files were not properly formatted\n"), code: 1},
			wantIn: []string{"taplo exited 1"}, wantRelay: true,
		},
		{
			name: "a list this check cannot read back", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte(` INFO found files total=1 excluded=0 files=["a\tb"]` + "\n")},
			wantIn: []string{"taplo's found files line cannot be read back"}, wantRelay: true,
		},
		{
			name: "taplo that stops before listing a file, reported as its exit alone", tracked: []string{".taplo.toml"},
			out:    output{stderr: []byte("ERROR invalid configuration\n"), code: 1},
			wantIn: []string{"taplo exited 1"}, wantRelay: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handed []string
			result, err := tomlFindings(fakeTaploRunner(t, tt.out, &handed), "taplo-path", root, tt.tracked)
			require.NoError(t, err)
			if tt.wantHanded != nil {
				assert.Equal(t, tt.wantHanded, handed)
			}
			if tt.wantSummary != "" {
				assert.Equal(t, tt.wantSummary, result.summary)
			}
			require.Len(t, result.findings, len(tt.wantIn), "findings: %q", result.findings)
			for _, want := range tt.wantIn {
				assert.True(t, slices.ContainsFunc(result.findings, func(f string) bool { return strings.Contains(f, want) }), "want a finding containing %q in %q", want, result.findings)
			}
			if tt.wantRelay {
				assert.Equal(t, tt.out.stderr, result.relay, "a failed row shows taplo's own log")
			} else {
				assert.Empty(t, result.relay)
			}
		})
	}

	t.Run("no tracked TOML file", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) {
			t.Fatal("taplo must not start with nothing to hand it")
			return output{}, nil
		}
		result, err := tomlFindings(run, "taplo-path", root, []string{"README.md"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], "git tracks no TOML file")
	})
	t.Run("taplo that does not start", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) { return output{}, os.ErrNotExist }
		_, err := tomlFindings(run, "taplo-path", root, []string{".taplo.toml"})
		assert.ErrorContains(t, err, "running taplo-path")
	})
}

func TestFormatFindings(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "a.md", "docs/b.yml")
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o700))
	installPrettier(t, root)

	tests := []struct {
		name        string
		out         output
		wantSummary string
		wantIn      []string
	}{
		{
			name:        "Prettier names every file it checked",
			out:         output{stdout: []byte("Checking formatting...\na.md\ndocs/b.yml\nAll matched files use Prettier code style!\n")},
			wantSummary: "prettier checked 2 files",
		},
		{
			name:        "file names carrying escape sequences, as a runner that forces color prints them",
			out:         output{stdout: []byte("\x1b[1mChecking formatting...\x1b[22m\n\x1b[90ma.md\x1b[39m\n\x1b[90mdocs/b.yml\x1b[39m\n")},
			wantSummary: "prettier checked 2 files",
		},
		{
			name:        "a line that names no file does not count",
			out:         output{stdout: []byte("Checking formatting...\r\na.md\r\ndir\r\nmissing.md\r\n")},
			wantSummary: "prettier checked 1 file",
		},
		{
			name:   "Prettier matched nothing",
			out:    output{stdout: []byte("Checking formatting...\nAll matched files use Prettier code style!\n")},
			wantIn: []string{"Prettier named no file it checked, so the format row checked nothing"},
		},
		{
			name:   "a misformatted file",
			out:    output{stdout: []byte("Checking formatting...\na.md\n"), stderr: []byte("[warn] a.md\n"), code: 1},
			wantIn: []string{"prettier exited 1"},
		},
		{
			name:   "Prettier that fails before it names a file, reported as its exit alone",
			out:    output{stderr: []byte("[error] Invalid configuration\n"), code: 2},
			wantIn: []string{"prettier exited 2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func(name string, env []string, args ...string) (output, error) {
				assert.Equal(t, "bun-path", name)
				assert.Nil(t, env)
				assert.Equal(t, prettierArgs, args)
				return tt.out, nil
			}
			result, err := formatFindings(run, "bun-path", root)
			require.NoError(t, err)
			if tt.wantSummary != "" {
				assert.Equal(t, tt.wantSummary, result.summary)
			}
			if len(tt.wantIn) == 0 {
				assert.Empty(t, result.findings)
				assert.Empty(t, result.relay)
				return
			}
			assert.Equal(t, tt.out.stderr, result.relay)
			require.Len(t, result.findings, len(tt.wantIn), "findings: %q", result.findings)
			for _, want := range tt.wantIn {
				assert.True(t, slices.ContainsFunc(result.findings, func(f string) bool { return strings.Contains(f, want) }), "want a finding containing %q in %q", want, result.findings)
			}
		})
	}

	t.Run("bun that does not start", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) { return output{}, os.ErrNotExist }
		_, err := formatFindings(run, "bun-path", root)
		assert.ErrorContains(t, err, "running bun-path")
	})
	t.Run("Prettier not installed, so bunx never starts", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) {
			t.Fatal("bunx must not start before the install holds Prettier")
			return output{}, nil
		}
		_, err := formatFindings(run, "bun-path", t.TempDir())
		assert.EqualError(t, err, "prettier is not installed in this checkout: run bun install --frozen-lockfile, or bun install --frozen-lockfile --ignore-scripts in a worktree (CONTRIBUTING.md#setup)")
	})
}

// Each case plants what node_modules/.bin holds for a tool, and the check must
// pass only a command that resolves to a regular file under the name the
// install writes on that platform, and refuse the rest with the set's message.
func TestInstalledTool(t *testing.T) {
	message := "prettier is not installed in this checkout: run bun install --frozen-lockfile, or bun install --frozen-lockfile --ignore-scripts in a worktree (CONTRIBUTING.md#setup)"
	tests := []struct {
		name    string
		goos    string
		plant   func(t *testing.T, bin string)
		wantErr bool
	}{
		{name: "the Windows command", goos: "windows", plant: func(t *testing.T, bin string) { writeFiles(t, bin, "prettier.exe") }},
		{name: "the command elsewhere", goos: "linux", plant: func(t *testing.T, bin string) { writeFiles(t, bin, "prettier") }},
		{name: "no node_modules at all", goos: "linux", plant: func(*testing.T, string) {}, wantErr: true},
		{name: "another tool's command alone", goos: "linux", plant: func(t *testing.T, bin string) { writeFiles(t, bin, "commitlint") }, wantErr: true},
		{name: "the bare name on Windows, where bun install writes prettier.exe", goos: "windows", plant: func(t *testing.T, bin string) { writeFiles(t, bin, "prettier", "prettier.bunx") }, wantErr: true},
		{name: "the .exe elsewhere, which is not the command", goos: "darwin", plant: func(t *testing.T, bin string) { writeFiles(t, bin, "prettier.exe") }, wantErr: true},
		{name: "a directory in the command's place", goos: "linux", plant: func(t *testing.T, bin string) { require.NoError(t, os.MkdirAll(filepath.Join(bin, "prettier"), 0o700)) }, wantErr: true},
		{
			name: "a link to the package's entry, as bun install writes it", goos: "linux",
			plant: func(t *testing.T, bin string) {
				writeFiles(t, filepath.Dir(bin), "prettier/bin/prettier.cjs")
				require.NoError(t, os.MkdirAll(bin, 0o700))
				if err := os.Symlink(filepath.Join("..", "prettier", "bin", "prettier.cjs"), filepath.Join(bin, "prettier")); err != nil {
					t.Skipf("this machine cannot create a symlink, so the link cannot be planted: %v", err)
				}
			},
		},
		{
			name: "a link a removed package left behind", goos: "linux",
			plant: func(t *testing.T, bin string) {
				require.NoError(t, os.MkdirAll(bin, 0o700))
				if err := os.Symlink(filepath.Join("..", "prettier", "bin", "prettier.cjs"), filepath.Join(bin, "prettier")); err != nil {
					t.Skipf("this machine cannot create a symlink, so the link cannot be planted: %v", err)
				}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.plant(t, filepath.Join(root, "node_modules", ".bin"))
			err := installedTool(root, "prettier", tt.goos)
			if tt.wantErr {
				assert.EqualError(t, err, message)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// fakeActionlintRunner plays actionlint over the files after --: it checks the
// command line, environment and config the row builds, logs -verbose's
// finished line for every file but skip, and answers with out's stdout and
// code. The finished lines carry one prefix, two and none in turn, as
// actionlint's parallel writes leave them.
func fakeActionlintRunner(t *testing.T, out output, skip string, handed *[]string) commandRunner {
	t.Helper()
	return func(name string, env []string, args ...string) (output, error) {
		assert.Equal(t, "actionlint-path", name)
		assert.Empty(t, env, "the runner withholds SHELLCHECK_OPTS, so the row adds nothing")
		before, after, found := cutArgs(args, "--")
		require.True(t, found, "the files follow --")
		require.Len(t, before, 5)
		assert.Equal(t, []string{"-shellcheck=shellcheck-path", "-pyflakes=", "-verbose", "-config-file"}, before[:4])
		config, err := os.ReadFile(before[4]) // #nosec G703 -- the path is the config the row wrote
		require.NoError(t, err)
		assert.Empty(t, config)
		*handed = after
		log := "verbose: Linting " + strconv.Itoa(len(after)) + " files\n"
		prefixes := []string{"verbose: ", "verbose: verbose: ", ""}
		for i, file := range after {
			log += "verbose: Linting " + file + "\n"
			if file != skip {
				log += prefixes[i%len(prefixes)] + "Found total 0 errors in 1 ms for " + file + "\n"
			}
		}
		out.stderr = append([]byte(log), out.stderr...)
		return out, nil
	}
}

func TestActionlintFindings(t *testing.T) {
	tracked := []string{
		".github/workflows/ci.yml", ".github/workflows/cd.yaml", ".GitHub/Workflows/Up.YML",
		".github/workflows/nested/x.yml", ".github/workflows/notes.md", ".github/dependabot.yml", "README.md",
	}
	wantHanded := []string{".github/workflows/ci.yml", ".github/workflows/cd.yaml", ".GitHub/Workflows/Up.YML"}

	tests := []struct {
		name        string
		out         output
		skip        string
		wantSummary string
		wantIn      []string
		wantRelay   string
	}{
		{
			name:        "actionlint lints every file the row handed it",
			wantSummary: "actionlint checked 3 workflow files: .github/workflows/ci.yml, .github/workflows/cd.yaml, .GitHub/Workflows/Up.YML",
		},
		{
			name:   "a file actionlint never reports linting",
			skip:   ".github/workflows/cd.yaml",
			wantIn: []string{"actionlint did not report linting .github/workflows/cd.yaml, which the workflows row handed it"},
		},
		{
			name:      "a finding",
			out:       output{stdout: []byte(".github/workflows/ci.yml:6:9: shellcheck reported issue [shellcheck]\n"), stderr: []byte("a warning outside -verbose\n"), code: 1},
			wantIn:    []string{"actionlint exited 1"},
			wantRelay: ".github/workflows/ci.yml:6:9: shellcheck reported issue [shellcheck]\na warning outside -verbose\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handed []string
			result, err := actionlintFindings(fakeActionlintRunner(t, tt.out, tt.skip, &handed), "actionlint-path", "shellcheck-path", t.TempDir(), tracked)
			require.NoError(t, err)
			assert.Equal(t, wantHanded, handed)
			if tt.wantSummary != "" {
				assert.Equal(t, tt.wantSummary, result.summary)
			}
			if len(tt.wantIn) == 0 {
				assert.Empty(t, result.findings)
			}
			for _, want := range tt.wantIn {
				assert.True(t, slices.ContainsFunc(result.findings, func(f string) bool { return strings.Contains(f, want) }), "want a finding containing %q in %q", want, result.findings)
			}
			assert.Equal(t, tt.wantRelay, string(result.relay), "the relay holds actionlint's findings and every line outside -verbose")
		})
	}

	t.Run("finished lines carrying escape sequences, as a runner that forces color prints them", func(t *testing.T) {
		var handed []string
		plain := fakeActionlintRunner(t, output{}, "", &handed)
		colored := strings.NewReplacer("verbose: ", "\x1b[2mverbose: \x1b[0m", "Found total", "\x1b[1mFound\x1b[0m total")
		run := func(name string, env []string, args ...string) (output, error) {
			out, err := plain(name, env, args...)
			out.stderr = []byte(colored.Replace(string(out.stderr)))
			return out, err
		}
		result, err := actionlintFindings(run, "actionlint-path", "shellcheck-path", t.TempDir(), tracked)
		require.NoError(t, err)
		assert.Equal(t, "actionlint checked 3 workflow files: .github/workflows/ci.yml, .github/workflows/cd.yaml, .GitHub/Workflows/Up.YML", result.summary)
		assert.Empty(t, result.findings)
		assert.Empty(t, result.relay, "a -verbose line stays out of the relay, colored or not")
	})
	t.Run("no tracked workflow", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) {
			t.Fatal("actionlint must not start with nothing to hand it")
			return output{}, nil
		}
		result, err := actionlintFindings(run, "actionlint-path", "shellcheck-path", t.TempDir(), []string{".github/dependabot.yml"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], "git tracks no file in .github/workflows")
	})
	t.Run("actionlint that does not start", func(t *testing.T) {
		run := func(string, []string, ...string) (output, error) { return output{}, errors.New("no such file") }
		_, err := actionlintFindings(run, "actionlint-path", "shellcheck-path", t.TempDir(), tracked)
		assert.ErrorContains(t, err, "running actionlint-path")
	})
	t.Run("a shell ShellCheck skips fails the row beside actionlint's own pass", func(t *testing.T) {
		root := t.TempDir()
		workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
		require.NoError(t, os.MkdirAll(filepath.Dir(workflow), 0o700))
		require.NoError(t, os.WriteFile(workflow, []byte("jobs:\n  a:\n    steps:\n      - run: echo $X\n        shell: /bin/bash -e {0}\n"), 0o600))
		var handed []string
		result, err := actionlintFindings(fakeActionlintRunner(t, output{}, "", &handed), "actionlint-path", "shellcheck-path", root, []string{".github/workflows/ci.yml"})
		require.NoError(t, err)
		require.Len(t, result.findings, 1)
		assert.Contains(t, result.findings[0], `.github/workflows/ci.yml sets jobs.a.steps[0].shell to "/bin/bash -e {0}"`)
	})
}

// Each case is one workflow's text, and the check must refuse every shell:
// value outside bash, sh and pwsh wherever GitHub reads one, however the YAML
// spells it, and pass every other workflow.
func TestShellFindings(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantIn  []string
	}{
		{name: "no shell anywhere", content: "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"},
		{name: "the three shells the set runs", content: "defaults:\n  run:\n    shell: bash\njobs:\n  a:\n    defaults:\n      run:\n        shell: pwsh\n    steps:\n      - run: echo hi\n        shell: sh\n"},
		{name: "a step running a bash command line", content: "jobs:\n  a:\n    steps:\n      - uses: actions/checkout@x\n      - run: echo $X\n        shell: /bin/bash -e {0}\n", wantIn: []string{`sets jobs.a.steps[1].shell to "/bin/bash -e {0}"`}},
		{name: "bash with arguments", content: "jobs:\n  a:\n    steps:\n      - run: echo $X\n        shell: bash -e {0}\n", wantIn: []string{`"bash -e {0}"`}},
		{name: "a workflow default", content: "defaults:\n  run:\n    shell: python\njobs: {}\n", wantIn: []string{`sets defaults.run.shell to "python"`}},
		{name: "a job default", content: "jobs:\n  build:\n    defaults:\n      run:\n        shell: cmd\n", wantIn: []string{`sets jobs.build.defaults.run.shell to "cmd"`}},
		{name: "a shell in capitals", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        shell: Bash\n", wantIn: []string{`"Bash"`}},
		{name: "a key in capitals", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        SHELL: zsh\n", wantIn: []string{`"zsh"`}},
		{name: "a value behind an alias", content: "x: &custom /bin/bash -e {0}\njobs:\n  a:\n    steps:\n      - run: echo\n        shell: *custom\n", wantIn: []string{`"/bin/bash -e {0}"`}},
		{name: "a value behind escapes", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        shell: \"\\x2Fbin/bash {0}\"\n", wantIn: []string{`"/bin/bash {0}"`}},
		{name: "a job named by a number", content: "jobs:\n  1:\n    steps:\n      - run: echo\n        shell: fish\n", wantIn: []string{`sets jobs.1.steps[0].shell to "fish"`}},
		{name: "a job id carrying an escape sequence", content: "jobs:\n  \"a\\e[2J\\r\":\n    steps:\n      - run: echo\n        shell: fish\n", wantIn: []string{`sets jobs.a\x1b[2J\r.steps[0].shell to "fish"`}},
		{name: "a job default under an id carrying a newline", content: "jobs:\n  \"b\\nc\":\n    defaults:\n      run:\n        shell: cmd\n", wantIn: []string{`sets jobs.b\nc.defaults.run.shell to "cmd"`}},
		{name: "a value that is not text carrying an escape sequence", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        shell: [\"\\e[2J\"]\n", wantIn: []string{`sets jobs.a.steps[0].shell to "[\x1b[2J]"`}},
		{name: "an empty shell", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        shell:\n", wantIn: []string{`sets jobs.a.steps[0].shell to "<nil>"`}},
		{name: "a shell that is not text", content: "jobs:\n  a:\n    steps:\n      - run: echo\n        shell: [bash]\n", wantIn: []string{`sets jobs.a.steps[0].shell to "[bash]"`}},
		{name: "a second document", content: "on: push\n---\njobs:\n  a:\n    steps:\n      - run: echo\n        shell: fish\n", wantIn: []string{`"fish"`}},
		{name: "YAML that does not parse", content: "jobs:\n  a: [\n", wantIn: []string{"does not parse as YAML"}},
		{name: "a duplicated key", content: "jobs:\n  a:\n    steps: []\n    steps: []\n", wantIn: []string{"does not parse as YAML"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(workflow), 0o700))
			require.NoError(t, os.WriteFile(workflow, []byte(tt.content), 0o600))
			found, err := shellFindings(root, []string{".github/workflows/ci.yml", ".github/workflows/gone.yml"})
			require.NoError(t, err)
			require.Len(t, found, len(tt.wantIn), "findings: %q", found)
			for i, want := range tt.wantIn {
				assert.Contains(t, found[i], want)
				assert.Contains(t, found[i], ".github/workflows/ci.yml")
			}
		})
	}
	t.Run("a workflow path that is a directory is an error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows", "ci.yml"), 0o700))
		_, err := shellFindings(root, []string{".github/workflows/ci.yml"})
		assert.ErrorContains(t, err, "reading .github/workflows/ci.yml")
	})
}

func TestTaploChecked(t *testing.T) {
	tests := []struct {
		name    string
		log     string
		want    []string
		wantOK  bool
		wantErr string
	}{
		{name: "no found files line", log: " INFO taplo: found configuration file\n"},
		{name: "a list", log: ` INFO found files total=2 excluded=0 files=["/a/b.toml", "/c.toml"] cwd="/"` + "\n", want: []string{"/a/b.toml", "/c.toml"}, wantOK: true},
		{name: "an empty list", log: ` INFO found files total=0 excluded=0 files=[] cwd="/"` + "\n", wantOK: true},
		{name: "a line with no list", log: " INFO found files total=0\n", wantOK: true, wantErr: "the line lists no files"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := taploChecked([]byte(tt.log))
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDebugStrings(t *testing.T) {
	tests := []struct {
		name    string
		list    string
		want    []string
		wantErr string
	}{
		{name: "an empty list", list: "[]"},
		{name: "one item", list: `["a"]`, want: []string{"a"}},
		{name: "two items and what follows", list: `["a", "b"] cwd="x"`, want: []string{"a", "b"}},
		{name: "the two escapes a path can need", list: `["a\"b", "c\\d"]`, want: []string{`a"b`, `c\d`}},
		{name: "text beyond ASCII", list: `["C:/Users/Zoë/ſ.toml"]`, want: []string{"C:/Users/Zoë/ſ.toml"}},
		{name: "any other escape", list: `["a\tb"]`, wantErr: "an escape this check does not read"},
		{name: "a trailing backslash", list: `["a\`, wantErr: "an escape this check does not read"},
		{name: "no closing quote", list: `["a`, wantErr: "no closing quote"},
		{name: "a separator it does not know", list: `["a";"b"]`, wantErr: "the list continues with"},
		{name: "an item with no quote", list: `[a]`, wantErr: "not a quote"},
		{name: "no opening bracket", list: `"a"`, wantErr: "does not open with ["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := debugStrings(tt.list)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCountFiles(t *testing.T) {
	assert.Equal(t, "1 TOML file", countFiles(1, "TOML"))
	assert.Equal(t, "3 workflow files", countFiles(3, "workflow"))
	assert.Equal(t, "0 files", countFiles(0, ""))
}

// Each case hands the check tracked paths, with the content of any it reads,
// and the check must refuse a workflow whose extension is not a lowercase
// .yml and an inline zizmor waiver anywhere under .github, and let every other
// path pass. A path under a version control directory is review's to refuse.
func TestWalkedFindings(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		files   map[string]string
		wantIn  string
	}{
		{name: "a workflow in capitals", tracked: []string{".github/workflows/UP.YML"}, wantIn: `".github/workflows/UP.YML" is a workflow named .YML, and every workflow here ends in .yml`},
		{name: "a workflow named .yaml", tracked: []string{".github/workflows/ci.yaml"}, wantIn: "is a workflow named .yaml"},
		{name: "a workflow directory in capitals", tracked: []string{".GitHub/Workflows/cd.Yml"}, wantIn: "is a workflow named .Yml"},
		{name: "a workflow named .yml", tracked: []string{".github/workflows/ci.yml"}},
		{name: "a YAML file beside the workflows", tracked: []string{".github/dependabot.YAML"}},
		{name: "a YAML file one level below the workflows", tracked: []string{".github/workflows/nested/x.YAML"}},
		{name: "a path under a version control directory, which review holds", tracked: []string{"docs/.git/notes.md", ".JJ/repo/x.yml"}},
		{
			name: "an inline zizmor waiver in a workflow", tracked: []string{".github/workflows/cd.yml"},
			files:  map[string]string{".github/workflows/cd.yml": "jobs:\n  a:\n    secrets: inherit # zizmor: ignore[secrets-inherit]\n"},
			wantIn: `".github/workflows/cd.yml" carries an inline zizmor ignore comment, which waives an audit outside .github/zizmor.yml`,
		},
		{
			name: "an inline waiver in a file .gitattributes marks binary, which git grep -I skips", tracked: []string{".gitattributes", ".github/workflows/cd.yml"},
			files: map[string]string{
				".gitattributes":           ".github/workflows/cd.yml -diff\n",
				".github/workflows/cd.yml": "jobs:\n  a:\n    secrets: inherit # zizmor: ignore[secrets-inherit]\n",
			},
			wantIn: `".github/workflows/cd.yml" carries an inline zizmor ignore comment`,
		},
		{
			name: "an inline waiver spelled another way, in a directory in capitals", tracked: []string{".GitHub/actions/x/action.yml"},
			files:  map[string]string{".GitHub/actions/x/action.yml": "runs: # ZIZMOR:ignore[unpinned-uses]\n"},
			wantIn: "carries an inline zizmor ignore comment",
		},
		{
			name: "an inline waiver outside .github, which zizmor never reads", tracked: []string{"docs/usage.md"},
			files: map[string]string{"docs/usage.md": "# zizmor: ignore[x]\n"},
		},
		{
			name: "zizmor.yml, whose rules are the one waiver list", tracked: []string{zizmorConfig},
			files: map[string]string{zizmorConfig: "rules:\n  secrets-inherit:\n    ignore:\n      - cd.yml\n"},
		},
		{name: "a tracked file the work tree deleted", tracked: []string{".github/workflows/gone.yml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				target := filepath.Join(dir, filepath.FromSlash(name))
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
				require.NoError(t, os.WriteFile(target, []byte(content), 0o600))
			}
			found, err := walkedFindings(dir, tt.tracked)
			require.NoError(t, err)
			if tt.wantIn == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Contains(t, found[0], tt.wantIn)
		})
	}

	t.Run("a tracked path under .github that is a directory is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".github", "x"), 0o700))
		_, err := walkedFindings(dir, []string{".github/x"})
		assert.ErrorContains(t, err, "reading .github/x")
	})
}

func TestEscaped(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text", in: "build", want: "build"},
		{name: "an escape sequence", in: "a\x1b[2Jb", want: `a\x1b[2Jb`},
		{name: "a carriage return and a newline", in: "a\r\nb", want: `a\r\nb`},
		{name: "a quote and a backslash", in: `a"b\c`, want: `a\"b\\c`},
		{name: "text beyond ASCII", in: "Zoë", want: "Zoë"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escaped(tt.in))
		})
	}
}
