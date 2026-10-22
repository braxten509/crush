package model

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/crush/internal/ui/dialog/auth"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// refreshHyperAndRetrySelect returns a command that silently refreshes
// the Hyper OAuth token and then re-runs the model selection. If the
// refresh fails, the selection resumes with ReAuthenticate set so the
// OAuth dialog opens.
func (m *UI) refreshHyperAndRetrySelect(msg dialog.ActionSelectModel) tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := ws.RefreshOAuthToken(ctx, config.ScopeGlobal, "hyper"); err != nil {
			slog.Warn("Hyper OAuth refresh failed, requesting re-auth", "error", err)
			msg.ReAuthenticate = true
		}
		return hyperRefreshDoneMsg{action: msg}
	}
}

// restoreModelFromSession checks the last assistant message in the
// loaded session and, if it used a different provider/model than the
// current config, restores that model/provider provided it is still
// available. Returns a tea.Cmd that rebuilds the agent models if a
// switch was made, or nil if no switch was needed.
func (m *UI) restoreModelFromSession(msgs []message.Message) tea.Cmd {
	var lastAssistant *message.Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.Assistant && !msgs[i].IsSummaryMessage {
			lastAssistant = &msgs[i]
			break
		}
	}
	if lastAssistant == nil || lastAssistant.Provider == "" || lastAssistant.Model == "" {
		return nil
	}

	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}

	currentLarge := cfg.Models[config.SelectedModelTypeLarge]
	if currentLarge.Provider == lastAssistant.Provider && currentLarge.Model == lastAssistant.Model {
		return nil
	}

	if !cfg.IsModelAvailable(lastAssistant.Provider, lastAssistant.Model) {
		slog.Debug("Skipping model restoration: provider/model not available",
			"provider", lastAssistant.Provider,
			"model", lastAssistant.Model)
		return nil
	}

	// The model comes back with the effort last used with it.
	selectedModel := cfg.Remembered(lastAssistant.Provider, lastAssistant.Model)
	_, hasSmall := cfg.Models[config.SelectedModelTypeSmall]
	ws := m.com.Workspace
	// The preference writes run off the Update goroutine; the theme
	// follows the provider and the coordinator rebuilds once they landed.
	return savePreferredModel(ws, config.SelectedModelTypeLarge, selectedModel, !hasSmall, func(err, smallErr error) tea.Cmd {
		if err != nil {
			slog.Error("Failed to restore model from session", "error", err)
			return nil
		}
		m.applyThemeForProvider(selectedModel.Provider)
		if smallErr != nil {
			slog.Error("Failed to set small model during session restore", "error", smallErr)
		}
		return m.updateAgentModelCmd(func() tea.Msg {
			if err := ws.UpdateAgentModel(context.TODO()); err != nil {
				return util.ReportError(err)()
			}
			slog.Info("Restored model from session",
				"provider", selectedModel.Provider,
				"model", selectedModel.Model)
			return nil
		})
	})
}

// importCopilotAndRetrySelect returns a command that imports GitHub Copilot
// tokens from VSCode, if available, and then resumes the model selection.
func importCopilotAndRetrySelect(ws workspace.Workspace, msg dialog.ActionSelectModel) tea.Cmd {
	return func() tea.Msg {
		ws.ImportCopilot()
		return copilotImportDoneMsg{action: msg}
	}
}

// handleSelectModel performs the model selection after any provider
// pre-checks (such as a silent Hyper OAuth refresh) have completed.
func (m *UI) handleSelectModel(msg dialog.ActionSelectModel) tea.Cmd {
	return m.selectModel(msg, false)
}

