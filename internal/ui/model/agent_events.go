package model

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// handleConnectionEvent reports the health of the client-server link and,
// once it recovers, reloads the open session. A reload is always needed
// after a degraded episode: events published while the stream was down are
// gone, and if the workspace itself was re-created any run died with it.
func (m *UI) handleConnectionEvent(msg workspace.ConnectionEvent) []tea.Cmd {
	info := util.InfoMsg{
		Type: util.InfoTypeWarn,
		Msg:  "Lost connection to the Crush server — reconnecting…",
		TTL:  30 * time.Second,
	}
	switch msg.State {
	case workspace.ConnectionDegraded:
		slog.Warn("Server connection degraded", "error", msg.Err, "stuck", msg.Stuck)
		if msg.Stuck {
			info.Type = util.InfoTypeError
			info.Msg = "Can't restore the connection to the Crush server. Restart Crush to recover."
			info.TTL = time.Minute
		}
	case workspace.ConnectionRecovered:
		info = util.InfoMsg{
			Type: util.InfoTypeSuccess,
			Msg:  "Reconnected to the Crush server.",
			TTL:  DefaultStatusTTL,
		}
	}
	m.status.SetInfoMsg(info)
	cmds := []tea.Cmd{clearInfoMsgCmd(info.TTL)}
	if msg.State == workspace.ConnectionRecovered {
		if cmd := m.requestMCPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.session != nil {
			id := m.session.ID
			if m.sessionLoad.active {
				id = m.sessionLoad.id
			}
			cmds = append(cmds, m.loadSession(id))
		}
	}
	return cmds
}

// handleChannelMessage injects a channel event pushed by an MCP server into a
// session so the agent reacts to it on its next turn. The rendered <channel>
// element is already validated and escaped by the mcp package. If no session is
// active yet, one is created so a pushed event is never silently dropped; if the
// agent is busy, AgentRun enqueues the message and it is picked up on the next
// step.
//
// Injection is skipped entirely when the workspace routes channel events
// itself (client/server mode): the server injects each event exactly once,
// and injecting here as well would duplicate the turn once per attached
// client. The injected turn still reaches this client through the normal
// session/message event stream.
func (m *UI) handleChannelMessage(ev mcp.Event) tea.Cmd {
	if m.com.Workspace.RoutesChannelEvents() {
		return nil
	}
	if ev.ChannelMessage == "" || !m.com.Workspace.AgentIsReady() {
		return nil
	}
	loadCmd, err := m.ensureSession()
	if err != nil {
		slog.Debug("Failed to create session for channel message", "error", err)
		return nil
	}
	if !m.hasSession() {
		slog.Debug("Channel message dropped: no active session after ensureSession", "channel", ev.Name)
		return loadCmd
	}
	// The coordinator sets the channel binding during the turn
	// (syncSessionChannel), so there is no need to write it here —
	// doing so would race with the coordinator's own write and
	// publish a duplicate session update.
	sessionID := m.session.ID
	channel := ev.Name
	content := ev.ChannelMessage
	runCmd := func() tea.Msg {
		if err := m.com.Workspace.AgentRunChannel(context.Background(), channel, sessionID, content); err != nil {
			slog.Debug("Failed to inject channel message", "error", err, "session", sessionID)
		}
		return nil
	}
	if loadCmd != nil {
		return tea.Batch(loadCmd, runCmd)
	}
	return runCmd
}

// handleQuestionNotification dismisses an open question form when
// any client resolved the pending batch. Only one question can be
// pending at a time, so any notification means the current form
// is stale regardless of BatchID. The CLI update form isn't one of
// the service's questions, so it stays.
func (m *UI) handleQuestionNotification(_ question.Notification) {
	if form, ok := m.activeInline.(*dialog.QuestionForm); ok && !m.cliUpdatePromptOpen() && !form.HasSecureEntry() {
		m.activeInline = nil
		m.textarea.Focus()
		m.updateLayoutAndSize()
	}
}

