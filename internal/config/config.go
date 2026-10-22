package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
)

const (
	appName              = "crush"
	defaultDataDirectory = ".crush"
	defaultInitializeAs  = "AGENTS.md"
)

var defaultContextPaths = []string{
	".github/copilot-instructions.md",
	".cursorrules",
	".cursor/rules/",
	"CLAUDE.md",
	"CLAUDE.local.md",
	"GEMINI.md",
	"gemini.md",
	"crush.md",
	"crush.local.md",
	"Crush.md",
	"Crush.local.md",
	"CRUSH.md",
	"CRUSH.local.md",
	"AGENTS.md",
	"agents.md",
	"Agents.md",
}

type SelectedModelType string

// String returns the string representation of the [SelectedModelType].
func (s SelectedModelType) String() string {
	return string(s)
}

const (
	SelectedModelTypeLarge SelectedModelType = "large"
	SelectedModelTypeSmall SelectedModelType = "small"
)

type SelectedModel struct {
	// The model id as used by the provider API.
	// Required.
	Model string `json:"model" jsonschema:"required,description=The model ID as used by the provider API,example=gpt-4o"`
	// The model provider, same as the key/id used in the providers config.
	// Required.
	Provider string `json:"provider" jsonschema:"required,description=The model provider ID that matches a key in the providers config,example=openai"`

	// Account is the sign-in a Claude Code or Codex model runs on: the ID
	// of an entry in options.accounts, or empty for the tool's own login.
	Account string `json:"account,omitempty" jsonschema:"description=ID of the options.accounts sign-in this Claude Code or Codex model runs on; empty is the tool's own login"`

	// ChosenProvider and ChosenModel are the model the user picked when the
	// fallback order has since moved the chat to another one; empty when
	// this is the picked model. The order follows the picked model's list.
	ChosenProvider string `json:"chosen_provider,omitempty" jsonschema:"description=Provider of the model the user picked, kept while the fallback order runs another"`
	ChosenModel    string `json:"chosen_model,omitempty" jsonschema:"description=The model the user picked, kept while the fallback order runs another"`

	// Only used by models that use the openai provider and need this set.
	ReasoningEffort string `json:"reasoning_effort,omitempty" jsonschema:"description=Reasoning effort level supported by the selected model,enum=none,enum=minimal,enum=low,enum=medium,enum=high,enum=xhigh,enum=max,enum=ultra"`

	// FAST mode: Codex CLI settings or Abacus OpenAI priority processing.
	ServiceTier string `json:"service_tier,omitempty" jsonschema:"description=Codex service tier; omitted inherits the CLI setting,enum=fast,enum=default"`

	// Ultracode: Claude Code's standing dynamic-workflow mode, on at any
	// effort level.
	Ultracode bool `json:"ultracode,omitempty" jsonschema:"description=Turn on Claude Code's Ultracode (dynamic workflows on every task)"`

	// Used by anthropic models that can reason to indicate if the model should think.
	Think bool `json:"think,omitempty" jsonschema:"description=Enable thinking mode for Anthropic models that support reasoning"`

	// Overrides the default model configuration.
	MaxTokens        int64    `json:"max_tokens,omitempty" jsonschema:"description=Maximum number of tokens for model responses,maximum=200000,example=4096"`
	Temperature      *float64 `json:"temperature,omitempty" jsonschema:"description=Sampling temperature,minimum=0,maximum=1,example=0.7"`
	TopP             *float64 `json:"top_p,omitempty" jsonschema:"description=Top-p (nucleus) sampling parameter,minimum=0,maximum=1,example=0.9"`
	TopK             *int64   `json:"top_k,omitempty" jsonschema:"description=Top-k sampling parameter"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty" jsonschema:"description=Frequency penalty to reduce repetition"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty" jsonschema:"description=Presence penalty to increase topic diversity"`

	// Override provider specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for the model"`
}

// Config holds the configuration for crush.
type Config struct {
	Schema string `json:"$schema,omitempty"`

	// We currently only support large/small as values here.
	Models map[SelectedModelType]SelectedModel `json:"models,omitempty" jsonschema:"description=Model configurations for different model types,example={\"large\":{\"model\":\"gpt-4o\",\"provider\":\"openai\"}}"`

	// Recently used models stored in the data directory config.
	RecentModels map[SelectedModelType][]SelectedModel `json:"recent_models,omitempty" jsonschema:"-"`

	// The providers that are configured
	Providers *csync.Map[string, ProviderConfig] `json:"providers,omitempty" jsonschema:"description=AI provider configurations"`

	MCP MCPs `json:"mcp,omitempty" jsonschema:"description=Model Context Protocol server configurations"`

	LSP LSPs `json:"lsp,omitempty" jsonschema:"description=Language Server Protocol configurations"`

	Options *Options `json:"options,omitempty" jsonschema:"description=General application options"`

	Permissions *Permissions `json:"permissions,omitempty" jsonschema:"description=Permission settings for tool usage"`

	Tools Tools `json:"tools,omitzero" jsonschema:"description=Tool configurations"`

	Hooks map[string][]HookConfig `json:"hooks,omitempty" jsonschema:"description=User-defined shell commands that fire on hook events (e.g. PreToolUse)"`

	// Env is a map of environment variables set on startup.
	Env map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set on startup"`

	Agents map[string]Agent `json:"-"`
}

