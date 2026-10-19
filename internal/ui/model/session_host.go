package model

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// hostStatus tells the session list what this session is doing, when it
// runs inside one (see internal/ui/sessionhost). It sends only changes.
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
		ThemeRevision:         m.theme.revision,
		State:                 sessionhost.StateReady,
		UseTerminalBackground: m.isTransparent,
	}
	if m.session != nil {
		st.Title = m.session.Title
	}
	for _, server := range m.taskRow.hostServers {
		st.Servers = append(st.Servers, sessionhost.Server{
			Port:  server.Port,
			Name:  server.Title(),
			Owner: server.Owner,
			Group: cmp.Or(server.Process.JobID, strconv.Itoa(server.Process.PID)),
		})
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
	case m.hasUnfinishedWork(), m.hasUntrackedProcess():
		st.State = sessionhost.StateBackground
	}
	return st
}

// hasUnfinishedWork reports running sub-agents or jobs of the shown chat:
// the agent's own record when working alone, the row's engine-fed state
// when shared (the agent globals are empty then).
func (m *UI) hasUnfinishedWork() bool {
	if !m.com.Shared() {
		return agent.SessionHasUnfinishedWork(m.currentSessionID())
	}
	id := m.currentSessionID()
	return id != "" && (len(m.visibleTasks()) > 0 || slices.ContainsFunc(m.taskRow.bgProcs, func(p agent.Process) bool { return p.JobID != "" && p.Session == id }))
}

// hasUntrackedProcess reports a command this chat's agent left running
// outside Crush's jobs, such as a CLI's own background shell. Crush's jobs
// are judged by SessionHasUnfinishedWork, which leaves services out.
func (m *UI) hasUntrackedProcess() bool {
	id := m.currentSessionID()
	return id != "" && slices.ContainsFunc(m.taskRow.bgProcs, func(p agent.Process) bool {
		return p.JobID == "" && p.Session == id
	})
}

// stopHostServer stops the server on port, as the session list asked.
func (m *UI) stopHostServer(port int) tea.Cmd {
	i := slices.IndexFunc(m.taskRow.hostServers, func(s agent.Server) bool { return s.Port == port })
	if i < 0 {
		return nil
	}
	process := m.taskRow.hostServers[i].Process
	com := m.com
	// Taken off at once; the next refresh shows whatever still listens.
	m.taskRow.hostServers = slices.DeleteFunc(m.taskRow.hostServers, func(s agent.Server) bool { return s.Process == process })
	return func() tea.Msg {
		if err := com.KillJob(context.Background(), process); err != nil {
			return util.ReportError(err)()
		}
		return nil
	}
}
