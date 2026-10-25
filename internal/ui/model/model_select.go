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

	// The Codex API is always listed, key or not: ask for the key first.
	if providerID == config.CodexAPIProviderID && msg.Model.Model != "" {
		if providerCfg, _ := cfg.Providers.Get(providerID); !providerCfg.HasAPIKey(m.com.Workspace.Resolver()) {
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
	ws := m.com.Workspace
	finish := func(err, smallErr error) tea.Cmd {
		return m.finishModelSelection(msg, isOnboarding, err, smallErr)
	}
	if msg.ModelType == config.SelectedModelTypeSmall {
		// The helper model (titles, branch summaries) is set in config.
		cmds = append(cmds, saveHelperModel(ws, msg.Model, func(err error) tea.Cmd { return finish(err, nil) }))
	} else {
		// A model picked here belongs to the chat it was picked in, or is the
		// last pick a new chat starts on (owner, 2026-10-24). The helper model
		// is set on its own and never follows it.
		cmds = append(cmds, m.saveShownModel(msg.Model, func(err error) tea.Cmd { return finish(err, nil) }))
	}

	m.dialog.CloseDialog(dialog.APIKeyInputID)
	m.dialog.CloseDialog(dialog.OAuthID)
	m.dialog.CloseDialog(dialog.ModelsID)

	if isOnboarding {
		m.setState(uiLanding, uiFocusEditor)
	}

	return tea.Batch(cmds...)
}

// finishModelSelection runs on the Update goroutine once the writes of a
// model selection landed: it reports their errors, swaps the theme and
// finishes onboarding. A chat's pick needs no rebuild: its next turn builds
// the chat's own model; a new helper model is built into the agent.
func (m *UI) finishModelSelection(msg dialog.ActionSelectModel, isOnboarding bool, err, smallErr error) tea.Cmd {
	var cmds []tea.Cmd
	helper := msg.ModelType == config.SelectedModelTypeSmall
	if err != nil {
		return util.ReportError(err)
	}
	if !helper {
		// Swap the theme live based on the picked model's provider.
		// Skipped when the provider resolves to the already-active theme,
		// which avoids a full markdown re-render of the transcript on every
		// selection.
		m.applyThemeForProvider(msg.Model.Provider)
	}
	if smallErr != nil {
		cmds = append(cmds, util.ReportError(smallErr))
	}

	ws := m.com.Workspace
	modelName := msg.Model.Model
	if cfg := m.com.Config(); cfg != nil {
		if catwalkModel := cfg.GetModel(msg.Model.Provider, msg.Model.Model); catwalkModel != nil && catwalkModel.Name != "" {
			modelName = catwalkModel.Name
		}
	}
	modelMsg := "Model changed to " + modelName
	if helper {
		modelMsg = "Helper model changed to " + modelName
	}

	if !isOnboarding {
		if helper {
			cmds = append(cmds, m.updateAgentModelCmd(func() tea.Msg {
				if err := ws.UpdateAgentModel(context.TODO()); err != nil {
					return util.ReportError(err)()
				}
				return util.NewInfoMsg(modelMsg)
			}))
		} else {
			cmds = append(cmds, util.ReportInfo(modelMsg))
		}
		if m.com.IsHyper() {
			cmds = append(cmds, m.fetchHyperCredits())
		}
		return tea.Batch(cmds...)
	}

	// Onboarding: set the agents up for the selected model and start the
	// coder.
	m.com.Config().SetupAgents()
	initCoder := doThen(func() error {
		return ws.InitCoderAgent(context.TODO())
	}, func(err error) tea.Cmd {
		var cmds []tea.Cmd
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
		}
		// The agent just came up: re-fetch the memoized ready state so the
		// landing view shows the selected model without waiting for the TTL
		// backstop.
		m.invalidateBusyCaches()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	})
	cmds = append(cmds, initCoder)
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
	_, _, model, _ := m.shownModelInfo()
	return model != nil && model.SupportsImages
}

// toggleThinking flips thinking for models without reasoning levels on the
// shown chat's model. The write runs off the Update goroutine.
func (m *UI) toggleThinking() tea.Cmd {
	current, provider, catalog, ok := m.shownModelInfo()
	if !ok {
		return util.ReportError(errors.New("no model is selected"))
	}
	if provider == nil || catalog == nil || !config.SupportsThinkingToggle(*provider, *catalog) {
		return util.ReportError(errors.New("selected model does not support a thinking toggle"))
	}
	current.Think = !current.Think
	return m.saveShownModel(current, func(err error) tea.Cmd {
		if err != nil {
			return util.ReportError(err)
		}
		status := "disabled"
		if current.Think {
			status = "enabled"
		}
		return util.ReportInfo("Thinking mode " + status)
	})
}

// setReasoningEffort validates and stores the effort for the shown chat's
// model. A validation error returns at once. The write runs off the Update
// goroutine; saved then runs on the Update goroutine with its error.
func (m *UI) setReasoningEffort(effort string, ultracode *bool, saved func(err error) tea.Cmd) (tea.Cmd, error) {
	cfg := m.com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}
	currentModel, ok := m.shownChoice()
	if !ok {
		return nil, errors.New("no model is selected")
	}
	if err := cfg.ValidateReasoningEffort(currentModel.Provider, currentModel.Model, effort); err != nil {
		return nil, err
	}
	currentModel.ReasoningEffort = effort
	info := "Reasoning effort set to " + effort
	if ultracode != nil {
		currentModel.Ultracode = *ultracode
		info += " · Ultracode " + onOff(*ultracode)
	}
	return m.saveShownModel(currentModel, func(err error) tea.Cmd {
		done := saved(err)
		if err != nil {
			return done
		}
		return tea.Batch(done, util.ReportInfo(info))
	}), nil
}