// cloneForWrite returns a copy of c that the store's typed field mutators
// may modify without racing readers of the currently published Config.
//
// Reads of a published Config take no lock beyond the pointer load, so a
// mutator must never write through the live pointer. Instead it clones,
// mutates the clone, and atomically swaps it in. The clone gives fresh
// copies of every field a typed mutator touches in place — Models,
// RecentModels, MCP, and Options (with its nested TUI pointer). Providers
// is a *csync.Map (internally synchronized) and is shared by reference;
// the remaining fields are immutable after load from the mutators'
// standpoint and are likewise shared.
func (c *Config) cloneForWrite() *Config {
	nc := *c
	nc.Models = maps.Clone(c.Models)
	nc.RecentModels = maps.Clone(c.RecentModels)
	nc.MCP = maps.Clone(c.MCP)
	if c.Options != nil {
		opts := *c.Options
		if c.Options.TUI != nil {
			tui := *c.Options.TUI
			opts.TUI = &tui
		}
		nc.Options = &opts
	}
	return &nc
}

// ensureTUI returns c.Options.TUI, allocating Options and TUI as needed so
// callers can assign TUI fields without nil checks.
func (c *Config) ensureTUI() *TUIOptions {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	return c.Options.TUI
}

func (c *Config) EnabledProviders() []ProviderConfig {
	var enabled []ProviderConfig
	for p := range c.Providers.Seq() {
		if !p.Disable {
			enabled = append(enabled, p)
		}
	}
	return enabled
}

// IsConfigured  return true if at least one provider is configured
func (c *Config) IsConfigured() bool {
	return len(c.EnabledProviders()) > 0
}

func (c *Config) GetModel(provider, model string) *catwalk.Model {
	if providerConfig, ok := c.Providers.Get(provider); ok {
		for _, m := range providerConfig.Models {
			if m.ID == model {
				return &m
			}
		}
		for _, m := range providerConfig.ChatGPTModels {
			if m.ID == model {
				return &m
			}
		}
		for _, m := range providerConfig.GrokModels {
			if m.ID == model {
				return &m
			}
		}
	}
	return nil
}

// ValidateReasoningEffort checks that effort is a reasoning level the
// given provider/model supports. It returns an error listing the accepted
// levels when the model cannot use it.
func (c *Config) ValidateReasoningEffort(provider, modelID, effort string) error {
	model := c.GetModel(provider, modelID)
	if model == nil {
		return fmt.Errorf("model %q not found for provider %q", modelID, provider)
	}
	if len(model.ReasoningLevels) == 0 {
		return fmt.Errorf("model %q does not support reasoning effort", modelID)
	}
	if slices.Contains(model.ReasoningLevels, effort) {
		return nil
	}
	return fmt.Errorf(
		"model %q does not support reasoning effort %q, accepted values: %s",
		modelID, effort, strings.Join(model.ReasoningLevels, ", "),
	)
}

// ValidateFastMode checks that the model has a fast mode: Claude Code and
// Codex models do, and so do Abacus's OpenAI priority models.
func (c *Config) ValidateFastMode(provider, modelID string) error {
	providerConfig, ok := c.Providers.Get(provider)
	model := c.GetModel(provider, modelID)
	if !ok || model == nil {
		return fmt.Errorf("model %q not found for provider %q", modelID, provider)
	}
	if SupportsFastMode(providerConfig, *model) {
		return nil
	}
	return fmt.Errorf("model %q has no fast mode; only Claude Code, Codex and Abacus OpenAI priority models do", modelID)
}

// IsModelAvailable returns true if the provider is enabled and the model
// exists in its catalog. Unlike GetModel, it rejects disabled providers.
func (c *Config) IsModelAvailable(provider, model string) bool {
	providerConfig, ok := c.Providers.Get(provider)
	if !ok || providerConfig.Disable {
		return false
	}
	// A model is available in any of the provider's catalogs: the
	// API-key list, or the ChatGPT/Grok subscription list fetched on
	// login. Subscription models live outside Models, so checking only
	// there would reject a model that is selectable and usable.
	for _, m := range providerConfig.Models {
		if m.ID == model {
			return true
		}
	}
	for _, m := range providerConfig.ChatGPTModels {
		if m.ID == model {
			return true
		}
	}
	for _, m := range providerConfig.GrokModels {
		if m.ID == model {
			return true
		}
	}
	return false
}

func (c *Config) GetProviderForModel(modelType SelectedModelType) *ProviderConfig {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	if providerConfig, ok := c.Providers.Get(model.Provider); ok {
		return &providerConfig
	}
	return nil
}

func (c *Config) GetModelByType(modelType SelectedModelType) *catwalk.Model {
	model, ok := c.Models[modelType]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

func (c *Config) LargeModel() *catwalk.Model {
	model, ok := c.Models[SelectedModelTypeLarge]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

func (c *Config) SmallModel() *catwalk.Model {
	model, ok := c.Models[SelectedModelTypeSmall]
	if !ok {
		return nil
	}
	return c.GetModel(model.Provider, model.Model)
}

const maxRecentModelsPerType = 5

func ptrValOr[T any](t *T, el T) T {
	if t == nil {
		return el
	}
	return *t
}
