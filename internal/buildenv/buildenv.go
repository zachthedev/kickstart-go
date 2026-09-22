// Package buildenv declares the build-time settings Taskfile.yml hands to
// the build.
//
// A build setting arrives through the environment or a `task` command line
// and reaches the linker as an `-ldflags -X` value. It is machine-specific,
// so it cannot be committed.
//
// This file is the declaration: `.env.template` is generated from it, and a
// test asserts the build injects nothing undeclared.
//
// Scope is build settings only. Not GOOS, GOARCH or CC, which select a
// toolchain rather than configure a build, and not anything the program
// reads at runtime, which the config file owns.
//
// TODO(kickstart): replace the entries Variables() returns with your project's
// own build settings.
package buildenv

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// Variable is one build-time setting.
type Variable struct {
	// Name is the environment variable, spelled as Taskfile.yml reads it.
	Name string
	// Purpose says what the build does with the value.
	Purpose string
	// Example is a value of the right shape, shown in a comment. It is a
	// placeholder rather than a real one: this reaches a committed file.
	Example string
	// Absent says what a build without the variable does instead. Every
	// setting here is optional.
	Absent string
}

// ///////////////////////////////////////////////
// Declarations
// ///////////////////////////////////////////////

// Variables returns every build setting, in the order it appears in the
// generated template. Declaration order, not sorted: related settings
// stay adjacent and the credential-shaped one goes last.
func Variables() []Variable {
	return []Variable{
		{
			Name:    "VERSION",
			Purpose: "SemVer string stamped into the binary and reported by the version flag.",
			Example: "1.4.2",
			Absent:  "the version comes from git describe, or 0.0.0-dev outside a repository",
		},
		{
			Name:    "REPO_OWNER",
			Purpose: "GitHub owner stamped into the binary, used to build release and issue URLs.",
			Example: "your-github-user",
			Absent:  "the owner is read from the origin remote, and is empty until the repo is published",
		},
		{
			Name:    "REPO_NAME",
			Purpose: "GitHub repository name, stamped alongside REPO_OWNER.",
			Example: "your-repo",
			Absent:  "the name is read from the origin remote, and is empty until the repo is published",
		},
	}
}

// Names returns every declared variable name.
func Names() []string {
	vars := Variables()
	names := make([]string, len(vars))
	for i, v := range vars {
		names[i] = v.Name
	}
	return names
}
