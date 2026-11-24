package model

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// toggleInputMode switches between planning and coding.
func (m *UI) toggleInputMode() tea.Cmd {
	if m.isAgentBusy() || m.modeSwitching {
		return util.ReportWarn("Agent is busy, please wait before switching input mode...")
	}
	if m.mode == uiInputModePlan {
		return m.setInputMode(uiInputModeCode)
	}
	return m.setInputMode(uiInputModePlan)
}

// Mode banner copy shown in the status bar after switching modes.
const (
	planModeBannerMsg     = "Plan with Crush before generating any code."
	yoloModeBannerMsg     = "System level commands stay blocked. The rest run unprompted."
	sysadminModeBannerMsg = "All commands are unblocked. Use wisely."
)

func (m *UI) setInputMode(target uiInputMode) tea.Cmd {
	agentID := config.AgentPlan
	if target == uiInputModeCode {
		agentID = config.AgentCoder
	}

	// The permission mode is orthogonal to the input mode, so report it
	// alongside the mode instead of treating a YOLO-enabled coder as plain
	// "code".
	perm := permission.PermissionModeNormal
	if target == uiInputModeCode {
		perm = m.permModeCached()
	}

	// The agent switch is an HTTP round-trip in client/server mode, so it
	// runs off the update loop together with the model update. The mode and
	// editor prompt only change once the switch succeeds (applyModeSwitch),
	// so a failed switch never leaves the editor claiming a mode the
	// server's active agent does not match.
	m.modeSwitching = true
	ws := m.com.Workspace
	return func() tea.Msg {
		err := ws.AgentSetMain(agentID)
		if err == nil {
			err = ws.UpdateAgentModel(context.Background())
		}
		return modeSwitchedMsg{
			mode: target,
			perm: perm,
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
	m.setEditorPrompt(m.permModeCached())
	var cmds []tea.Cmd
	if msg.continueSessionID != "" && m.session != nil && m.session.ID == msg.continueSessionID {
		cmds = append(cmds, m.sendMessageInternal("Implement the plan.", true))
	}
	switch {
	case msg.mode == uiInputModePlan:
		cmds = append(cmds, util.CmdHandler(util.InfoMsg{Type: util.InfoTypePlan, Msg: planModeBannerMsg}))
	case msg.perm == permission.PermissionModeSysadmin:
		cmds = append(cmds, util.CmdHandler(util.InfoMsg{Type: util.InfoTypeSysadmin, Msg: sysadminModeBannerMsg}))
	case msg.perm == permission.PermissionModeYolo:
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
	perm              permission.PermissionMode
	err               error
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
	if _, ok := m.activeInline.(*questionform.PlanHandoffInline); ok {
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
	if _, ok := m.activeInline.(*questionform.PlanHandoffInline); ok {
		m.activeInline = nil
		// Focus goes through setFocus so chat, sidebar and editor agree; its
		// cursor-blink command is dropped, as the old textarea.Focus() was.
		_ = m.setFocus(uiFocusEditor)
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
	inline := questionform.NewPlanHandoffInline(m.com)
	inline.OnConfirm = func() tea.Cmd {
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
