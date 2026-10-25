package model

import (
	"context"
	"encoding/json"
	"image"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/ultraviolet/layout"
)

// shownChatModel is the model of the chat on screen (the last pick on the
// landing page) with its catalog entry, or nil when the agent isn't ready or
// no model is known. It reads only memoized state and the in-memory config,
// so it is safe on every frame.
func (m *UI) shownChatModel() *workspace.AgentModel {
	if !m.agentReady {
		return nil
	}
	choice, ok := m.shownChoice()
	if !ok {
		return nil
	}
	model := workspace.AgentModel{ModelCfg: choice}
	if cfg := m.com.Config(); cfg != nil {
		if catalog := cfg.GetModel(choice.Provider, choice.Model); catalog != nil {
			model.CatwalkCfg = *catalog
		}
	}
	return &model
}

// shownChoice is the model the chat on screen owns, or the last pick a new
// chat starts on when none is open (owner, 2026-10-24: every chat owns its
// model, no folder-wide one).
func (m *UI) shownChoice() (config.SelectedModel, bool) {
	if m.hasSession() && len(m.session.Model) > 0 {
		var model config.SelectedModel
		if json.Unmarshal(m.session.Model, &model) == nil && model.Provider != "" && model.Model != "" {
			return model, true
		}
	}
	return m.lastPick, m.lastPick.Provider != "" && m.lastPick.Model != ""
}

// shownModelInfo is the shown model with its provider and catalog entry;
// either may be nil when the config no longer offers it.
func (m *UI) shownModelInfo() (config.SelectedModel, *config.ProviderConfig, *catwalk.Model, bool) {
	return m.com.ChatModel()
}

// saveShownModel saves model on the chat on screen, or as the last pick with
// no chat open, off the Update goroutine. Once saved, the screen shows it
// and then runs with the save's error.
func (m *UI) saveShownModel(model config.SelectedModel, then func(err error) tea.Cmd) tea.Cmd {
	ws := m.com.Workspace
	sessionID := ""
	if m.hasSession() {
		sessionID = m.currentSessionID()
	}
	var saved session.Session
	return doThen(func() error {
		if sessionID == "" {
			return ws.SetLastPick(context.Background(), model)
		}
		var err error
		saved, err = ws.SetSessionModel(context.Background(), sessionID, modelJSON(model))
		return err
	}, func(err error) tea.Cmd {
		if err == nil {
			switch {
			case sessionID == "":
				m.lastPick = model
			case m.currentSessionID() == sessionID:
				m.session.Model = saved.Model
				if len(m.session.Model) == 0 {
					m.session.Model = modelJSON(model)
				}
			}
		}
		return then(err)
	})
}

// lastPickMsg carries the last pick read off the Update goroutine.
type lastPickMsg struct{ model config.SelectedModel }

// fetchLastPick reads the model a new chat starts on off the Update goroutine.
func (m *UI) fetchLastPick() tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		pick, ok := ws.LastPick(context.Background())
		if !ok {
			return nil
		}
		return lastPickMsg{model: pick}
	}
}

// landingView renders the landing page view showing the current working
// directory, model information, and LSP/MCP status in a two-column layout.
func (m *UI) landingView() string {
	t := m.com.Styles
	width := m.layout.main.Dx()
	cwd := common.PrettyPathInline(t, m.com.Workspace.WorkingDir(), m.gitBranch, width)

	parts := []string{
		cwd,
	}

	parts = append(parts, "", m.modelInfo(width))
	infoSection := lipgloss.JoinVertical(lipgloss.Left, parts...)

	var remainingHeightArea image.Rectangle
	layout.Vertical(
		layout.Len(lipgloss.Height(infoSection)+1),
		layout.Fill(1),
	).Split(m.layout.main).Assign(new(image.Rectangle), &remainingHeightArea)

	mcpLspSectionWidth := min(30, (width-2)/3)

	lspSection := m.lspInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)
	mcpSection := m.mcpInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)
	skillsSection := m.skillsInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)

	content := lipgloss.JoinHorizontal(lipgloss.Left, lspSection, " ", mcpSection, " ", skillsSection)

	return lipgloss.NewStyle().
		Width(width).
		Height(m.layout.main.Dy() - 1).
		PaddingTop(1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, infoSection, "", content),
		)
}