// handlePermissionNotification updates tool items when permission state changes.
func (m *UI) handlePermissionNotification(notification permission.PermissionNotification) {
	if toolItem := m.chat.MessageItem(notification.ToolCallID); toolItem != nil {
		if permItem, ok := toolItem.(chat.ToolMessageItem); ok {
			if notification.Granted {
				permItem.SetStatus(chat.ToolStatusRunning)
			} else {
				permItem.SetStatus(chat.ToolStatusAwaitingPermission)
			}
		}
	}

	// If this notification reflects a final resolution (granted or denied),
	// dismiss any open permissions dialog whose tool call ID matches. This
	// covers the case where another client resolved the request remotely.
	if !notification.Granted && !notification.Denied {
		return
	}
	if d := m.dialog.Dialog(dialog.PermissionsID); d != nil {
		if perm, ok := d.(*dialog.Permissions); ok && perm.ToolCallID() == notification.ToolCallID {
			m.dialog.CloseDialog(dialog.PermissionsID)
		}
	}
}

// handleAgentNotification translates domain agent events into desktop
// notifications using the UI notification backend.
func (m *UI) handleAgentNotification(n notify.Notification) tea.Cmd {
	var cmds []tea.Cmd
	switch n.Type {
	case notify.TypeAgentFinished:
		cmds = append(cmds, m.checkAgentFinished(n))
		// Show what the stored balance says right away, and fetch again:
		// the refresh for the turn's last response is only kicked off once
		// its request finishes, so it may still be in flight here.
		m.updateHyperCredits()
		if m.com.IsHyper() {
			cmds = append(cmds, m.fetchHyperCredits())
		}
	case notify.TypeAgentError:
		m.turnOutcome = tea.ProgramStateError
		// Terminal edge like TypeAgentFinished; fall through to the
		// busy/queue refresh below.
	case notify.TypeReAuthenticate:
		return m.handleReAuthenticate(n.ProviderID)
	case notify.TypeAWSSSOAuth:
		return m.handleAWSSSOAuth(n.AWSSOCommand, n.AWSSOURL)
	case notify.TypeAWSSSOAuthResult:
		return m.handleAWSSSOAuthResult(n.Message)
	default:
		return nil
	}
	// TypeAgentFinished / TypeAgentError are the busy→idle edge: the agent
	// clears its active request before publishing precisely so observers
	// can re-probe. Drop the memoized busy state and re-fetch it and the
	// prompt queue off-thread.
	m.invalidateBusyCaches()
	m.queue.invalidate()
	if cmd := m.dispatchBusyRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *UI) handleReAuthenticate(providerID string) tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	providerCfg, ok := cfg.Providers.Get(providerID)
	if !ok {
		return nil
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil
	}
	return m.openAuthenticationDialog(providerCfg.ToProvider(), cfg.Models[agentCfg.Model], agentCfg.Model)
}

// handleAWSSSOAuth opens the AWS SSO progress dialog (or updates the SSO URL
// on an already-open one). The refresh command runs in the coordinator; this
// dialog is a display surface driven by agent notifications.
func (m *UI) handleAWSSSOAuth(command, url string) tea.Cmd {
	// Update the URL on an already-open dialog.
	if existing := m.dialog.Dialog(dialog.AWSSSOID); existing != nil {
		if awsDlg, ok := existing.(*dialog.AWSSSO); ok && url != "" {
			awsDlg.SetURL(url)
		}
		m.dialog.BringToFront(dialog.AWSSSOID)
		return nil
	}
	if command == "" {
		return nil
	}
	dlg, cmd := dialog.NewAWSSSO(m.com, command)
	if url != "" {
		dlg.SetURL(url)
	}
	m.dialog.OpenDialogWithGrace(dlg)
	return cmd
}

// handleAWSSSOAuthResult finishes the AWS SSO dialog once the refresh command
// exits: it closes on success or shows the error so the user can dismiss it.
func (m *UI) handleAWSSSOAuthResult(errMsg string) tea.Cmd {
	existing := m.dialog.Dialog(dialog.AWSSSOID)
	if existing == nil {
		return nil
	}
	awsDlg, ok := existing.(*dialog.AWSSSO)
	if !ok {
		return nil
	}
	if errMsg == "" {
		// Success: the turn retries transparently, so no need to linger.
		m.dialog.CloseDialog(dialog.AWSSSOID)
		return nil
	}
	awsDlg.Finish(errMsg)
	return nil
}
