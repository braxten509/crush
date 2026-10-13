package model

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/workspace"
)

// handleWorkspaceEvent handles the events the workspace publishes:
// sessions, messages, files, LSP and MCP state, permissions, questions
// and tasks.
// ok is false for any other message.
func (m *UI) handleWorkspaceEvent(msg tea.Msg) (tea.Cmd, updateFlow, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case pubsub.Event[notify.Notification]:
		if cmd := m.handleAgentNotification(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[notify.RunComplete]:
		if msg.Payload.Cancelled {
			cmds = append(cmds, m.finishPendingPrompt(msg.Payload.SubmissionID, false))
		} else if msg.Payload.Error != "" {
			cmds = append(cmds, m.resolvePendingPrompt(msg.Payload.SubmissionID, true))
		}
		if cmd := m.handlePlanHandoff(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[session.Session]:
		cmds = append(cmds, m.handleSessionEvent(msg)...)
	case pubsub.Event[message.Message]:
		cmds = append(cmds, m.handleMessageEvent(msg)...)
	case pubsub.Event[history.File]:
		cmds = append(cmds, m.handleFileEvent(msg.Payload))
	case pubsub.Event[app.LSPEvent]:
		// Refresh the memoized LSP state off-thread: LSPGetStates is a
		// synchronous HTTP round-trip in client/server mode and diagnostics
		// events can arrive per edited file.
		if cmd := m.requestLSPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[workspace.LSPEvent]:
		if cmd := m.requestLSPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[skills.Event]:
		m.skillStates = msg.Payload.States
	case pubsub.Event[mcp.Event]:
		switch msg.Payload.Type {
		case mcp.EventStateChanged:
			return tea.Batch(m.handleStateChanged()...), flowReplace, true
		case mcp.EventPromptsListChanged:
			return handleMCPPromptsEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventToolsListChanged:
			return handleMCPToolsEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventResourcesListChanged:
			return handleMCPResourcesEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventChannelMessage:
			return m.handleChannelMessage(msg.Payload), flowReplace, true
		}
	case pubsub.Event[permission.PermissionRequest]:
		if cmd := m.openPermissionsDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("Permission required to execute \"%s\"", msg.Payload.ToolName),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[permission.PermissionNotification]:
		m.handlePermissionNotification(msg.Payload)
	case pubsub.Event[question.Request]:
		if cmd := m.openBatchFormDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.ScrollToBottom()
		cmds = append(cmds, m.playNotificationSound(notification.SoundQuestion))
		if cmd := m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("%d questions need your input", len(msg.Payload.Questions)),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[agent.Task]:
		if cmd := m.handleTaskEvent(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[question.Notification]:
		m.handleQuestionNotification(msg.Payload)
	case workspace.ConnectionEvent:
		cmds = append(cmds, m.handleConnectionEvent(msg)...)
	default:
		return nil, flowContinue, false
	}
	return tea.Batch(cmds...), flowContinue, true
}

// handleSessionEvent follows a change to a session: the open one deleted
// starts a new chat, the open one updated refreshes the todos and pills.
func (m *UI) handleSessionEvent(msg pubsub.Event[session.Session]) []tea.Cmd {
	var cmds []tea.Cmd
	if cmd := m.holdSessionLoadSession(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if msg.Type == pubsub.DeletedEvent {
		if m.session != nil && m.session.ID == msg.Payload.ID {
			if cmd := m.newSession(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return cmds
	}
	if m.session != nil && msg.Payload.ID == m.session.ID {
		prevHasInProgress := hasInProgressTodo(m.session.Todos)
		prevPillsHeight := m.pillsAreaHeight()
		m.updateHyperCredits()
		m.session = &msg.Payload
		m.syncKeptSkillsDialog()
		if !prevHasInProgress && hasInProgressTodo(m.session.Todos) {
			m.todoIsSpinning = true
			cmds = append(cmds, m.todoSpinner.Tick)
		}
		// The pills panel reserves vertical space that the chat area
		// must yield. Recompute the layout whenever that footprint
		// changes (todos appearing, the list growing, etc.) so the
		// box renders on first paint rather than waiting for a toggle.
		// When the footprint is unchanged we still re-render the pill
		// content so status changes (e.g. the in-progress spinner)
		// show up.
		if m.pillsAreaHeight() != prevPillsHeight {
			m.updateLayoutAndSize()
		} else {
			m.renderPills()
		}
		m.autoExpandPillsIfReasonable()
	}
	return cmds
}

// handleMessageEvent follows a change to a message of the open session,
// or of a sub-agent run by one of its tools.
func (m *UI) handleMessageEvent(msg pubsub.Event[message.Message]) []tea.Cmd {
	var cmds []tea.Cmd
	// Events for a session still loading are replayed once it lands.
	if m.holdSessionLoadEvent(msg) {
		return cmds
	}
	// Check if this is a child session message for an agent tool.
	if m.session == nil {
		return cmds
	}
	if msg.Payload.SessionID != m.session.ID {
		// This might be a child session message from an agent tool.
		if cmd := m.handleChildSessionMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}
	switch msg.Type {
	case pubsub.CreatedEvent:
		cmds = append(cmds, m.appendSessionMessage(msg.Payload))
		// A new message is a run boundary — a user prompt starting
		// a turn or the agent replying/dequeueing. Drop the
		// memoized busy state and re-fetch it and the queue
		// off-thread. Per-chunk UpdatedEvents deliberately do NOT
		// trigger this: during streaming that would put workspace
		// probes on every token.
		m.invalidateBusyCaches()
		m.queue.invalidate()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.UpdatedEvent:
		cmds = append(cmds, m.updateSessionMessage(msg.Payload))
	case pubsub.DeletedEvent:
		m.chat.RemoveMessage(msg.Payload.ID)
	}
	// start the spinner if there is a new message
	if hasInProgressTodo(m.session.Todos) && m.isAgentBusy() && !m.todoIsSpinning {
		m.todoIsSpinning = true
		cmds = append(cmds, m.todoSpinner.Tick)
	}
	// stop the spinner if the agent is not busy anymore
	if m.todoIsSpinning && !m.isAgentBusy() {
		m.todoIsSpinning = false
	}
	// there is a number of things that could change the pills here so we want to re-render
	m.renderPills()
	return cmds
}
