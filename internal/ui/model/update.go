package model

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// updateFlow says how update goes on after a message handler.
type updateFlow uint8

const (
	// flowContinue runs the work every update ends with: the placeholder,
	// the refresh backstop, attachments, the animation clock and the frame
	// GC (see finishUpdate).
	flowContinue updateFlow = iota
	// flowReturn skips that work and returns the commands gathered so far
	// together with the handler's.
	flowReturn
	// flowReplace skips it too and returns only the handler's command.
	flowReplace
)

// update handles one message: the secure entry and the sub-agent view get
// first pick, routeMsg hands it to its handler, and finishUpdate does the
// work every update ends with unless the handler ended the update early.
func (m *UI) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A release ends a scrollbar drag wherever it goes, so a dialog or the
	// secure entry opening mid-drag can't leave the drag stuck on.
	var endedScrollbarDrag bool
	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		endedScrollbarDrag = m.chat.HandleScrollbarRelease()
	}

	// Secure input bypasses chat, clipboard attachments, global keys, history,
	// question answers, and remote presence handling entirely.
	if cmd, ok := m.handleEngineSecureMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleSecureEntryMsg(msg); ok {
		return m, cmd
	}

	var cmds []tea.Cmd
	m.beginFrameUpdate()
	defer m.syncRemotePresence()
	if cmd, consumed := m.handleSubagentView(msg); consumed {
		return m, cmd
	} else if cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Update terminal capabilities
	m.caps.Update(msg)
	m.trackTurnOutcome(msg)

	cmd, flow := m.routeMsg(msg, endedScrollbarDrag)
	switch flow {
	case flowReturn:
		return m, tea.Batch(append(cmds, cmd)...)
	case flowReplace:
		return m, cmd
	}
	cmds = append(cmds, cmd)
	cmds = append(cmds, m.finishUpdate(msg)...)
	return m, tea.Batch(cmds...)
}

// routeMsg hands a message to its handler: input to its owner, then the
// terminal's, the workspace's and the UI's own messages by concern.
// Anything else goes to the dialogs, if any are open.
func (m *UI) routeMsg(msg tea.Msg, endedScrollbarDrag bool) (tea.Cmd, updateFlow) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPressMsg(msg), flowContinue
	case tea.PasteMsg:
		return m.routePaste(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg, endedScrollbarDrag)
	case common.CoalescedWheelMsg:
		return m.handleMouseWheel(msg)
	}
	if cmd, flow, ok := m.handleTerminalMsg(msg); ok {
		return cmd, flow
	}
	if cmd, ok := m.handleConfigRefreshMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, flow, ok := m.handleWorkspaceEvent(msg); ok {
		return cmd, flow
	}
	if cmd, ok := m.handleRemoteMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleAgentMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleSessionMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleUIMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleTickMsg(msg); ok {
		return cmd, flowContinue
	}
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg), flowContinue
	}
	return nil, flowContinue
}

// finishUpdate is the work every update ends with, whatever the message.
func (m *UI) finishUpdate(msg tea.Msg) []tea.Cmd {
	m.syncPlaceholder()

	// TTL backstop: schedule an off-thread re-probe for any memoized
	// workspace state that has gone stale. Never does IO on this
	// goroutine.
	cmds := m.staleWorkspaceRefreshCmds()

	// at this point this can only handle [message.Attachment] message, and we
	// should return all cmds anyway.
	if _, isAttachment := msg.(message.Attachment); isAttachment && m.attachmentsLimitReached() {
		cmds = append(cmds, util.ReportWarn(fmt.Sprintf("Maximum attachment reached for this session (%d)", m.currentModelMaxAttachments())))
	} else if m.attachments.Update(msg) {
		m.frame.invalidate()
	}
	m.syncAttachmentsLayout()
	// Any update may have put a spinner on screen (new message, tool update,
	// scroll, session load); make sure the clock is running. This is the
	// sole place the clock is armed so a tick never sits inside a caller's
	// tea.Sequence.
	// The header spins while dispatched subagents work, and they work out
	// of sight, so the clock cannot be left to the transcript alone, which
	// only arms it for an item currently on screen.
	m.subAgentsRunning = m.runningSubAgents()
	if m.state == uiChat {
		m.chat.SetExternalAnimation(m.subAgentsRunning > 0)
		if cmd := m.chat.EnsureAnimating(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Same for the scrollbar's auto-hide: a scroll may be applied from
		// anywhere, so the deadline is armed here instead of being handed
		// back to each call site to forward.
		if cmd := m.chat.EnsureScrollbarHide(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if v := m.subagentView; v != nil {
			if cmd := v.chat.EnsureAnimating(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if cmd := v.chat.EnsureScrollbarHide(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if cmd := m.endFrameUpdate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// syncPlaceholder keeps the focused editor's placeholder in step with the
// mode and the agent. It runs at the end of every update.
func (m *UI) syncPlaceholder() {
	prevPlaceholder := m.textarea.Placeholder
	if m.focus == uiFocusEditor {
		m.textarea.Placeholder = m.placeholder(m.isAgentBusy(), m.mode == uiInputModePlan, m.permModeCached())
	}
	if m.textarea.Placeholder != prevPlaceholder {
		m.frame.invalidate()
	}
}
