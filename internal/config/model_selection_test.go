package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func TestConfig_defaultModelSelection(t *testing.T) {
	t.Run("default behavior uses the default models for given provider", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
	t.Run("should error if no providers configured", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "$MISSING_KEY",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.Error(t, err)
	})
	t.Run("should not error if model is missing", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)
		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
	})

	t.Run("should configure the default models with a custom provider", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "$MISSING", // will not be included in the config
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models: []catwalk.Model{
						{
							ID:               "model",
							DefaultMaxTokens: 600,
						},
					},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)
		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "model", large.Model)
		require.Equal(t, "custom", large.Provider)
		require.Equal(t, int64(600), large.MaxTokens)
		require.Equal(t, "model", small.Model)
		require.Equal(t, "custom", small.Provider)
		require.Equal(t, int64(600), small.MaxTokens)
	})

	t.Run("should fail if no model configured", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "$MISSING", // will not be included in the config
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "not-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models:  []catwalk.Model{},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)
		_, _, err = cfg.defaultModelSelection(knownProviders)
		require.Error(t, err)
	})
	t.Run("should use the default provider first", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "set",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"custom": {
					APIKey:  "test-key",
					BaseURL: "https://api.custom.com/v1",
					Models: []catwalk.Model{
						{
							ID:               "large-model",
							DefaultMaxTokens: 1000,
						},
					},
				},
			}),
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)
		large, small, err := cfg.defaultModelSelection(knownProviders)
		require.NoError(t, err)
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
}

func TestConfig_configureSelectedModels(t *testing.T) {
	t.Run("reload mode should not persist fallback defaults", func(t *testing.T) {
		dir := t.TempDir()
		globalPath := filepath.Join(dir, "crush.json")
		require.NoError(t, os.WriteFile(globalPath, []byte(`{"models":{"large":{"provider":"ghost","model":"missing"}}}`), 0o600))

		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{ID: "large-model", DefaultMaxTokens: 1000},
					{ID: "small-model", DefaultMaxTokens: 500},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large": {Provider: "ghost", Model: "missing"},
			},
		}
		cfg.setDefaults(dir, "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		cfg.takeLegacyChatModel()
		resolved, resolveErr := resolveSelectedModels(cfg, knownProviders)
		require.NoError(t, resolveErr)
		cfg.Models[SelectedModelTypeSmall] = resolved.Small

		// In-memory falls back to default.
		require.Equal(t, "openai", resolved.Start.Provider)
		require.Equal(t, "large-model", resolved.Start.Model)

		// Disk remains unchanged (resolveSelectedModels never persists).
		data, readErr := os.ReadFile(globalPath)
		require.NoError(t, readErr)
		require.Contains(t, string(data), `"provider":"ghost"`)
		require.Contains(t, string(data), `"model":"missing"`)
	})
	t.Run("should override defaults", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "larger-model",
						DefaultMaxTokens: 2000,
					},
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large": {
					Model: "larger-model",
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		cfg.takeLegacyChatModel()
		resolved, resolveErr := resolveSelectedModels(cfg, knownProviders)
		require.NoError(t, resolveErr)
		cfg.Models[SelectedModelTypeSmall] = resolved.Small
		large := resolved.Start
		small := cfg.Models[SelectedModelTypeSmall]
		require.Equal(t, "larger-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(2000), large.MaxTokens)
		require.Equal(t, "small-model", small.Model)
		require.Equal(t, "openai", small.Provider)
		require.Equal(t, int64(500), small.MaxTokens)
	})
	t.Run("should be possible to use multiple providers", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
			{
				ID:                  "anthropic",
				APIKey:              "abc",
				DefaultLargeModelID: "a-large-model",
				DefaultSmallModelID: "a-small-model",
				Models: []catwalk.Model{
					{
						ID:               "a-large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "a-small-model",
						DefaultMaxTokens: 200,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"small": {
					Model:     "a-small-model",
					Provider:  "anthropic",
					MaxTokens: 300,
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		cfg.takeLegacyChatModel()
		resolved, resolveErr := resolveSelectedModels(cfg, knownProviders)
		require.NoError(t, resolveErr)
		cfg.Models[SelectedModelTypeSmall] = resolved.Small
		large := resolved.Start
		small := cfg.Models[SelectedModelTypeSmall]
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(1000), large.MaxTokens)
		require.Equal(t, "a-small-model", small.Model)
		require.Equal(t, "anthropic", small.Provider)
		require.Equal(t, int64(300), small.MaxTokens)
	})

	t.Run("should override the max tokens only", func(t *testing.T) {
		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{
						ID:               "large-model",
						DefaultMaxTokens: 1000,
					},
					{
						ID:               "small-model",
						DefaultMaxTokens: 500,
					},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large": {
					MaxTokens: 100,
				},
			},
		}
		cfg.setDefaults("/tmp", "")
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		cfg.takeLegacyChatModel()
		resolved, resolveErr := resolveSelectedModels(cfg, knownProviders)
		require.NoError(t, resolveErr)
		cfg.Models[SelectedModelTypeSmall] = resolved.Small
		large := resolved.Start
		require.Equal(t, "large-model", large.Model)
		require.Equal(t, "openai", large.Provider)
		require.Equal(t, int64(100), large.MaxTokens)
	})
	t.Run("resolve and persist fallback under writeMu does not deadlock", func(t *testing.T) {
		dir := t.TempDir()
		globalPath := filepath.Join(dir, "crush.json")
		require.NoError(t, os.WriteFile(globalPath, []byte(`{}`), 0o600))

		knownProviders := []catwalk.Provider{
			{
				ID:                  "openai",
				APIKey:              "abc",
				DefaultLargeModelID: "large-model",
				DefaultSmallModelID: "small-model",
				Models: []catwalk.Model{
					{ID: "large-model", DefaultMaxTokens: 1000},
					{ID: "small-model", DefaultMaxTokens: 500},
				},
			},
		}

		cfg := &Config{
			Models: map[SelectedModelType]SelectedModel{
				"large":                {Provider: "openai", Model: "this-model-does-not-exist"},
				SelectedModelTypeSmall: {Provider: "openai", Model: "also-does-not-exist"},
			},
		}
		cfg.setDefaults(dir, "")
		store := &ConfigStore{config: cfg, globalDataPath: globalPath}
		env := env.NewFromMap(map[string]string{})
		resolver := NewShellVariableResolver(env)
		_, err := cfg.configureProviders(context.Background(), env, resolver, knownProviders)
		require.NoError(t, err)

		// Simulate the Load path: resolve (pure), then persist fallbacks
		// under writeMu using updateLocked. Before the refactor, the
		// combined configureSelectedModels(persist=true) self-deadlocked
		// because SetHelperModel re-acquired writeMu.
		done := make(chan error, 1)
		go func() {
			cfg.takeLegacyChatModel()
			resolved, resolveErr := resolveSelectedModels(cfg, knownProviders)
			if resolveErr != nil {
				done <- resolveErr
				return
			}
			cfg.Models[SelectedModelTypeSmall] = resolved.Small

			store.writeMu.Lock()
			defer store.writeMu.Unlock()
			if resolved.WriteSmall {
				if err := store.updateLocked(ScopeGlobal, func(c *Config) map[string]any {
					return store.helperModelFields(c, resolved.Small)
				}); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
			// Should have fallen back to defaults.
			require.Equal(t, "small-model", cfg.Models[SelectedModelTypeSmall].Model)
		case <-time.After(5 * time.Second):
			t.Fatal("resolve + persist deadlocked under writeMu")
		}
	})
}

