package agent

import (
	"cmp"
	"context"
	"fmt"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/config"
)

func (c *coordinator) buildAgent(ctx context.Context, prompt *prompt.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	helper, err := c.buildHelperModel(ctx)
	if err != nil {
		return nil, err
	}

	var tasks *taskHub
	if agent.ID == config.AgentCoder {
		tasks = c.tasks
	}
	// No model of the agent's own: every chat runs on the model it owns
	// (owner, 2026-10-24: no shared model anywhere), and the system prompt
	// prefix comes from that model's provider (promptPrefixFor).
	result := NewSessionAgent(SessionAgentOptions{
		HelperModel:          helper,
		SystemPrompt:         "",
		IsSubAgent:           isSubAgent,
		Tasks:                tasks,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Cfg:                  c.cfg,
		Tools:                nil,
		Notify:               c.notify,
		RunComplete:          c.runComplete,
		ModelFor:             c.modelForSelection,
		NewChatModel: func() (config.SelectedModel, bool) {
			return config.NewChatModel(c.cfg.Config())
		},
	})

	// The readiness goroutines below perform one-time setup — building the
	// system prompt and the initial tool list — whose results the
	// coordinator needs for its whole lifetime, so they must survive the
	// caller's context being canceled. Several entry points build an agent
	// from a short-lived HTTP request context: the server's
	// InitAgent/UpdateAgent handlers, and UpdateModels -> buildTools ->
	// agentTool -> buildAgent for the sub-agent. The tool-list build reads
	// the MCP registry as it stands; servers still connecting are picked up
	// by later runs. WithoutCancel drops cancellation while keeping context
	// values; the work is local and always completes.
	initCtx := context.WithoutCancel(ctx)

	c.readyWg.Go(func() error {
		// The prompt reads no model: one prompt serves every chat's.
		systemPrompt, err := prompt.Build(initCtx, "", "", c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	c.readyWg.Go(func() error {
		tools, err := c.buildTools(initCtx, agent, isSubAgent)
		if err != nil {
			return err
		}
		result.SetTools(tools)
		return nil
	})

	return result, nil
}

// buildHelperModel builds the helper model (titles, branch summaries, web
// fetches), set in Settings and never following a chat's model.
func (c *coordinator) buildHelperModel(ctx context.Context) (Model, error) {
	helperCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, errHelperModelNotSelected
	}
	providerCfg, ok := c.cfg.Config().Providers.Get(helperCfg.Provider)
	if !ok {
		return Model{}, errHelperModelProviderNotConfigured
	}
	provider, err := c.providerBuilder().Provider(providerCfg, helperCfg, true)
	if err != nil {
		return Model{}, err
	}
	// Resolve the selected model from the provider's whole catalog: an
	// API-key model, a ChatGPT subscription model, or a Grok one. The
	// subscription catalogs live outside Models, so looking only there
	// strands a Grok/ChatGPT model that catwalk doesn't also publish.
	catwalkModel := c.cfg.Config().GetModel(helperCfg.Provider, helperCfg.Model)
	if catwalkModel == nil {
		return Model{}, errHelperModelNotFound
	}
	languageModel, err := c.providerBuilder().LanguageModel(ctx, provider, providerCfg, helperCfg)
	if err != nil {
		return Model{}, err
	}
	return Model{
		Model:      languageModel,
		CatwalkCfg: *catwalkModel,
		ModelCfg:   helperCfg,
		FlatRate:   providerCfg.FlatRate,
	}, nil
}

// modelForSelection builds one model from a saved choice (a chat's own
// model), for a turn on that chat.
func (c *coordinator) modelForSelection(ctx context.Context, selection config.SelectedModel) (Model, error) {
	providerCfg, ok := c.cfg.Config().Providers.Get(selection.Provider)
	if !ok {
		return Model{}, fmt.Errorf("provider %q is not configured", selection.Provider)
	}
	provider, err := c.providerBuilder().Provider(providerCfg, selection, false)
	if err != nil {
		return Model{}, err
	}
	catwalkModel := c.cfg.Config().GetModel(selection.Provider, selection.Model)
	if catwalkModel == nil {
		return Model{}, fmt.Errorf("model %q is not offered by %q", selection.Model, selection.Provider)
	}
	languageModel, err := c.providerBuilder().LanguageModel(ctx, provider, providerCfg, selection)
	if err != nil {
		return Model{}, err
	}
	return Model{Model: languageModel, CatwalkCfg: *catwalkModel, ModelCfg: selection, FlatRate: providerCfg.FlatRate}, nil
}

// providerBuilder builds providers and models from the live config.
func (c *coordinator) providerBuilder() providers.Builder {
	return providers.Builder{Config: c.cfg, Permissions: c.permissions, History: c.history}
}

// getProviderOptions returns the provider options for a call to model.
func getProviderOptions(model Model, providerCfg config.ProviderConfig) fantasy.ProviderOptions {
	return providers.Options(model.CatwalkCfg, model.ModelCfg, providerCfg)
}

func mergeCallOptions(model Model, cfg config.ProviderConfig) (fantasy.ProviderOptions, *float64, *float64, *int64, *float64, *float64) {
	modelOptions := getProviderOptions(model, cfg)
	temp := cmp.Or(model.ModelCfg.Temperature, model.CatwalkCfg.Options.Temperature)
	topP := cmp.Or(model.ModelCfg.TopP, model.CatwalkCfg.Options.TopP)
	topK := cmp.Or(model.ModelCfg.TopK, model.CatwalkCfg.Options.TopK)
	freqPenalty := cmp.Or(model.ModelCfg.FrequencyPenalty, model.CatwalkCfg.Options.FrequencyPenalty)
	presPenalty := cmp.Or(model.ModelCfg.PresencePenalty, model.CatwalkCfg.Options.PresencePenalty)
	return modelOptions, temp, topP, topK, freqPenalty, presPenalty
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// A ChatGPT or Grok login without its model catalog — the fetch at
	// login failed, or the credentials predate it — would leave the
	// models dialog's subscription section empty. Fill it in lazily; the
	// guards make this a no-op once a catalog exists.
	c.cfg.RefetchOpenAIChatGPTModels(ctx)
	c.cfg.RefetchGrokModels(ctx)

	// A refreshed sign-in or key, or a changed provider, is only in a model
	// built after it, so the chats build theirs again. A running turn keeps
	// its choice and picks the new build up at its next request.
	c.forgetChatModels()

	// A model switch in another chat must not swap the model under a run
	// that is already streaming: the in-flight request would go out on the
	// new model, which may be one the provider cannot serve. The next turn
	// rebuilds models from config anyway (runTurn calls updateAgentModels),
	// so the switch still lands as soon as the run finishes.
	if c.IsBusy() {
		return nil
	}

	agent, name := c.activeAgent()
	return c.updateAgentModels(ctx, agent, name)
}

// updateAgentModels rebuilds the model and tool configuration for the
// given agent from the current config.
func (c *coordinator) updateAgentModels(ctx context.Context, agent SessionAgent, name string) error {
	// build the helper again so we make sure we get the latest config
	helper, err := c.buildHelperModel(ctx)
	if err != nil {
		return err
	}
	agent.SetHelperModel(helper)

	agentCfg, ok := c.cfg.Config().Agents[name]
	if !ok {
		return fmt.Errorf("%w: %s", errMainAgentNotFound, name)
	}

	tools, err := c.buildTools(ctx, agentCfg, false)
	if err != nil {
		return err
	}
	agent.SetTools(tools)
	return nil
}
