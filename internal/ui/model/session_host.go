package model

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// hostStatus tells the session list what this session is doing, when it
// runs inside one (see internal/sessionhost). It sends only changes.
func (m *UI) hostStatus() tea.Cmd {
	if !m.inSessionHost {
		return nil
	}
	st := m.sessionHostStatus()
	if m.hostStatusSent && st.Equal(m.lastHostStatus) {
		return nil
	}
	m.lastHostStatus, m.hostStatusSent = st, true
	return tea.Raw(st.Sequence())
}

func (m *UI) sessionHostStatus() sessionhost.Status {
	st := sessionhost.Status{
		Dir:                   m.com.Workspace.WorkingDir(),
		Theme:                 common.ThemeNameFromConfig(m.com.Config()),
		State:                 sessionhost.StateReady,
		UseTerminalBackground: m.isTransparent,
	}
	if m.session != nil {
		st.Title = m.session.Title
	}
	for _, server := range m.taskRow.hostServers {
		st.Servers = append(st.Servers, sessionhost.Server{Port: server.Port, Name: server.Title()})
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
	_, asking := m.activeInline.(*questionform.QuestionForm)
	switch {
	case asking || m.dialog.ContainsDialog(dialog.PermissionsID):
		st.State = sessionhost.StateWaiting
	case m.isAgentBusy():
		st.State = sessionhost.StateWorking
	case agent.SessionHasUnfinishedWork(m.currentSessionID()):
		st.State = sessionhost.StateBackground
	}
	return st
}

// stopHostServer stops the server on port, as the session list asked.
func (m *UI) stopHostServer(port int) tea.Cmd {
	i := slices.IndexFunc(m.taskRow.hostServers, func(s agent.Server) bool { return s.Port == port })
	if i < 0 {
		return nil
	}
	process := m.taskRow.hostServers[i].Process
	// Taken off at once; the next refresh shows whatever still listens.
	m.taskRow.hostServers = slices.DeleteFunc(m.taskRow.hostServers, func(s agent.Server) bool { return s.Process == process })
	return func() tea.Msg {
		if err := agent.StopBackgroundProcess(process); err != nil {
			return util.ReportError(err)()
		}
		return nil
	}
}
