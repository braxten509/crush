package model

import (
	"context"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) toggleInputMode() tea.Cmd {
	if m.isAgentBusy() || m.modeSwitching {
		return util.ReportWarn("Agent is busy, please wait before switching input mode...")
	}
	if m.mode == uiInputModePlan {
		// Second step of the Shift+Tab cycle: plan -> YOLO. Enabling YOLO
		// here is the only case where the cycle may disable it again.
		if !m.com.Workspace.PermissionSkipRequests() {
			m.toggleYoloMode()
			m.cycleYolo = true
		}
		return m.setInputMode(uiInputModeCode)
	}
	// Only the cycle may turn YOLO back off: YOLO the user enabled himself
	// (Ctrl+Y, the command palette) survives entering plan mode.
	if m.com.Workspace.PermissionSkipRequests() && m.cycleYolo {
		m.toggleYoloMode()
		return util.ReportInfo("input mode: code")
	}
	return m.setInputMode(uiInputModePlan)
}

// switchPlanToYolo handles activating YOLO while in plan mode: YOLO is a
// coding concern, so instead of a "plan + yolo" state the UI switches
// straight to the coder with YOLO enabled. Activation is idempotent — YOLO
// carried into plan mode stays on, and the user ends up in full YOLO mode
// either way.
func (m *UI) switchPlanToYolo() tea.Cmd {
	if m.isAgentBusy() || m.modeSwitching {
		return util.ReportWarn("Agent is busy, please wait before switching input mode...")
	}
	if !m.com.Workspace.PermissionSkipRequests() {
		m.toggleYoloMode()
	}
	// Explicit activation pins YOLO: the Shift+Tab cycle must not disable
	// it on the next pass.
	m.cycleYolo = false
	return m.setInputMode(uiInputModeCode)
}

// Mode banner copy shown in the status bar after switching modes.
const (
	planModeBannerMsg = "Plan with Crush before generating any code."
	yoloModeBannerMsg = "Skip permission prompts. System level commands will be blocked."
)

func (m *UI) setInputMode(target uiInputMode) tea.Cmd {
	agentID := config.AgentPlan
	if target == uiInputModeCode {
		agentID = config.AgentCoder
	}

	// YOLO is orthogonal to the input mode, so report it alongside the mode
	// instead of treating a YOLO-enabled coder as plain "code".
	yolo := target == uiInputModeCode && m.com.Workspace.PermissionSkipRequests()

	// The agent switch is an HTTP round-trip in client/server mode, so it
	// runs off the update loop together with the model update. The mode and
	// editor prompt only change once the switch succeeds (applyModeSwitch),
	// so a failed switch never leaves the editor claiming a mode the
	// server's active agent does not match.
	m.modeSwitching = true
	return func() tea.Msg {
		err := m.com.Workspace.AgentSetMain(agentID)
		if err == nil {
			err = m.com.Workspace.UpdateAgentModel(context.Background())
		}
		return modeSwitchedMsg{
			mode: target,
			yolo: yolo,
			err:  err,
		}
	}
}

// applyModeSwitch finalizes an input-mode switch once the backend has
// settled. On error the previous mode is kept so the editor never claims a
// mode the server's active agent does not match.
func (m *UI) applyModeSwitch(msg modeSwitchedMsg) []tea.Cmd {
	if msg.err != nil {
		return []tea.Cmd{util.ReportError(msg.err)}
	}
	m.mode = msg.mode
	m.setEditorPrompt(m.yoloModeCached())
	var cmds []tea.Cmd
	if msg.continueSessionID != "" && m.session != nil && m.session.ID == msg.continueSessionID {
		cmds = append(cmds, m.sendMessageInternal("Implement the plan.", true))
	}
	switch {
	case msg.mode == uiInputModePlan:
		cmds = append(cmds, util.CmdHandler(util.InfoMsg{Type: util.InfoTypePlan, Msg: planModeBannerMsg}))
	case msg.yolo:
		cmds = append(cmds, util.CmdHandler(util.InfoMsg{Type: util.InfoTypeYolo, Msg: yoloModeBannerMsg}))
	default:
		cmds = append(cmds, util.ReportInfo("input mode: code"))
	}
	return cmds
}

