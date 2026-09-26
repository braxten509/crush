package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

// Provider types backed by an installed agent CLI instead of an HTTP API.
// Turns run through the CLI itself (its own tools, login and settings), and
// Crush hands the conversation over when the user switches between them.
const (
	TypeClaudeCode  catwalk.Type = "claude-code"
	TypeCodexCLI    catwalk.Type = "codex-cli"
	TypeGrokCLI     catwalk.Type = "grok-cli"
	TypeAbacusCLI   catwalk.Type = "abacus-cli"
	TypeAGYCLI      catwalk.Type = "agy-cli"
	TypeOpenCodeCLI catwalk.Type = "opencode-cli"
)

// IsCLIProviderType reports whether t is driven through an agent CLI.
func IsCLIProviderType(t catwalk.Type) bool {
	return slices.ContainsFunc(cliProviders, func(p cliProvider) bool { return p.cfg.Type == t })
}

var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

type cliProvider struct {
	bin string
	cfg ProviderConfig
}

// Built-in model lists, used until discovery (cli_models.go) has cached
// what each CLI actually offers. Users can also override them in config.
var cliProviders = []cliProvider{
	{"claude", ProviderConfig{
		ID:       string(TypeClaudeCode),
		Name:     "Claude Code",
		Type:     TypeClaudeCode,
		FlatRate: true,
		Models: []catwalk.Model{
			{ID: "opus", Name: "Claude Opus", ContextWindow: 400_000, DefaultMaxTokens: 32_000, CanReason: true, ReasoningLevels: claudeEfforts, DefaultReasoningEffort: "high", SupportsImages: true},
			{ID: "fable", Name: "Claude Fable", ContextWindow: 400_000, DefaultMaxTokens: 32_000, CanReason: true, ReasoningLevels: claudeEfforts, DefaultReasoningEffort: "high", SupportsImages: true},
			{ID: "sonnet", Name: "Claude Sonnet", ContextWindow: 400_000, DefaultMaxTokens: 32_000, CanReason: true, ReasoningLevels: claudeEfforts, DefaultReasoningEffort: "high", SupportsImages: true},
			{ID: "haiku", Name: "Claude Haiku", ContextWindow: 400_000, DefaultMaxTokens: 32_000, SupportsImages: true},
		},
	}},
	{"codex", ProviderConfig{
		ID:       string(TypeCodexCLI),
		Name:     "Codex",
		Type:     TypeCodexCLI,
		FlatRate: true,
		Models: []catwalk.Model{
			{ID: "gpt-6-astra", Name: "GPT-6 Astra", ContextWindow: 872_000, DefaultMaxTokens: 64_000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultReasoningEffort: "medium"},
			{ID: "gpt-6-sol", Name: "GPT-6 Sol", ContextWindow: 400_000, DefaultMaxTokens: 64_000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultReasoningEffort: "medium"},
			{ID: "gpt-6-luna", Name: "GPT-6 Luna", ContextWindow: 400_000, DefaultMaxTokens: 64_000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"},
			{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", ContextWindow: 400_000, DefaultMaxTokens: 64_000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultReasoningEffort: "medium"},
		},
	}},
	{"grok", ProviderConfig{
		ID:       string(TypeGrokCLI),
		Name:     "Grok",
		Type:     TypeGrokCLI,
		FlatRate: true,
		Models:   cliModels("grok-4.7", "Grok 4.7", "grok-4.7-build-fast", "Grok 4.7 Build Fast", "grok-4.6", "Grok 4.6", "grok-4.5", "Grok 4.5"),
	}},
	{"abacusai", ProviderConfig{
		ID:       string(TypeAbacusCLI),
		Name:     "Abacus",
		Type:     TypeAbacusCLI,
		FlatRate: true,
		Models:   cliModels("ROUTE_LLM", "RouteLLM", "CLAUDE_V5_5_OPUS_THINKING", "Opus 5.5", "OPENAI_GPT6_ASTRA_THINKING", "GPT-6 Astra", "XAI_GROK_4_7", "Grok 4.7", "ROUTE_LLM_LOW", "RouteLLM Low"),
	}},
	{"agy", ProviderConfig{
		ID:       string(TypeAGYCLI),
		Name:     "Antigravity",
		Type:     TypeAGYCLI,
		FlatRate: true,
		Models:   cliModels("gemini-3.1-pro-high", "Gemini 3.1 Pro (High)", "gemini-3.8-flash-high", "Gemini 3.8 Flash (High)", "claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)", "gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"),
	}},
	{"opencode", ProviderConfig{
		ID:       string(TypeOpenCodeCLI),
		Name:     "OpenCode Go",
		Type:     TypeOpenCodeCLI,
		FlatRate: true,
		Models:   cliModels("opencode-go/glm-5.3", "glm-5.3", "opencode-go/kimi-k3", "kimi-k3", "opencode-go/deepseek-v4-pro", "deepseek-v4-pro", "opencode-go/glm-5.3-flash", "glm-5.3-flash"),
	}},
}

// cliModels builds models from id, name pairs. These CLIs don't report
// context sizes, so all get the same generous defaults.
func cliModels(idNames ...string) []catwalk.Model {
	var models []catwalk.Model
	for i := 0; i+1 < len(idNames); i += 2 {
		models = append(models, catwalk.Model{ID: idNames[i], Name: idNames[i+1], ContextWindow: 400_000, DefaultMaxTokens: 32_000})
	}
	return models
}

// Off in tests, so the CLIs installed on the machine running them neither
// leak into provider expectations nor get run.
var (
	detectCLIs    = !testing.Testing()
	refreshModels = !testing.Testing()
)

// Tests must not read the developer's own global config either (this
// fork's setup turns the API providers off there, which many upstream tests
// rely on). Tests that care set these themselves.
func init() {
	if !testing.Testing() {
		return
	}
	for _, key := range []string{"CRUSH_GLOBAL_CONFIG", "CRUSH_GLOBAL_DATA"} {
		if os.Getenv(key) != "" {
			continue
		}
		if dir, err := os.MkdirTemp("", "crush-test-global"); err == nil {
			os.Setenv(key, dir)
		}
	}
}

// addCLIProviders registers the agent CLIs found on path (a PATH list). A
// provider the user configured keeps their settings; missing fields fall
// back to the defaults, so `{"claude-code": {"disable": true}}` is all it
// takes to hide one.
func (c *Config) addCLIProviders(path string) {
	for _, p := range cliProviders {
		cfg, exists := c.Providers.Get(p.cfg.ID)
		if !exists {
			if !detectCLIs || !onPath(path, p.bin) {
				continue
			}
			cfg = p.cfg
			if models := cachedModels(p.cfg.ID); len(models) > 0 {
				cfg.Models = models
			}
			c.Providers.Set(p.cfg.ID, cfg)
			continue
		}
		cfg.Type = p.cfg.Type
		if cfg.Name == "" {
			cfg.Name = p.cfg.Name
		}
		if len(cfg.Models) == 0 {
			cfg.Models = slices.Clone(p.cfg.Models)
		}
		cfg.FlatRate = true
		c.Providers.Set(p.cfg.ID, cfg)
	}
	if detectCLIs && refreshModels {
		maybeRefreshCLIModels(path)
	}
}

// defaultCLIModels picks the first enabled agent CLI's first model as the
// large model and its last (smallest) as the small one.
func (c *Config) defaultCLIModels() (SelectedModel, SelectedModel, bool) {
	for _, p := range cliProviders {
		cfg, ok := c.Providers.Get(p.cfg.ID)
		if !ok || cfg.Disable || len(cfg.Models) == 0 {
			continue
		}
		large, small := cfg.Models[0], cfg.Models[len(cfg.Models)-1]
		return SelectedModel{Provider: cfg.ID, Model: large.ID, MaxTokens: large.DefaultMaxTokens, ReasoningEffort: large.DefaultReasoningEffort},
			SelectedModel{Provider: cfg.ID, Model: small.ID, MaxTokens: small.DefaultMaxTokens, ReasoningEffort: small.DefaultReasoningEffort},
			true
	}
	return SelectedModel{}, SelectedModel{}, false
}

func onPath(path, bin string) bool {
	for _, dir := range filepath.SplitList(path) {
		if info, err := os.Stat(filepath.Join(dir, bin)); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
