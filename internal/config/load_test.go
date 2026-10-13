package config

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(runIsolated(m))
}

// runIsolated runs the tests with the global config and data paths in a
// temp dir, so no test can rewrite the developer's real config.
func runIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "crush-config-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(home)
	os.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(home, "config"))
	os.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data"))
	return m.Run()
}

func TestConfig_LoadFromBytes(t *testing.T) {
	data1 := []byte(`{"providers": {"openai": {"api_key": "key1", "base_url": "https://api.openai.com/v1"}}}`)
	data2 := []byte(`{"providers": {"openai": {"api_key": "key2", "base_url": "https://api.openai.com/v2"}}}`)
	data3 := []byte(`{"providers": {"openai": {}}}`)

	loadedConfig, err := loadFromBytes([][]byte{data1, data2, data3})

	require.NoError(t, err)
	require.NotNil(t, loadedConfig)
	require.Equal(t, 1, loadedConfig.Providers.Len())
	pc, _ := loadedConfig.Providers.Get("openai")
	require.Equal(t, "key2", pc.APIKey)
	require.Equal(t, "https://api.openai.com/v2", pc.BaseURL)
}

func TestLookupConfigs_BoundedByProject(t *testing.T) {
	// Force GlobalConfig and GlobalConfigData to point at locations we
	// control so they can be present in the result without polluting
	// the developer's real config.
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)
	t.Setenv("CRUSH_GLOBAL_DATA", dataDir)

	t.Run("does not pick up crush.json above non-git project", func(t *testing.T) {
		parent := t.TempDir()

		// crush.json above the project must not be adopted.
		require.NoError(t, os.WriteFile(
			filepath.Join(parent, "crush.json"),
			[]byte(`{}`),
			0o644,
		))

		project := filepath.Join(parent, "project")
		require.NoError(t, os.Mkdir(project, 0o755))

		got := lookupConfigs(project)
		for _, p := range got {
			require.NotEqual(t, filepath.Join(parent, "crush.json"), p)
		}
	})

	t.Run("does not climb out of git worktree to find crush.json", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		parent := t.TempDir()

		require.NoError(t, os.WriteFile(
			filepath.Join(parent, "crush.json"),
			[]byte(`{}`),
			0o644,
		))

		worktree := filepath.Join(parent, "worktree")
		require.NoError(t, os.Mkdir(worktree, 0o755))
		gitInit := exec.CommandContext(t.Context(), "git", "init", "-q")
		gitInit.Dir = worktree
		require.NoError(t, gitInit.Run())

		got := lookupConfigs(worktree)
		strayEval, err := filepath.EvalSymlinks(filepath.Join(parent, "crush.json"))
		require.NoError(t, err)
		for _, p := range got {
			pEval, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			require.NotEqual(t, strayEval, pEval, "must not adopt parent crush.json")
		}
	})

	t.Run("picks up crush.json inside the project", func(t *testing.T) {
		project := t.TempDir()
		local := filepath.Join(project, "crush.json")
		require.NoError(t, os.WriteFile(local, []byte(`{}`), 0o644))

		got := lookupConfigs(project)

		localEval, err := filepath.EvalSymlinks(local)
		require.NoError(t, err)
		var foundLocal bool
		for _, p := range got {
			pEval, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			if pEval == localEval {
				foundLocal = true
				break
			}
		}
		require.True(t, foundLocal, "expected project crush.json to be in lookup result: %v", got)
	})

	t.Run("global config is always included regardless of boundary", func(t *testing.T) {
		project := t.TempDir()

		got := lookupConfigs(project)
		// Global config and global data path are always prepended,
		// even when no project file exists.
		require.Contains(t, got, GlobalConfig())
		require.Contains(t, got, GlobalConfigData())
	})

	t.Run("global shell config (crushrc) is included", func(t *testing.T) {
		project := t.TempDir()

		got := lookupConfigs(project)
		// A global crushrc is discovered only beside the user config. The data
		// directory is machine-owned state and must never execute a crushrc.
		require.Contains(t, got, shellConfigSibling(GlobalConfig()))
		require.NotContains(t, got, shellConfigSibling(GlobalConfigData()))
	})

	t.Run("project crushrc and .crushrc are discovered", func(t *testing.T) {
		project := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(project, "crushrc"), []byte(""), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(project, ".crushrc"), []byte(""), 0o644))

		got := lookupConfigs(project)
		require.Contains(t, got, filepath.Join(project, "crushrc"))
		require.Contains(t, got, filepath.Join(project, ".crushrc"))
	})

	t.Run("system config is loaded first", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("system config not supported on Windows")
		}

		got := lookupConfigs(t.TempDir())
		require.NotEmpty(t, got)
		// The system-wide config must be first so it has the lowest
		// priority when configs are merged.
		require.Equal(t, "/etc/crush/crush.json", got[0])
	})
}