// modeSwitchedMsg reports that the async agent switch started by
// setInputMode has finished (successfully or not).
type modeSwitchedMsg struct {
	continueSessionID string
	mode              uiInputMode
	yolo              bool
	err               error
}

// toggleYoloCommand is Ctrl+Y from the command palette.
func (m *UI) toggleYoloCommand() tea.Cmd {
	if m.mode == uiInputModePlan {
		// Same as Ctrl+Y in plan mode: YOLO only exists as YOLO
		// coding, so activating it leaves plan mode.
		return m.switchPlanToYolo()
	}
	m.toggleYoloMode()
	return nil
}

// handlePlanHandoff checks whether a completed run in plan mode contained the
// plan-ready sentinel marker and, if so, opens the plan handoff dialog.
func (m *UI) handlePlanHandoff(rc notify.RunComplete) tea.Cmd {
	if m.mode != uiInputModePlan {
		return nil
	}
	if rc.Error != "" || rc.Cancelled {
		return nil
	}
	if m.session == nil || rc.SessionID != m.session.ID {
		return nil
	}
	if !common.PlanReadyMarkerPresent(rc.Text) {
		slog.Debug("Plan run completed without ready marker", "session_id", rc.SessionID)
		return nil
	}
	m.setPlanReadyPending(rc.SessionID)
	if _, ok := m.activeInline.(*dialog.PlanHandoffInline); ok {
		return nil
	}
	m.openPlanHandoff()
	return nil
}

// resetPlanModeState drops any pending plan handoff and, when plan mode is
// active, switches back to code mode. Used when the UI moves to a different
// session, since plan mode is scoped to the session it was enabled in.
func (m *UI) resetPlanModeState() tea.Cmd {
	m.setPlanReadyPending("")
	if _, ok := m.activeInline.(*dialog.PlanHandoffInline); ok {
		m.activeInline = nil
		m.textarea.Focus()
	}
	if m.mode != uiInputModePlan {
		return nil
	}
	// The backend rejects agent switches while a run is active (409). When
	// one is, keep the mode as-is: the server's active agent still matches
	// what the editor shows, and the next Shift+Tab lands back in code mode
	// once the run finishes.
	if m.isAgentBusy() {
		return nil
	}
	return m.setInputMode(uiInputModeCode)
}

// setPlanReadyPending records (or clears, with an empty ID) the session that
// has an unconfirmed ready plan.
func (m *UI) setPlanReadyPending(sessionID string) {
	m.planReadySessionID = sessionID
}

// openPlanHandoff replaces the textarea with the inline "switch to code"
// prompt. Dismissing it keeps the pending plan, so the prompt can be reopened
// by pressing enter on an empty editor while still in plan mode.
func (m *UI) openPlanHandoff() {
	if m.secureQuestionFormOpen() {
		return
	}
	inline := dialog.NewPlanHandoffInline(m.com)
	inline.OnConfirm = func(yolo bool) tea.Cmd {
		if m.com.Workspace.PermissionSkipRequests() != yolo {
			m.toggleYoloMode()
		}
		m.setPlanReadyPending("")
		sessionID := m.session.ID
		cmd := m.setInputMode(uiInputModeCode)
		return func() tea.Msg {
			result := cmd()
			if switched, ok := result.(modeSwitchedMsg); ok {
				switched.continueSessionID = sessionID
				return switched
			}
			return result
		}
	}
	inline.OnRequestChanges = func(feedback string) tea.Cmd {
		return m.sendMessage(feedback)
	}
	m.openInline(inline)
	if m.status != nil {
		m.updateLayoutAndSize()
	}
}
