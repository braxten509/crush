package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// pipelineEnv isolates a Load/ReloadFromDisk round trip: global config and
// data live in throwaway directories, the provider catalog comes from the
// embedded copy (no network), and every env var the configs below set is
// registered with t.Setenv so applyEnv's os.Setenv calls are undone.
//
// No t.Parallel(): these tests Setenv and reset the memoized providers.
type pipelineEnv struct {
	workDir    string
	dataDir    string
	globalDir  string
	globalData string // the global data file (ScopeGlobal writes land here)
}

func newPipelineEnv(t *testing.T) pipelineEnv {
	t.Helper()
	root := t.TempDir()
	e := pipelineEnv{
		workDir:   filepath.Join(root, "work"),
		dataDir:   filepath.Join(root, "work", ".crush"),
		globalDir: filepath.Join(root, "global"),
	}
	dataHome := filepath.Join(root, "data")
	e.globalData = filepath.Join(dataHome, "crush.json")
	for _, d := range []string{e.workDir, e.dataDir, e.globalDir, dataHome} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	t.Setenv("CRUSH_GLOBAL_CONFIG", e.globalDir)
	t.Setenv("CRUSH_GLOBAL_DATA", dataHome)
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("PIPELINE_OPENAI_KEY", "sk-from-env")
	t.Setenv("PIPELINE_A_BASE", "")
	t.Setenv("PIPELINE_B_DERIVED", "")
	// Blank every credential the embedded catalog or the provider gates
	// read, so the host's environment cannot configure extra providers.
	for _, p := range embedded.GetAll() {
		if v, ok := strings.CutPrefix(p.APIKey, "$"); ok {
			t.Setenv(strings.Trim(v, "{}"), "")
		}
	}
	for _, k := range []string{
		"HYPER_API_KEY", "VERTEXAI_PROJECT", "VERTEXAI_LOCATION",
		"AWS_BEARER_TOKEN_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI",
	} {
		t.Setenv(k, "")
	}
	resetProviderState()
	t.Cleanup(resetProviderState)
	return e
}

func (e pipelineEnv) write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// writeRichConfig lays down a global config, a project crush.json, a
// project crushrc and a workspace overlay that together exercise every
// stage of the load pipeline: env application, hook normalization, known
// and custom providers, header resolution, MCP/LSP defaults, explicit
// model selection, and the workspace merge.
func (e pipelineEnv) writeRichConfig(t *testing.T) {
	t.Helper()
	e.write(t, filepath.Join(e.globalDir, "crush.json"), `{
		"env": {
			"PIPELINE_A_BASE": "configured",
			"PIPELINE_B_DERIVED": "$PIPELINE_A_BASE-derived"
		},
		"options": {"context_paths": ["GLOBAL.md"]},
		"hooks": {
			"pre_tool_use": [{"command": "true", "matcher": "^bash$"}]
		},
		"lsp": {"gopls": {"command": "gopls"}}
	}`)
	e.write(t, filepath.Join(e.workDir, "crush.json"), `{
		"models": {
			"large": {"provider": "openai", "model": "gpt-4o", "max_tokens": 1234},
			"small": {"provider": "local", "model": "tiny"}
		},
		"providers": {
			"openai": {
				"api_key": "$PIPELINE_OPENAI_KEY",
				"extra_headers": {"X-Static": "static", "X-Empty": "$PIPELINE_UNSET_VAR"}
			},
			"local": {
				"type": "openai-compat",
				"base_url": "http://127.0.0.1:1/v1",
				"api_key": "local-key",
				"extra_headers": {"X-Derived": "$PIPELINE_B_DERIVED"},
				"models": [{"id": "tiny", "name": "Tiny"}, {"id": "big"}]
			},
			"broken": {
				"type": "not-a-real-type",
				"base_url": "http://127.0.0.1:2",
				"models": [{"id": "x"}]
			}
		},
		"mcp": {
			"files": {"type": "stdio", "command": "mcp-files", "args": ["--root", "."]}
		}
	}`)
	e.write(t, filepath.Join(e.workDir, "crushrc"), "option notifications bell\n")
	e.write(t, filepath.Join(e.dataDir, "crush.json"), `{
		"options": {"tui": {"compact_mode": true}}
	}`)
}

