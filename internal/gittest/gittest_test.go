package gittest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The variables a hook exports, and a few more git reads, must be gone for the
// test and back afterward, so the caller's shell is untouched.
func TestIsolate_DropsInheritedGitVariables(t *testing.T) {
	inherited := map[string]string{
		"GIT_DIR":              filepath.Join(t.TempDir(), "victim", ".git"),
		"GIT_WORK_TREE":        filepath.Join(t.TempDir(), "victim"),
		"GIT_INDEX_FILE":       filepath.Join(t.TempDir(), "index"),
		"GIT_COMMON_DIR":       filepath.Join(t.TempDir(), "common"),
		"GIT_OBJECT_DIRECTORY": filepath.Join(t.TempDir(), "objects"),
		"GIT_NAMESPACE":        "probe",
		"GIT_CONFIG_COUNT":     "1",
		"GIT_CONFIG_KEY_0":     "core.attributesFile",
		"GIT_CONFIG_VALUE_0":   "/elsewhere",
	}
	for name, value := range inherited {
		t.Setenv(name, value)
	}

	t.Run("isolated", func(t *testing.T) {
		Isolate(t)
		for name := range inherited {
			_, present := os.LookupEnv(name)
			assert.False(t, present, "%s survived Isolate", name)
		}
	})

	for name, value := range inherited {
		assert.Equal(t, value, os.Getenv(name), "%s was not restored after the test", name)
	}
}

// git reads no configuration but an empty file, and discovery stops at the
// test's temporary root.
func TestIsolate_MasksConfigurationAndDiscovery(t *testing.T) {
	Isolate(t)

	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		path := os.Getenv(name)
		require.NotEmpty(t, path, "%s is unset", name)
		data, err := os.ReadFile(path) // #nosec G703 -- the path is the test's own temp file
		require.NoError(t, err, "%s names no readable file", name)
		assert.Empty(t, data, "%s names a file with content", name)
	}

	ceiling := os.Getenv("GIT_CEILING_DIRECTORIES")
	require.NotEmpty(t, ceiling, "GIT_CEILING_DIRECTORIES is unset")
	repo := t.TempDir()
	assert.True(t, strings.HasPrefix(repo, ceiling+string(filepath.Separator)),
		"a repository the test creates at %s sits outside the ceiling %s", repo, ceiling)
}
