package model

import (
	"cmp"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

// hostStatus tells the session list what this session is doing, when it
// runs inside one (see internal/sessionhost). It sends only changes.
func (m *UI) hostStatus() tea.Cmd {
	if !m.inSessionHost {
		return nil
	}
	st := m.sessionHostStatus()
	if m.hostStatusSent && st == m.lastHostStatus {
		return nil
	}
	m.lastHostStatus, m.hostStatusSent = st, true
	return tea.Raw(st.Sequence())
}

func (m *UI) sessionHostStatus() sessionhost.Status {
	st := sessionhost.Status{
		Dir:   m.com.Workspace.WorkingDir(),
		Theme: common.ThemeNameFromConfig(m.com.Config()),
		State: sessionhost.StateReady,
	}
	if m.session != nil {
		st.Title = m.session.Title
	}
	if model := m.selectedLargeModel(); model != nil {
		st.Model = model.CatwalkCfg.Name
		if effort := cmp.Or(model.ModelCfg.ReasoningEffort, model.CatwalkCfg.DefaultReasoningEffort); effort != "" && model.CatwalkCfg.CanReason {
			st.Model += " · " + common.FormatReasoningEffort(effort)
		}
		if model.ModelCfg.ServiceTier == "fast" {
			st.Model += " · Fast"
		}
	}
	_, asking := m.activeInline.(*dialog.QuestionForm)
	switch {
	case asking || m.dialog.ContainsDialog(dialog.PermissionsID):
		st.State = sessionhost.StateWaiting
	case m.isAgentBusy():
		st.State = sessionhost.StateWorking
	}
	return st
}
