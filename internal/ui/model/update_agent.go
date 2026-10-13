package model

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/ui/dialog/mcpservers"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// handleAgentMsg handles the results of the agent, model, shell and
// probe commands.
// ok is false for any other message.
func (m *UI) handleAgentMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case agentFinishedMsg:
		cmds = append(cmds, m.handleAgentFinished(msg))
	case busyStateMsg:
		cmds = append(cmds, m.applyBusyState(msg)...)
	case promptQueueMsg:
		cmds = append(cmds, m.applyPromptQueue(msg)...)
	case lspStatesMsg:
		if cmd := m.applyLSPStates(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case agentModelChangedMsg:
		// The coordinator model changed (selection, thinking, reasoning):
		// re-fetch the memoized ready/model state off-thread.
		m.invalidateBusyCaches()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case agentRunSubmittedMsg:
		cmds = append(cmds, m.handleRunSubmitted(msg)...)
	case pendingPromptResolvedMsg:
		cmds = append(cmds, m.applyResolvedPrompt(msg))
	case agentInterruptedMsg:
		id := msg.sessionID
		if id == "" {
			id = m.currentSessionID()
		}
		delete(m.queue.interruptInFlight, id)
		if id != m.currentSessionID() {
			break
		}
		m.invalidateBusyCaches()
		m.queue.invalidate()
		cmds = append(cmds, m.dispatchBusyRefresh(), m.dispatchPromptQueueRefresh())
	case modeSwitchedMsg:
		m.modeSwitching = false
		cmds = append(cmds, m.applyModeSwitch(msg)...)

	case sendMessageMsg:
		cmds = append(cmds, m.sendMessage(msg.Content, msg.Attachments...))

	case mcpStateChangedMsg:
		if dia := m.dialog.Dialog(dialog.MCPTogglesID); dia != nil {
			if toggles, ok := dia.(*mcpservers.MCPToggles); ok {
				for name, info := range msg.states {
					toggles.SetItemStatus(name, mcpStatusText(info))
				}
			}
		}
		cmds = append(cmds, m.applyMCPStates(msg)...)
	case mcpStartingRetryMsg:
		// A server was still connecting when the last fetch landed; re-probe
		// so "starting..." converges without depending on state_changed
		// events (which can be missed in client/server mode).
		if cmd := m.requestMCPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case queuedPromptRecalledMsg:
		cmds = append(cmds, m.applyRecalledPrompt(msg))

	case shellStreamMsg:
		cmds = append(cmds, m.handleShellStream(msg)...)
	case shellResultMsg:
		cmds = append(cmds, m.handleShellResult(msg)...)
	case hyperRefreshDoneMsg:
		if cmd := m.handleSelectModel(msg.action); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case copilotImportDoneMsg:
		if cmd := m.selectModel(msg.action, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case creditsUpdatedMsg:
		m.hyperCredits = msg.credits
	case usageRefreshedMsg:
		m.applyUsageRefresh(msg)
	case cliLimitsMsg:
		// New limits are read while drawing; this only redraws.
	case gitBranchUpdatedMsg:
		m.gitBranch = msg.branch
	case dialog.ActionMCPAuthStarted:
		cmds = append(cmds, m.authenticateMCP(msg.Ctx, msg.Name))
	case dialog.ActionMCPAuthComplete, dialog.ActionMCPAuthErrored:
		// The OAuth flow finished server-side: refresh the memoized states
		// so the sidebar reflects the outcome even when the state_changed
		// event was missed.
		if cmd := m.requestMCPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.dialog.HasDialogs() {
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleRunSubmitted settles a prompt submission: a failed one is resolved
// or reported, and the busy and queue state is fetched to confirm the
// optimistic values sendMessage wrote.
func (m *UI) handleRunSubmitted(msg agentRunSubmittedMsg) []tea.Cmd {
	var cmds []tea.Cmd
	id := msg.dispatchID
	if id == "" {
		id = msg.submissionID
	}
	if submission, ok := m.queue.submitting[id]; ok {
		submission.cancel()
		delete(m.queue.submitting, id)
	}
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			cmds = append(cmds, m.finishPendingPrompt(msg.submissionID, false))
		} else {
			var definitive *message.DefinitiveSubmissionError
			cmds = append(cmds, m.resolvePendingPrompt(msg.submissionID, errors.As(msg.err, &definitive)), util.ReportError(msg.err))
		}
	}
	m.turnOutcome = ""
	// A prompt was just accepted (run started or enqueued): fetch the
	// authoritative busy/queue state to confirm the optimistic values
	// sendMessage wrote.
	m.invalidateBusyCaches()
	m.queue.invalidate()
	if cmd := m.dispatchBusyRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// handleShellStream appends a chunk of a bang command's output to its
// pending item and keeps draining the stream.
func (m *UI) handleShellStream(msg shellStreamMsg) []tea.Cmd {
	var cmds []tea.Cmd
	if item := m.chat.MessageItem(msg.PendingID); item != nil {
		if shellItem, ok := item.(*chat.ShellItem); ok {
			shellItem.AppendOutput(msg.Chunk)
			m.chat.ScrollToBottom()
		}
	}
	// Continue draining the stream channel.
	if msg.streamCh != nil {
		ch := msg.streamCh
		pid := msg.PendingID
		cmds = append(cmds, func() tea.Msg {
			chunk, ok := <-ch
			if !ok {
				return nil
			}
			return shellStreamMsg{PendingID: pid, Chunk: chunk, streamCh: ch}
		})
	}
	return cmds
}

// handleShellResult completes a bang command's item, or adds one.
func (m *UI) handleShellResult(msg shellResultMsg) []tea.Cmd {
	var cmds []tea.Cmd
	// Clear the bang cancel func — command is done.
	if m.bangCancel != nil {
		m.bangCancel()
		m.bangCancel = nil
	}
	// Complete the pending shell item if it exists, otherwise create a new one.
	completed := false
	if msg.PendingID != "" {
		if item := m.chat.MessageItem(msg.PendingID); item != nil {
			if shellItem, ok := item.(*chat.ShellItem); ok {
				shellItem.Complete(msg.Output, msg.ExitCode)
				m.chat.ScrollToBottom()
				completed = true
			}
		}
	}
	if !completed {
		item := chat.NewShellItem(m.com.Styles, msg.Command, msg.Output, msg.ExitCode)
		m.chat.AppendMessages(item)
		m.chat.ScrollToBottom()
	}
	cmds = append(cmds, m.loadPromptHistory())
	return cmds
}