// selectModel performs the model selection. copilotImported is set once
// the Copilot token import ran for this selection.
func (m *UI) selectModel(msg dialog.ActionSelectModel, copilotImported bool) tea.Cmd {
	var cmds []tea.Cmd

	// we ignore dialogs with the oauth id as they need to be able to be dismissed
	if m.isAgentBusy() && !m.dialog.ContainsDialog(dialog.OAuthID) {
		return util.ReportWarn("Agent is busy, please wait...")
	}

	cfg := m.com.Config()
	if cfg == nil {
		return util.ReportError(errors.New("configuration not found"))
	}

	var (
		providerID   = msg.Model.Provider
		isCopilot    = providerID == string(catwalk.InferenceProviderCopilot)
		isConfigured = func() bool { _, ok := cfg.Providers.Get(providerID); return ok }
		isOnboarding = m.state == uiOnboarding
	)

	// For Hyper, if the stored OAuth token is expired, try a silent
	// refresh before deciding whether the provider is configured. Keeps
	// users from hitting a 401 on their first message after the
	// short-lived access token ages out.
	if !msg.ReAuthenticate && providerID == "hyper" {
		if pc, ok := cfg.Providers.Get(providerID); ok && pc.OAuthToken != nil && pc.OAuthToken.IsExpired() {
			return m.refreshHyperAndRetrySelect(msg)
		}
	}

	// Attempt to import GitHub Copilot tokens from VSCode if available.
	// The import reads and writes files, so it runs off the Update
	// goroutine and the selection resumes once it finished.
	if isCopilot && !isConfigured() && !msg.ReAuthenticate && !copilotImported {
		return importCopilotAndRetrySelect(m.com.Workspace, msg)
	}

	// The OpenAI and xAI providers hold exactly one credential: an
	// account login or an API key. The empty model ID marks the OAuth
	// flow's hand-off message (sign-in completed, or the method choice
	// going to OAuth), and a catalog model needs one of the credentials
	// before it can serve.
	if providerID == string(catwalk.InferenceProviderOpenAI) ||
		providerID == string(catwalk.InferenceProviderXAI) {
		providerCfg, _ := cfg.Providers.Get(providerID)
		if msg.Model.Model == "" {
			m.dialog.CloseDialog(dialog.ModelsID)
			if providerCfg.OAuthToken != nil && !msg.ReAuthenticate {
				// A sign-in just completed: reopen the list so the user
				// can pick from the now-available catalog.
				m.dialog.CloseDialog(dialog.OAuthID)
				if cmd := m.openModelsDialog(); cmd != nil {
					return cmd
				}
				return nil
			}
			return m.openAuthenticationDialog(msg.Provider, msg.Model, msg.ModelType)
		}
		if providerCfg.OAuthToken == nil && !providerCfg.HasAPIKey(m.com.Workspace.Resolver()) {
			m.dialog.CloseDialog(dialog.ModelsID)
			return m.openAuthenticationDialog(msg.Provider, msg.Model, msg.ModelType)
		}
	}

	if !isConfigured() || msg.ReAuthenticate {
		m.dialog.CloseDialog(dialog.ModelsID)
		if cmd := m.openAuthenticationDialog(msg.Provider, msg.Model, msg.ModelType); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	}

	// A ChatGPT or Grok sign-in swaps the provider's catalog for the one
	// the account serves, so a model chosen before the flow may no longer
	// exist afterward. Apply the remembered choice only when the refreshed
	// catalog still offers it; otherwise reopen the list and say so rather
	// than silently falling back to a default.
	if providerID == string(catwalk.InferenceProviderOpenAI) ||
		providerID == string(catwalk.InferenceProviderXAI) {
		if !cfg.IsModelAvailable(providerID, msg.Model.Model) {
			m.dialog.CloseDialog(dialog.ModelsID)
			cmds = append(cmds, util.ReportError(fmt.Errorf(
				"%s isn't offered by your %s account; choose another model",
				msg.Model.Model, cmp.Or(msg.Provider.Name, providerID),
			)))
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return tea.Batch(cmds...)
		}
	}

	// The dialogs close (and onboarding ends) at once; the preference
	// writes run off the Update goroutine and finishModelSelection picks
	// up once they landed.
	// A picked model comes with the effort last used with it, not its
	// default (owner, 2026-10-22).
	if effort := cfg.ModelEffort(providerID, msg.Model.Model); effort != "" {
		msg.Model.ReasoningEffort = effort
	}
	_, hasSmall := cfg.Models[config.SelectedModelTypeSmall]
	// Ensure the small model is set when it is unset.
	fillSmall := !hasSmall && msg.ModelType != config.SelectedModelTypeSmall
	cmds = append(cmds, savePreferredModel(m.com.Workspace, msg.ModelType, msg.Model, fillSmall, func(err, smallErr error) tea.Cmd {
		return m.finishModelSelection(msg, isOnboarding, err, smallErr)
	}))

	m.dialog.CloseDialog(dialog.APIKeyInputID)
	m.dialog.CloseDialog(dialog.OAuthID)
	m.dialog.CloseDialog(dialog.ModelsID)

	if isOnboarding {
		m.setState(uiLanding, uiFocusEditor)
	}

	return tea.Batch(cmds...)
}