// pipelineSnapshot is everything Load and ReloadFromDisk publish on the
// store, in a comparable form.
type pipelineSnapshot struct {
	Config         string
	Agents         map[string]Agent
	LoadedPaths    []string
	WorkspacePath  string
	KnownProviders []string
	TrackedPaths   []string
}

func snapshotStore(t *testing.T, s *ConfigStore) pipelineSnapshot {
	t.Helper()
	cfg := s.Config()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var ids []string
	for _, p := range s.KnownProviders() {
		ids = append(ids, string(p.ID))
	}
	return pipelineSnapshot{
		Config:         string(data),
		Agents:         cfg.Agents,
		LoadedPaths:    s.LoadedPaths(),
		WorkspacePath:  s.workspacePath,
		KnownProviders: ids,
		TrackedPaths:   slices.Clone(s.trackedConfigPaths),
	}
}

// TestLoadPipeline_ReloadMatchesLoad characterizes the two entry points to
// the load pipeline: given the same files and environment, Load and a
// subsequent ReloadFromDisk must publish the same configuration.
func TestLoadPipeline_ReloadMatchesLoad(t *testing.T) {
	e := newPipelineEnv(t)
	e.writeRichConfig(t)

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	loaded := snapshotStore(t, store)

	require.NoError(t, store.ReloadFromDisk(context.Background()))
	reloaded := snapshotStore(t, store)

	require.Equal(t, loaded, reloaded)

	// Spot-check that the pipeline actually ran every stage, so the
	// equality above is not vacuous.
	cfg := store.Config()
	require.Equal(t, "configured", os.Getenv("PIPELINE_A_BASE"))
	require.Contains(t, cfg.Hooks, "PreToolUse")
	require.True(t, cfg.Options.TUI.CompactMode, "workspace overlay must apply")
	require.Equal(t, "bell", cfg.Options.Notifications, "crushrc must apply")
	require.Contains(t, loaded.LoadedPaths, filepath.Join(e.dataDir, "crush.json"))

	openai, ok := cfg.Providers.Get("openai")
	require.True(t, ok)
	require.Equal(t, "$PIPELINE_OPENAI_KEY", openai.APIKey)
	require.Equal(t, "$PIPELINE_OPENAI_KEY", openai.APIKeyTemplate)
	require.Equal(t, "static", openai.ExtraHeaders["X-Static"])
	require.NotContains(t, openai.ExtraHeaders, "X-Empty")
	require.NotEmpty(t, openai.Models)

	local, ok := cfg.Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, "local", local.ID)
	require.Equal(t, "local", local.Name)
	require.Equal(t, "configured-derived", local.ExtraHeaders["X-Derived"])

	_, ok = cfg.Providers.Get("broken")
	require.False(t, ok, "unsupported custom provider types are dropped")

	large, _ := cfg.StartModel()
	require.Equal(t, "openai", large.Provider)
	require.Equal(t, "gpt-4o", large.Model)
	require.EqualValues(t, 1234, large.MaxTokens)
	small := cfg.Models[SelectedModelTypeSmall]
	require.Equal(t, "local", small.Provider)
	require.Equal(t, "tiny", small.Model)

	require.Contains(t, cfg.Agents, AgentCoder)
	require.NotEmpty(t, cfg.LSP["gopls"].FileTypes, "LSP defaults must apply")
}

