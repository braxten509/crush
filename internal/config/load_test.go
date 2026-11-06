package config

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
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

func TestConfig_LoadFromBytes_MigratesCatwalkV2Models(t *testing.T) {
	data := []byte(`{
		"providers": {
			"openrouter": {
				"type": "openrouter",
				"models": [
					{
						"id": "reasoning-model",
						"name": "Reasoning Model",
						"cost_per_1m_in": 1.5,
						"cost_per_1m_out": 6,
						"cost_per_1m_in_cached": 0.15,
						"cost_per_1m_out_cached": 0.6,
						"context_window": 200000,
						"default_max_tokens": 8192,
						"can_reason": true,
						"reasoning_levels": ["low", "high"],
						"default_reasoning_effort": "high",
						"supports_attachments": true
					},
					{
						"id": "dumb-model",
						"name": "Dumb Model",
						"cost_per_1m_in": 0.5,
						"cost_per_1m_out": 2,
						"can_reason": false
					}
				]
			}
		}
	}`)

	cfg, err := loadFromBytes([][]byte{data})

	require.NoError(t, err)
	provider, ok := cfg.Providers.Get("openrouter")
	require.True(t, ok)

	require.Len(t, provider.Models, 2)

	reasoning := provider.Models[0]
	require.Equal(t, "reasoning-model", reasoning.ID)
	require.Equal(t, 1.5, reasoning.Pricing.Input)
	require.Equal(t, 6.0, reasoning.Pricing.Output)
	require.Equal(t, 0.15, reasoning.Pricing.CacheCreate)
	require.Equal(t, 0.6, reasoning.Pricing.CacheHit)
	require.True(t, reasoning.Capabilities.Vision)
	require.Equal(t, catwalk.ThinkingToggleable, reasoning.Reasoning.Thinking)
	require.Equal(t, []string{"low", "high"}, reasoning.ReasoningEffortLevels())
	require.Equal(t, "high", reasoning.Reasoning.DefaultEffortLevel)
	require.Equal(t, int64(200000), reasoning.ContextWindow)
	require.Equal(t, int64(8192), reasoning.DefaultMaxTokens)

	dumb := provider.Models[1]
	require.Equal(t, 0.5, dumb.Pricing.Input)
	require.Equal(t, 2.0, dumb.Pricing.Output)
	require.False(t, dumb.Capabilities.Vision)
	require.Equal(t, catwalk.ThinkingNever, dumb.Reasoning.Thinking)
}

func TestConfig_LoadFromBytes_KeepsCatwalkV3Models(t *testing.T) {
	data := []byte(`{
		"providers": {
			"custom": {
				"type": "completions",
				"models": [
					{
						"id": "v3-model",
						"name": "V3 Model",
						"pricing": {"input": 3, "output": 15},
						"capabilities": {"vision": true},
						"reasoning": {"thinking": "always"}
					}
				]
			}
		}
	}`)

	cfg, err := loadFromBytes([][]byte{data})

	require.NoError(t, err)
	provider, ok := cfg.Providers.Get("custom")
	require.True(t, ok)
	require.Len(t, provider.Models, 1)
	model := provider.Models[0]
	require.Equal(t, 3.0, model.Pricing.Input)
	require.Equal(t, 15.0, model.Pricing.Output)
	require.True(t, model.Capabilities.Vision)
	require.Equal(t, catwalk.ThinkingAlways, model.Reasoning.Thinking)
}

func TestLoadFromConfigPaths_PersistsCatwalkV2Migration(t *testing.T) {
	t.Parallel()
	t.Run("rewrites v2 config files on disk", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "crush.json")
		require.NoError(t, os.WriteFile(path, []byte(`{
			"providers": {
				"openrouter": {
					"type": "openrouter",
					"models": [
						{
							"id": "reasoning-model",
							"name": "Reasoning Model",
							"cost_per_1m_in": 1.5,
							"cost_per_1m_out": 6,
							"can_reason": true,
							"reasoning_levels": ["low", "high"],
							"default_reasoning_effort": "high",
							"supports_attachments": true
						}
					]
				}
			}
		}`), 0o644))

		cfg, loaded, err := loadFromConfigPaths(context.Background(), []string{path})
		require.NoError(t, err)
		require.Contains(t, loaded, path)
		provider, ok := cfg.Providers.Get("openrouter")
		require.True(t, ok)
		require.Len(t, provider.Models, 1)
		require.Equal(t, 1.5, provider.Models[0].Pricing.Input)
		require.Equal(t, catwalk.ThinkingToggleable, provider.Models[0].Reasoning.Thinking)

		// The file itself is rewritten with the v3 schema and the legacy
		// fields are gone.
		rewritten, err := os.ReadFile(path)
		require.NoError(t, err)
		var raw map[string]any
		require.NoError(t, json.Unmarshal(rewritten, &raw))
		model := raw["providers"].(map[string]any)["openrouter"].(map[string]any)["models"].([]any)[0]
		m := model.(map[string]any)
		require.Contains(t, m, "pricing")
		require.Contains(t, m, "reasoning")
		require.Contains(t, m, "capabilities")
		require.NotContains(t, m, "cost_per_1m_in")
		require.NotContains(t, m, "can_reason")
		require.NotContains(t, m, "supports_attachments")
		require.NotContains(t, m, "reasoning_levels")
	})

	t.Run("leaves v3 config files untouched", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "crush.json")
		original := []byte(`{
			"providers": {
				"custom": {
					"type": "completions",
					"models": [
						{
							"id": "v3-model",
							"name": "V3 Model",
							"pricing": {"input": 3, "output": 15},
							"reasoning": {"thinking": "always"}
						}
					]
				}
			}
		}`)
		require.NoError(t, os.WriteFile(path, original, 0o644))

		_, _, err := loadFromConfigPaths(context.Background(), []string{path})
		require.NoError(t, err)
		rewritten, err := os.ReadFile(path)
		require.NoError(t, err)
		require.JSONEq(t, string(original), string(rewritten))
	})
}

func TestMigrateCatwalkV2KeepsLargeIntegersExact(t *testing.T) {
	in := `{"providers":{"x":{"models":[{"id":"m","context_window":9007199254740993,"cost_per_1m_in":0.1}]}}}`
	out, changed := migrateCatwalkV2([]byte(in))
	require.True(t, changed)
	require.Contains(t, string(out), `"context_window":9007199254740993`)
	require.Contains(t, string(out), `"pricing":{"input":0.1}`)
}

func TestNormalizeProviderType(t *testing.T) {
	t.Parallel()
	for in, want := range map[catwalk.Type]catwalk.Type{
		"openai":                catwalk.TypeResponses,
		"openai-compat":         catwalk.TypeCompletions,
		"anthropic":             catwalk.TypeMessages,
		catwalk.TypeCompletions: catwalk.TypeCompletions,
		"hyper":                 "hyper",
	} {
		require.Equal(t, want, NormalizeProviderType(in), string(in))
	}
}