// finishModelSelection runs on the Update goroutine once the preference
// writes of a model selection landed: it reports their errors, swaps the
// theme, finishes onboarding and rebuilds the coordinator's model.
func (m *UI) finishModelSelection(msg dialog.ActionSelectModel, isOnboarding bool, err, smallErr error) tea.Cmd {
	var cmds []tea.Cmd
	if err != nil {
		cmds = append(cmds, util.ReportError(err))
	} else {
		if msg.ModelType == config.SelectedModelTypeLarge {
			// Swap the theme live based on the newly selected large
			// model's provider. Skipped when the provider resolves to
			// the already-active theme, which avoids a full markdown
			// re-render of the transcript on every selection.
			m.applyThemeForProvider(msg.Model.Provider)
		}
		if smallErr != nil {
			cmds = append(cmds, util.ReportError(smallErr))
		}
	}

	ws := m.com.Workspace
	modelName := msg.Model.Model
	if cfg := m.com.Config(); cfg != nil {
		if catwalkModel := cfg.GetModel(msg.Model.Provider, msg.Model.Model); catwalkModel != nil && catwalkModel.Name != "" {
			modelName = catwalkModel.Name
		}
	}
	modelMsg := fmt.Sprintf("%s model changed to %s", stringext.Capitalize(string(msg.ModelType)), modelName)
	updateModel := m.updateAgentModelCmd(func() tea.Msg {
		if err := ws.UpdateAgentModel(context.TODO()); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg(modelMsg)
	})

	if !isOnboarding {
		cmds = append(cmds, updateModel)
		if m.com.IsHyper() {
			cmds = append(cmds, m.fetchHyperCredits())
		}
		return tea.Batch(cmds...)
	}

	// Onboarding: set the agents up for the selected model and start the
	// coder before rebuilding its model.
	m.com.Config().SetupAgents()
	initCoder := doThen(func() error {
		return ws.InitCoderAgent(context.TODO())
	}, func(err error) tea.Cmd {
		var cmds []tea.Cmd
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
		}
		// The agent just came up: re-fetch the memoized ready/model
		// state so the landing view shows the selected model without
		// waiting for the TTL backstop.
		m.invalidateBusyCaches()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	})
	cmds = append(cmds, tea.Sequence(initCoder, updateModel))
	return tea.Batch(cmds...)
}

func (m *UI) openAuthenticationDialog(provider catwalk.Provider, model config.SelectedModel, modelType config.SelectedModelType) tea.Cmd {
	var (
		dlg dialog.Dialog
		cmd tea.Cmd

		isOnboarding = m.state == uiOnboarding
	)

	switch provider.ID {
	case "hyper":
		dlg, cmd = auth.NewOAuthHyper(m.com, isOnboarding, provider, model, modelType)
	case catwalk.InferenceProviderCopilot:
		dlg, cmd = auth.NewOAuthCopilot(m.com, isOnboarding, provider, model, modelType)
	case catwalk.InferenceProviderOpenAI:
		providerCfg, _ := m.com.Config().Providers.Get(string(provider.ID))
		hasAPIKey := providerCfg.HasAPIKey(m.com.Workspace.Resolver())
		switch {
		case model.Model == "" || providerCfg.OAuthToken != nil:
			// The sign-in placeholder, or a re-authentication while the
			// ChatGPT login is the credential in force.
			dlg, cmd = auth.NewOAuthOpenAI(m.com, isOnboarding, provider, model, modelType)
		case !hasAPIKey:
			// No credential at all: let the user pick the method.
			dlg = auth.NewAuthMethod(m.com, isOnboarding, provider, model, modelType)
		default:
			// An API key is the credential in force: edit it.
			dlg, cmd = auth.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)
		}
	case catwalk.InferenceProviderXAI:
		providerCfg, _ := m.com.Config().Providers.Get(string(provider.ID))
		hasAPIKey := providerCfg.HasAPIKey(m.com.Workspace.Resolver())
		switch {
		case model.Model == "" || providerCfg.OAuthToken != nil:
			// The sign-in flow's hand-off, or a re-authentication while
			// the Grok login is the credential in force.
			dlg, cmd = auth.NewOAuthGrok(m.com, isOnboarding, provider, model, modelType)
		case !hasAPIKey:
			// No credential at all: let the user pick the method.
			dlg = auth.NewAuthMethod(m.com, isOnboarding, provider, model, modelType)
		default:
			// An API key is the credential in force: edit it.
			dlg, cmd = auth.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)
		}
	default:
		dlg, cmd = auth.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)
	}

	if m.dialog.ContainsDialog(dlg.ID()) {
		m.dialog.BringToFront(dlg.ID())
		return nil
	}

	m.dialog.OpenDialogWithGrace(dlg)
	return cmd
}