// TestLoadPipeline_AnthropicOAuthRemovedOnDisk characterizes the one disk
// write configureProviders requests: a stale Claude Code subscription
// (an Anthropic provider carrying an OAuth token) is dropped from memory
// and deleted from the global data file, on load and on reload alike.
func TestLoadPipeline_AnthropicOAuthRemovedOnDisk(t *testing.T) {
	e := newPipelineEnv(t)
	const stale = `{
		"providers": {
			"anthropic": {
				"api_key": "token",
				"oauth": {"access_token": "token", "refresh_token": "refresh"}
			},
			"openai": {"api_key": "sk-test"}
		}
	}`
	e.write(t, e.globalData, stale)

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	_, ok := store.Config().Providers.Get("anthropic")
	require.False(t, ok)
	data, err := os.ReadFile(e.globalData)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(data, "providers.anthropic").Exists(), "load must remove the stale provider on disk")
	require.True(t, gjson.GetBytes(data, "providers.openai.api_key").Exists(), "unrelated providers stay on disk")

	e.write(t, e.globalData, stale)
	require.NoError(t, store.ReloadFromDisk(context.Background()))
	_, ok = store.Config().Providers.Get("anthropic")
	require.False(t, ok)
	data, err = os.ReadFile(e.globalData)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(data, "providers.anthropic").Exists(), "reload must remove the stale provider on disk")
}

// TestConfigureProviders_AnthropicOAuthIsPendingWrite verifies that
// configureProviders leaves the stale-subscription removal to its caller
// instead of writing to disk itself.
func TestConfigureProviders_AnthropicOAuthIsPendingWrite(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"anthropic": {APIKey: "token", OAuthToken: &oauth.Token{AccessToken: "token"}},
		}),
	}
	cfg.setDefaults(t.TempDir(), "")
	vars := env.NewFromMap(map[string]string{})
	known := []catwalk.Provider{{ID: catwalk.InferenceProviderAnthropic, APIKey: "$ANTHROPIC_API_KEY"}}

	pending, err := cfg.configureProviders(t.Context(), vars, NewShellVariableResolver(vars), known)
	require.NoError(t, err)
	require.Equal(t, []pendingWrite{{Scope: ScopeGlobal, Remove: "providers.anthropic"}}, pending)
	_, ok := cfg.Providers.Get("anthropic")
	require.False(t, ok)
}

// TestLoadPipeline_ModelFallbackPersistence characterizes how an unknown
// model selection is handled: both entry points fall back to the
// provider's default in memory, but only Load persists the correction.
// A reload must not write the fallback, because the global config file is
// shared with sibling instances that may know providers this one does not.
func TestLoadPipeline_ModelFallbackPersistence(t *testing.T) {
	e := newPipelineEnv(t)
	const unknown = `{
		"models": {"small": {"provider": "openai", "model": "does-not-exist"}},
		"providers": {"openai": {"api_key": "sk-test"}}
	}`
	e.write(t, e.globalData, unknown)

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	small := store.Config().Models[SelectedModelTypeSmall]
	require.Equal(t, "openai", small.Provider)
	require.NotEqual(t, "does-not-exist", small.Model)
	data, err := os.ReadFile(e.globalData)
	require.NoError(t, err)
	require.Equal(t, small.Model, gjson.GetBytes(data, "models.small.model").String(), "load persists the fallback")

	// Reset the pin Load recorded so the reload sees the file's choice.
	store.overrides.Models = nil
	e.write(t, e.globalData, unknown)
	require.NoError(t, store.ReloadFromDisk(context.Background()))
	require.Equal(t, small.Model, store.Config().Models[SelectedModelTypeSmall].Model)
	data, err = os.ReadFile(e.globalData)
	require.NoError(t, err)
	require.Equal(t, "does-not-exist", gjson.GetBytes(data, "models.small.model").String(), "reload must not persist the fallback")
}

// TestLoadPipeline_UnconfiguredLoadsWithoutAgents characterizes a load
// with no usable provider: it succeeds, publishes the config without
// agents, and leaves the model selection untouched.
func TestLoadPipeline_UnconfiguredLoadsWithoutAgents(t *testing.T) {
	e := newPipelineEnv(t)
	e.write(t, e.globalData, `{"options": {"disable_default_providers": false}}`)
	t.Setenv("CRUSH_DISABLE_DEFAULT_PROVIDERS", "")

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	require.False(t, store.Config().IsConfigured())
	require.Empty(t, store.Config().Agents)

	require.NoError(t, store.ReloadFromDisk(context.Background()))
	require.False(t, store.Config().IsConfigured())
	require.Empty(t, store.Config().Agents)
}

