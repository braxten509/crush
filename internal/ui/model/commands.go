package model

import (
	"context"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// loadCustomCommands loads the custom commands asynchronously.
func (m *UI) loadCustomCommands() tea.Cmd {
	ws := m.com.Workspace
	cfg := m.com.Config()
	return func() tea.Msg {
		customCommands, err := commands.LoadCustomCommands(cfg)
		if err != nil {
			slog.Error("Failed to load custom commands", "error", err)
		}
		// Append user-invocable skills as commands.
		skillEntries, err := ws.ListSkills(context.Background())
		if err != nil {
			slog.Error("Failed to load skill commands", "error", err)
		}
		customCommands = append(customCommands, commands.FromSkillCatalog(skillEntries)...)
		return userCommandsLoadedMsg{Commands: customCommands}
	}
}

// loadMCPrompts returns a command that loads the MCP prompts.
func loadMCPrompts(ws workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		prompts, err := ws.ListMCPPrompts(context.Background())
		if err != nil {
			slog.Error("Failed to load MCP prompts", "error", err)
		}
		if prompts == nil {
			// flag them as loaded even if there is none or an error
			prompts = []commands.MCPPrompt{}
		}
		return mcpPromptsLoadedMsg{Prompts: prompts}
	}
}

func (m *UI) runMCPPrompt(clientID, promptID string, arguments map[string]string) tea.Cmd {
	ws := m.com.Workspace
	load := func() tea.Msg {
		prompt, err := ws.GetMCPPrompt(clientID, promptID, arguments)
		if err != nil {
			// TODO: make this better
			return util.ReportError(err)()
		}

		if prompt == "" {
			return nil
		}
		return sendMessageMsg{
			Content: prompt,
		}
	}

	var cmds []tea.Cmd
	if cmd := m.dialog.StartLoading(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, load, func() tea.Msg {
		return closeDialogMsg{}
	})

	return tea.Sequence(cmds...)
}

// handleStateChanged reacts to an MCP state change: the memoized sidebar
// states refresh off-thread (an authoritative re-fetch rather than the event
// payload, so a missed field cannot drift), the coordinator rebuilds to pick
// up newly registered tools, and the MCP prompts reload.
func (m *UI) handleStateChanged() []tea.Cmd {
	ws := m.com.Workspace
	return []tea.Cmd{
		m.requestMCPRefresh(),
		m.updateAgentModelCmd(func() tea.Msg {
			ws.UpdateAgentModel(context.Background())
			return nil
		}),
		loadMCPrompts(ws),
	}
}

func handleMCPPromptsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshPrompts(context.Background(), name)
		return nil
	}
}

func handleMCPToolsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.RefreshMCPTools(context.Background(), name)
		return nil
	}
}

func handleMCPResourcesEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshResources(context.Background(), name)
		return nil
	}
}

// enableDockerMCP returns a command that enables and starts the Docker MCP
// server.
func enableDockerMCP(ws workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if err := ws.EnableDockerMCP(ctx); err != nil {
			return util.ReportError(err)()
		}

		return util.NewInfoMsg("Docker MCP enabled and started successfully")
	}
}

// disableDockerMCP returns a command that disables the Docker MCP server.
func disableDockerMCP(ws workspace.Workspace) tea.Cmd {
	return func() tea.Msg {
		if err := ws.DisableDockerMCP(); err != nil {
			return util.ReportError(err)()
		}

		return util.NewInfoMsg("Docker MCP disabled successfully")
	}
}