// TestHelperModelNeverFollowsAChat: the helper model is set on its own
// (owner, 2026-10-24: no shared model anywhere). An old file's models.large
// does not move it, and with none set it is the default selection's helper,
// written once, which a later chat pick does not change.
func TestHelperModelNeverFollowsAChat(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	knownProviders := []catwalk.Provider{{
		ID:                  "openai",
		APIKey:              "abc",
		DefaultLargeModelID: "gpt-big",
		DefaultSmallModelID: "gpt-mini",
		Models:              []catwalk.Model{{ID: "gpt-big", DefaultMaxTokens: 1000}, {ID: "gpt-mini", DefaultMaxTokens: 500}},
	}}
	load := func(models map[SelectedModelType]SelectedModel) (*Config, resolvedModels) {
		cfg := &Config{
			Models: models,
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"local": {APIKey: "k", BaseURL: "http://127.0.0.1:9/v1", Models: []catwalk.Model{{ID: "y1", DefaultMaxTokens: 600}}},
			}),
		}
		cfg.setDefaults(t.TempDir(), "")
		e := env.NewFromMap(map[string]string{})
		_, err := cfg.configureProviders(context.Background(), e, NewShellVariableResolver(e), knownProviders)
		require.NoError(t, err)
		cfg.takeLegacyChatModel()
		resolved, err := resolveSelectedModels(cfg, knownProviders)
		require.NoError(t, err)
		return cfg, resolved
	}

	// An old file: the shared model on "local", the helper on openai.
	cfg, resolved := load(map[SelectedModelType]SelectedModel{
		"large": {Provider: "local", Model: "y1"},
		"small": {Provider: "openai", Model: "gpt-mini"},
	})
	require.Equal(t, "openai", resolved.Small.Provider, "the helper stays where it was set")
	require.NotContains(t, cfg.Models, SelectedModelType("large"), "no folder-wide model is kept")
	start, ok := cfg.StartModel()
	require.True(t, ok)
	require.Equal(t, "local", start.Provider, "the old value only seeds a new chat")

	// No helper set: the default selection's, written once, and not the
	// old shared model's provider.
	_, resolved = load(map[SelectedModelType]SelectedModel{"large": {Provider: "local", Model: "y1"}})
	require.Equal(t, SelectedModel{Provider: "openai", Model: "gpt-mini", MaxTokens: 500}, resolved.Small)
	require.True(t, resolved.WriteSmall, "the default helper is written once")

	// A chat pick (the last pick) on another provider leaves it alone.
	require.NoError(t, WriteLastPick(SelectedModel{Provider: "local", Model: "y1"}))
	_, again := load(map[SelectedModelType]SelectedModel{"small": resolved.Small})
	require.Equal(t, resolved.Small.Provider, again.Small.Provider)
	require.False(t, again.WriteSmall)
}