// TestLoadPipeline_UnconfiguredLoadTracksStaleness is a regression test for
// Load skipping the staleness snapshot when no provider was usable, which
// left a later edit to the config files undetected until the first reload.
func TestLoadPipeline_UnconfiguredLoadTracksStaleness(t *testing.T) {
	e := newPipelineEnv(t)
	t.Setenv("CRUSH_DISABLE_DEFAULT_PROVIDERS", "")
	projectConfig := filepath.Join(e.workDir, "crush.json")
	e.write(t, projectConfig, `{"options": {"debug": false}}`)

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	require.False(t, store.Config().IsConfigured())
	require.False(t, store.ConfigStaleness().Dirty)

	e.write(t, projectConfig, `{"options": {"debug": true, "context_paths": ["X.md"]}}`)
	result := store.ConfigStaleness()
	require.True(t, result.Dirty)
	require.Contains(t, result.Changed, projectConfig)
}

// TestLoadPipeline_ReloadKeepsDebugFlag is a regression test for a reload
// dropping the --debug flag: Load forced Options.Debug on, but the reload
// path rebuilt the config from the files alone, so any config write
// silently turned off debug logging for the rest of the session.
func TestLoadPipeline_ReloadKeepsDebugFlag(t *testing.T) {
	e := newPipelineEnv(t)
	e.writeRichConfig(t)

	store, err := Load(e.workDir, e.dataDir, true)
	require.NoError(t, err)
	require.True(t, store.Config().Options.Debug)

	require.NoError(t, store.ReloadFromDisk(context.Background()))
	require.True(t, store.Config().Options.Debug, "reload must keep the --debug flag")
}

// TestLoadPipeline_ReloadKeepsEnvironmentDefaults is a regression test for
// a reload dropping the defaults Load derives from where Crush runs rather
// than from the config files: the bounded file walks outside a git working
// tree and Apple Terminal's transparent background.
func TestLoadPipeline_ReloadKeepsEnvironmentDefaults(t *testing.T) {
	e := newPipelineEnv(t)
	e.writeRichConfig(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	require.True(t, store.Config().Options.TUI.IsTransparent())

	// The test process runs inside a git working tree, so simulate the
	// outside case the way Load records it.
	store.outsideWorktree = true
	require.NoError(t, store.ReloadFromDisk(context.Background()))

	cfg := store.Config()
	require.True(t, cfg.Options.TUI.IsTransparent(), "reload must keep Apple Terminal transparency")
	depth, items := cfg.Tools.Ls.Limits()
	require.Equal(t, 2, depth)
	require.Equal(t, 100, items)
	depth, items = cfg.Options.TUI.Completions.Limits()
	require.Equal(t, 2, depth)
	require.Equal(t, 100, items)
}

// TestReadConfig_EnvironmentDefaultsYieldToConfig verifies that the
// environment-derived defaults never override explicit settings.
func TestReadConfig_EnvironmentDefaultsYieldToConfig(t *testing.T) {
	e := newPipelineEnv(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	e.write(t, filepath.Join(e.workDir, "crush.json"), `{
		"options": {"tui": {"transparent": false, "completions": {"max_depth": 7}}},
		"tools": {"ls": {"max_items": 9}}
	}`)

	cfg, _, err := readConfig(t.Context(), buildParams{
		workingDir:      e.workDir,
		dataDir:         e.dataDir,
		outsideWorktree: true,
	})
	require.NoError(t, err)
	require.False(t, cfg.Options.TUI.IsTransparent())
	depth, items := cfg.Options.TUI.Completions.Limits()
	require.Equal(t, 7, depth)
	require.Equal(t, 100, items)
	depth, items = cfg.Tools.Ls.Limits()
	require.Equal(t, 2, depth)
	require.Equal(t, 9, items)
}
