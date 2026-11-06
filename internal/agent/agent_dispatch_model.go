package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/config"
)

// A subagent runs on its own chat's model, which starts as its parent chat's
// (CreateTaskSession copies it). There is no shared model to fall back on
// (owner, 2026-10-24: every chat owns its model), so a model override is
// simply a different choice saved with the subagent's chat, and a restart
// finds it there.

// resolveModelOverride turns the dispatch tool's model-override params into
// the model selection a subagent should run on. parent is the model choice of
// the chat that called the tool; it supplies the provider when only a model
// is given. current is the choice the subagent runs on without an override,
// which a reasoning-only override keeps. ok is false when no override was
// requested. A non-empty refusal means one was requested but cannot be
// served; it goes back to the model so the call can be retried.
func (c *coordinator) resolveModelOverride(params AgentDispatchParams, parent, current config.SelectedModel) (sel config.SelectedModel, ok bool, refusal string) {
	provider := strings.TrimSpace(params.Provider)
	model := strings.TrimSpace(params.Model)
	reasoning := strings.TrimSpace(params.Reasoning)
	switch {
	case provider == "" && model == "" && reasoning == "":
		return config.SelectedModel{}, false, ""
	case provider != "" && model == "":
		return config.SelectedModel{}, false, "provider is given without model; pass model as well"
	case model == "":
		// Reasoning alone rides the model the subagent runs on anyway.
		if current.Provider == "" || current.Model == "" {
			return config.SelectedModel{}, false, "the subagent has no model to set reasoning on"
		}
		sel = current
	default:
		if provider == "" {
			if parent.Provider == "" {
				return config.SelectedModel{}, false, "the parent agent has no model to take a provider from"
			}
			provider = parent.Provider
		}
		// A fresh selection: everything else is the model's own default. On
		// the parent's provider it keeps the parent's login, so the subagent
		// runs on the account the chat was set to.
		sel = config.SelectedModel{Provider: provider, Model: model}
		if provider == parent.Provider {
			sel.Account = parent.Account
		}
	}
	catwalkModel, refusal := c.availableModel(sel)
	if refusal == "" {
		refusal = applyReasoning(&sel, catwalkModel, reasoning)
	}
	return sel, true, refusal
}

// availableModel finds a selection's model among what the catalog would list.
func (c *coordinator) availableModel(sel config.SelectedModel) (catwalk.Model, string) {
	cfg := c.cfg.Config()
	provider, ok := cfg.Providers.Get(sel.Provider)
	if !ok || provider.Disable {
		ids := make([]string, 0)
		for _, p := range usableProviders(cfg) {
			ids = append(ids, p.ID)
		}
		return catwalk.Model{}, fmt.Sprintf("provider %q is not available; use one of: %s", sel.Provider, strings.Join(ids, ", "))
	}
	models := provider.UsableModels()
	idx := slices.IndexFunc(models, func(m catwalk.Model) bool { return m.ID == sel.Model })
	if idx < 0 {
		return catwalk.Model{}, fmt.Sprintf("model %q is not available from provider %q; use one of: %s", sel.Model, sel.Provider, modelIDList(models))
	}
	return models[idx], ""
}

// reasoningOptions lists the values a model takes for reasoning: its effort
// levels, or on and off for a model that can only toggle thinking.
func reasoningOptions(m catwalk.Model) []string {
	if levels := m.ReasoningEffortLevels(); len(levels) > 0 {
		return levels
	}
	if m.Reasoning.Thinking == catwalk.ThinkingToggleable {
		return []string{"on", "off"}
	}
	return nil
}

// applyReasoning sets the selection's effort level or thinking toggle,
// whichever the model reasons with.
func applyReasoning(sel *config.SelectedModel, m catwalk.Model, reasoning string) string {
	if reasoning == "" {
		return ""
	}
	options := reasoningOptions(m)
	if !slices.Contains(options, reasoning) {
		if len(options) == 0 {
			return fmt.Sprintf("model %q does not support reasoning", m.ID)
		}
		return fmt.Sprintf("model %q takes reasoning %s", m.ID, strings.Join(options, ", "))
	}
	if len(m.ReasoningEffortLevels()) > 0 {
		sel.ReasoningEffort = reasoning
	} else {
		sel.Think = reasoning == "on"
	}
	return ""
}

// catalogModelsPerProvider keeps one huge catalog from crowding out the prompt.
const catalogModelsPerProvider = 25

// usableProviders lists enabled providers by ID, so the catalog text is stable across turns.
func usableProviders(cfg *config.Config) []config.ProviderConfig {
	providers := cfg.EnabledProviders()
	slices.SortFunc(providers, func(a, b config.ProviderConfig) int { return strings.Compare(a.ID, b.ID) })
	return providers
}

func modelIDList(models []catwalk.Model) string {
	listed := models[:min(len(models), catalogModelsPerProvider)]
	ids := make([]string, 0, len(listed))
	for _, m := range listed {
		if options := reasoningOptions(m); len(options) > 0 {
			ids = append(ids, m.ID+" ["+strings.Join(options, "|")+"]")
		} else {
			ids = append(ids, m.ID)
		}
	}
	list := strings.Join(ids, ", ")
	if extra := len(models) - len(listed); extra > 0 {
		list += fmt.Sprintf(", and %d more", extra)
	}
	return list
}