func TestLoadFromConfigPaths_InvalidJSON(t *testing.T) {
	t.Parallel()

	t.Run("identifies the offending file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		good := filepath.Join(tmpDir, "good.json")
		bad := filepath.Join(tmpDir, "bad.json")
		require.NoError(t, os.WriteFile(good, []byte(`{"providers":{}}`), 0o644))
		require.NoError(t, os.WriteFile(bad, []byte(`{not valid json}`), 0o644))

		_, _, err := loadFromConfigPaths(context.Background(), []string{good, bad})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid JSON in config file")
		require.Contains(t, err.Error(), "bad.json")
	})

	t.Run("skips missing and empty files", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		empty := filepath.Join(tmpDir, "empty.json")
		require.NoError(t, os.WriteFile(empty, []byte(""), 0o644))

		cfg, _, err := loadFromConfigPaths(context.Background(), []string{
			filepath.Join(tmpDir, "nonexistent.json"),
			empty,
		})
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
}

// TestLoadFromConfigPaths_ConflictWarningNamesKeys verifies that when a JSON
// config and a crushrc coexist in the same directory, the merge warning names
// the overlapping top-level keys so incremental migrations can spot stale
// duplicates.
func TestLoadFromConfigPaths_ConflictWarningNamesKeys(t *testing.T) {
	capture := func(t *testing.T) *strings.Builder {
		t.Helper()
		var buf strings.Builder
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		t.Cleanup(func() { slog.SetDefault(prev) })
		return &buf
	}

	t.Run("names overlapping keys", func(t *testing.T) {
		buf := capture(t)
		tmpDir := t.TempDir()
		jsonPath := filepath.Join(tmpDir, "crush.json")
		rcPath := filepath.Join(tmpDir, "crushrc")
		require.NoError(t, os.WriteFile(jsonPath, []byte(`{"options":{"debug":true},"providers":{}}`), 0o644))
		require.NoError(t, os.WriteFile(rcPath, []byte("option debug true\n"), 0o644))

		_, _, err := loadFromConfigPaths(context.Background(), []string{jsonPath, rcPath})
		require.NoError(t, err)
		require.Contains(t, buf.String(), "crushrc taking precedence")
		require.Contains(t, buf.String(), `"conflicting_keys":"options"`)
	})

	t.Run("no warning when nothing overlaps", func(t *testing.T) {
		buf := capture(t)
		tmpDir := t.TempDir()
		jsonPath := filepath.Join(tmpDir, "crush.json")
		rcPath := filepath.Join(tmpDir, "crushrc")
		require.NoError(t, os.WriteFile(jsonPath, []byte(`{"providers":{}}`), 0o644))
		require.NoError(t, os.WriteFile(rcPath, []byte("option debug true\n"), 0o644))

		_, _, err := loadFromConfigPaths(context.Background(), []string{jsonPath, rcPath})
		require.NoError(t, err)
		require.NotContains(t, buf.String(), "crushrc taking precedence",
			"disjoint coexistence should not warn")
	})
}

func TestConfig_EnabledProviders(t *testing.T) {
	t.Run("all providers enabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: false,
				},
			}),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 2)
	})

	t.Run("some providers disabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: true,
				},
			}),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 1)
		require.Equal(t, "openai", enabled[0].ID)
	})

	t.Run("empty providers map", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMap[string, ProviderConfig](),
		}

		enabled := cfg.EnabledProviders()
		require.Len(t, enabled, 0)
	})
}

func TestConfig_IsConfigured(t *testing.T) {
	t.Run("returns true when at least one provider is enabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: false,
				},
			}),
		}

		require.True(t, cfg.IsConfigured())
	})

	t.Run("returns false when no providers are configured", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMap[string, ProviderConfig](),
		}

		require.False(t, cfg.IsConfigured())
	})

	t.Run("returns false when all providers are disabled", func(t *testing.T) {
		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:      "openai",
					APIKey:  "key1",
					Disable: true,
				},
				"anthropic": {
					ID:      "anthropic",
					APIKey:  "key2",
					Disable: true,
				},
			}),
		}

		require.False(t, cfg.IsConfigured())
	})
}

func TestConfig_LoadFromBytes_Env(t *testing.T) {
	data := []byte(`{"env": {"AWS_PROFILE": "my-profile", "AWS_REGION": "us-west-2"}}`)

	loadedConfig, err := loadFromBytes([][]byte{data})

	require.NoError(t, err)
	require.NotNil(t, loadedConfig.Env)
	require.Equal(t, "my-profile", loadedConfig.Env["AWS_PROFILE"])
	require.Equal(t, "us-west-2", loadedConfig.Env["AWS_REGION"])
}

func TestConfig_LoadFromBytes_EnvMerge(t *testing.T) {
	data1 := []byte(`{"env": {"AWS_PROFILE": "first", "AWS_REGION": "us-east-1"}}`)
	data2 := []byte(`{"env": {"AWS_PROFILE": "second"}}`)

	loadedConfig, err := loadFromBytes([][]byte{data1, data2})

	require.NoError(t, err)
	require.NotNil(t, loadedConfig.Env)
	require.Equal(t, "second", loadedConfig.Env["AWS_PROFILE"])
	require.Equal(t, "us-east-1", loadedConfig.Env["AWS_REGION"])
}