// openAuthenticationDialogWithMethod opens the authentication dialog for
// the method the user chose in the auth method picker. The model the
// user selected is carried through the OAuth flow so the choice persists;
// handleSelectModel applies it only when the signed-in catalog still
// offers it and errors otherwise.
func (m *UI) openAuthenticationDialogWithMethod(provider catwalk.Provider, model config.SelectedModel, modelType config.SelectedModelType, useOAuth bool) tea.Cmd {
	isOnboarding := m.state == uiOnboarding

	var (
		dlg dialog.Dialog
		cmd tea.Cmd
	)
	if useOAuth {
		switch provider.ID {
		case catwalk.InferenceProviderOpenAI:
			dlg, cmd = auth.NewOAuthOpenAI(m.com, isOnboarding, provider, model, modelType)
		case catwalk.InferenceProviderXAI:
			dlg, cmd = auth.NewOAuthGrok(m.com, isOnboarding, provider, model, modelType)
		}
	} else {
		dlg, cmd = auth.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)
	}

	if m.dialog.ContainsDialog(dlg.ID()) {
		m.dialog.BringToFront(dlg.ID())
		return nil
	}

	m.dialog.OpenDialogWithGrace(dlg)
	return cmd
}

func (m *UI) currentModelSupportsImages() bool {
	cfg := m.com.Config()
	if cfg == nil {
		return false
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return false
	}
	model := cfg.GetModelByType(agentCfg.Model)
	return model != nil && model.SupportsImages
}

// toggleThinking flips thinking for models without reasoning levels. The
// preference write and the coordinator rebuild run off the Update
// goroutine.
func (m *UI) toggleThinking() tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return m.updateAgentModelCmd(util.ReportError(errors.New("configuration not found")))
	}

	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return m.updateAgentModelCmd(util.ReportError(errors.New("agent configuration not found")))
	}

	currentModel := cfg.Models[agentCfg.Model]
	provider := cfg.GetProviderForModel(agentCfg.Model)
	catalog := cfg.GetModelByType(agentCfg.Model)
	if provider == nil || catalog == nil || !config.SupportsThinkingToggle(*provider, *catalog) {
		return m.updateAgentModelCmd(util.ReportError(errors.New("selected model does not support a thinking toggle")))
	}
	currentModel.Think = !currentModel.Think
	ws := m.com.Workspace
	kind := agentCfg.Model
	return m.updateAgentModelCmd(func() tea.Msg {
		if err := ws.UpdatePreferredModel(config.ScopeGlobal, kind, currentModel); err != nil {
			return util.ReportError(err)()
		}
		ws.UpdateAgentModel(context.TODO())
		status := "disabled"
		if currentModel.Think {
			status = "enabled"
		}
		return util.NewInfoMsg("Thinking mode " + status)
	})
}

// setReasoningEffort validates and stores the effort for the coder's model.
// A validation error returns at once. The preference write runs off the
// Update goroutine; saved then runs on the Update goroutine with its error,
// and a successful write rebuilds the coordinator's model.
func (m *UI) setReasoningEffort(effort string, ultracode *bool, saved func(err error) tea.Cmd) (tea.Cmd, error) {
	cfg := m.com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}

	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil, errors.New("agent configuration not found")
	}

	currentModel := cfg.Models[agentCfg.Model]
	if err := cfg.ValidateReasoningEffort(currentModel.Provider, currentModel.Model, effort); err != nil {
		return nil, err
	}
	currentModel.ReasoningEffort = effort
	info := "Reasoning effort set to " + effort
	if ultracode != nil {
		currentModel.Ultracode = *ultracode
		info += " · Ultracode " + onOff(*ultracode)
	}
	ws := m.com.Workspace
	kind := agentCfg.Model
	return doThen(func() error {
		return ws.UpdatePreferredModel(config.ScopeGlobal, kind, currentModel)
	}, func(err error) tea.Cmd {
		done := saved(err)
		if err != nil {
			return done
		}
		return tea.Batch(done, m.updateAgentModelCmd(func() tea.Msg {
			ws.UpdateAgentModel(context.TODO())
			return util.NewInfoMsg(info)
		}))
	}), nil
}