// dispatchModelCatalog is appended to the dispatch tool's description; empty
// when nothing is usable. parent names the model the caller runs on; the
// tools are shared by every chat, so it is zero there and the header names
// no model.
func dispatchModelCatalog(cfg *config.Config, parent Model) string {
	var b strings.Builder
	for _, p := range usableProviders(cfg) {
		if models := p.UsableModels(); len(models) > 0 {
			fmt.Fprintf(&b, "\n- `%s`: %s", p.ID, modelIDList(models))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	header := "\n\nModels available to `model`, by `provider`"
	if parent.ModelCfg.Provider != "" {
		header += fmt.Sprintf(" (you are running on `%s`)", modelDisplayName(parent))
	}
	return header + ":\n" + b.String()
}

// modelDisplayName names a model as provider/model.
func modelDisplayName(m Model) string {
	return selectionDisplayName(m.ModelCfg)
}

// selectionDisplayName names a model choice as provider/model, or empty for
// no choice.
func selectionDisplayName(sel config.SelectedModel) string {
	if sel.Provider == "" && sel.Model == "" {
		return ""
	}
	return sel.Provider + "/" + sel.Model
}

// checkModelOverride resolves a dispatch's model override and, when one was
// requested, makes sure the model builds, so a refused override costs no
// subagent. A nil selection means none was requested.
func (c *coordinator) checkModelOverride(ctx context.Context, params AgentDispatchParams, parent, current config.SelectedModel) (*config.SelectedModel, string) {
	sel, ok, refusal := c.resolveModelOverride(params, parent, current)
	if refusal != "" || !ok {
		return nil, refusal
	}
	if _, err := c.modelForSelection(ctx, sel); err != nil {
		return nil, "failed to set up the requested model: " + err.Error()
	}
	return &sel, ""
}

// setChatModel saves a model choice with a chat, which then runs on it.
func (c *coordinator) setChatModel(ctx context.Context, sessionID string, sel config.SelectedModel) error {
	choice, err := json.Marshal(sel)
	if err != nil {
		return err
	}
	_, err = c.sessions.SetModel(ctx, sessionID, choice)
	return err
}

// overrideRecord is what a transcript records of an override: only the
// settings the call asked for, with the provider it resolved to.
func overrideRecord(params AgentDispatchParams, sel config.SelectedModel) (model, provider, reasoning string) {
	if strings.TrimSpace(params.Model) != "" {
		model, provider = sel.Model, sel.Provider
	}
	return model, provider, strings.TrimSpace(params.Reasoning)
}

// sendOnNewModel moves an idle subagent to another model and reopens it with
// the message, keeping its session. The new choice is saved with the
// subagent's chat, so it stays there for later runs and restarts.
func (c *coordinator) sendOnNewModel(ctx context.Context, entry *dispatchedAgent, params AgentSendParams, parent config.SelectedModel) fantasy.ToolResponse {
	if entry.stopping() {
		entry.awaitSettled(10 * time.Second)
	}
	running := fmt.Sprintf("subagent %q is running; stop it with agent_stop before moving it to another model", entry.Label)
	if entry.running() {
		return fantasy.NewTextErrorResponse(running)
	}
	override := AgentDispatchParams{Model: params.Model, Provider: params.Provider, Reasoning: params.Reasoning}
	previous, _ := c.chatSelection(ctx, entry.SessionID)
	sel, refusal := c.checkModelOverride(ctx, override, parent, previous)
	if refusal != "" {
		return fantasy.NewTextErrorResponse(refusal)
	}
	if sel == nil {
		return fantasy.NewTextErrorResponse("no model, provider or reasoning was given to move the subagent to")
	}
	if err := c.setChatModel(ctx, entry.SessionID, *sel); err != nil {
		return fantasy.NewTextErrorResponse("Failed to move the subagent: " + err.Error())
	}
	reopened, err := c.reopen(ctx, entry, params.Message)
	if err != nil || !reopened {
		// Not moved after all: it keeps the model it had.
		if previous.Provider != "" {
			if restoreErr := c.setChatModel(context.WithoutCancel(ctx), entry.SessionID, previous); restoreErr != nil {
				return fantasy.NewTextErrorResponse("Failed to move the subagent, and its old model could not be put back: " + restoreErr.Error())
			}
		}
		if err != nil {
			return fantasy.NewTextErrorResponse("Failed to deliver message: " + err.Error())
		}
		return fantasy.NewTextErrorResponse(running)
	}
	meta := AgentSendResponseMetadata{Label: entry.Label, Reopened: true, RunModel: selectionDisplayName(*sel)}
	meta.Model, meta.Provider, meta.Reasoning = overrideRecord(override, *sel)
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(fmt.Sprintf(
		"Moved %q to %s and delivered the message. It will answer in a new report.",
		entry.Label, meta.RunModel,
	)), meta)
}
