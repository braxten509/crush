package model

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/remotecontrol"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Remote Control in a window that shares the engine (engine-design.md 5.7,
// 5.13): the engine runs the share; this window starts and stops it, tells
// it what it shows, and runs the three phone requests that change the
// window itself. Everything else the phone asks for runs in the engine.

type remoteEngineStartedMsg struct {
	status     proto.RemoteStatus
	err        error
	generation uint64
}

func (m *UI) engineWorkspace() (*workspace.ClientWorkspace, bool) {
	ws, ok := m.com.Workspace.(*workspace.ClientWorkspace)
	return ws, ok
}

// startEngineRemote asks the engine to start this window's share.
func (m *UI) startEngineRemote(ws *workspace.ClientWorkspace) tea.Cmd {
	generation, ctx, cancel := m.remote.beginStart()
	launch := remote.LaunchID()
	return func() tea.Msg {
		defer cancel()
		st, err := ws.StartRemote(ctx, launch)
		return remoteEngineStartedMsg{status: st, err: err, generation: generation}
	}
}

func (m *UI) handleRemoteEngineStarted(msg remoteEngineStartedMsg) tea.Cmd {
	ws, ok := m.engineWorkspace()
	if !ok {
		return nil
	}
	if msg.generation != m.remote.generation || (!m.remote.quiet && !m.dialog.ContainsDialog(dialog.RemoteSetupID)) {
		// Cancelled meanwhile: a share that came up anyway is stopped.
		if msg.err == nil && msg.status.State == "on" && !m.remote.engineOn {
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = ws.StopRemote(ctx)
				return nil
			}
		}
		return nil
	}
	m.remote.starting = false
	m.remote.setupCancel = nil
	err := msg.err
	if err == nil && msg.status.State == "off" && msg.status.Error != "" {
		err = errors.New(msg.status.Error)
	}
	if err == nil && msg.status.State == "setup" && msg.status.Setup != nil {
		s := msg.status.Setup
		info := remote.SetupInfo{State: remote.SetupState(s.State), Title: s.Title, Detail: s.Detail, Action: s.Action, Button: s.Button}
		if m.remote.quiet {
			m.remote.quiet = false
			return util.ReportError(errors.New("Remote Control: " + info.Title))
		}
		m.remote.setupInfo = info
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*remotecontrol.RemoteSetup); ok {
			d.SetInfo(info)
		}
		if info.State != remote.SetupUnsupported {
			return m.pollRemoteSetup()
		}
		return nil
	}
	if err == nil && msg.status.State != "on" {
		err = errors.New("sharing did not start")
	}
	if err != nil {
		if m.remote.quiet {
			m.remote.quiet = false
			return util.ReportError(errors.New("Remote Control: " + err.Error()))
		}
		info := remote.SetupInfo{Title: "Sharing could not start", Detail: err.Error() + ". Choose Check again to retry."}
		m.remote.setupInfo = info
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*remotecontrol.RemoteSetup); ok {
			d.SetInfo(info)
		}
		return nil
	}
	// The launch id is used once (REM-009).
	remote.SetLaunchID("")
	m.dialog.CloseDialog(dialog.RemoteSetupID)
	m.remote.engineOn = true
	m.remote.engineStatus = msg.status
	m.remote.presence = remote.Presence{}
	m.remote.phones = msg.status.Phones
	m.syncRemotePresence()
	m.status.SetRemote(true)
	if !m.remote.quiet {
		m.openRemoteDialog()
	}
	m.remote.quiet = false
	return nil
}

// stopEngineRemote turns this window's engine share off.
func (m *UI) stopEngineRemote(ws *workspace.ClientWorkspace) tea.Cmd {
	m.remote.engineOn = false
	m.status.SetRemote(false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ws.StopRemote(ctx); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("Remote Control is off")
	}
}

// handleRemoteEngineStatus follows the engine's share state: phones that
// connect, and a share that ended.
func (m *UI) handleRemoteEngineStatus(st proto.RemoteStatus) tea.Cmd {
	if !m.remote.engineOn {
		return nil
	}
	m.remote.engineStatus = st
	if st.State != "on" {
		m.remote.engineOn = false
		m.status.SetRemote(false)
		m.dialog.CloseDialog(dialog.RemoteID)
		return nil
	}
	if p, ok := m.remote.newPhone(st.Phones); ok {
		return util.ReportInfo("Remote Control: " + p + " connected")
	}
	return nil
}

// syncEnginePresence tells the engine what the window shows, on a change.
func (m *UI) syncEnginePresence(p remote.Presence) {
	ws, ok := m.engineWorkspace()
	if !ok || p == m.remote.presence {
		return
	}
	m.remote.presence = p
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ws.SetRemotePresence(ctx, proto.RemotePresence{SessionID: p.SessionID, Focused: p.Focused, Plan: p.Plan}); err != nil {
			slog.Debug("Failed to report the window to the phone share", "error", err)
		}
	}()
}

// engineRemoteStatus is what the Remote Control dialog shows for an engine share.
func (m *UI) engineRemoteStatus() remotecontrol.RemoteStatus {
	st := m.remote.engineStatus
	port := ""
	if st.Port != 0 {
		port = strconv.Itoa(st.Port)
	}
	return remotecontrol.RemoteStatus{Host: st.Host, Port: port, Phones: st.Phones}
}

// handleEngineRemoteAction runs a phone request the engine handed to this
// window, with the same rules as a local share (remote.go runRemoteAction).
func (m *UI) handleEngineRemoteAction(a proto.RemoteAction) tea.Cmd {
	ws, ok := m.engineWorkspace()
	if !ok {
		return nil
	}
	finish := func(done proto.RemoteActionDone) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := ws.FinishRemoteAction(ctx, a.ActionID, done); err != nil {
				slog.Debug("Failed to answer the phone", "error", err)
			}
			return nil
		}
	}
	fail := func(err error) tea.Cmd { return finish(proto.RemoteActionDone{Error: err.Error()}) }
	current := ""
	if m.hasSession() {
		current = m.session.ID
	}
	if a.SessionID != current {
		return fail(errors.New("the Crush window switched to another chat"))
	}
	switch a.Kind {
	case "new_chat_send":
		cmd, err := m.ensureSession()
		if err != nil {
			return fail(err)
		}
		if !m.hasSession() {
			return fail(errors.New("the new chat could not start"))
		}
		id := m.session.ID
		// The engine checks the answer against this window's current chat,
		// so report the new chat before answering; loadSession's own report
		// runs alongside and may land later.
		return tea.Batch(cmd, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := ws.SetCurrentSession(ctx, id); err != nil {
				slog.Debug("Failed to report current session", "session_id", id, "error", err)
			}
			if err := ws.FinishRemoteAction(ctx, a.ActionID, proto.RemoteActionDone{SessionID: id}); err != nil {
				slog.Debug("Failed to answer the phone", "error", err)
			}
			return nil
		})
	case "yolo":
		if m.mode == uiInputModePlan && (m.isAgentBusy() || m.modeSwitching) {
			return fail(errAgentBusy)
		}
		return tea.Batch(m.toggleYoloCommand(), finish(proto.RemoteActionDone{}))
	case "mode":
		if m.isAgentBusy() || m.modeSwitching {
			return fail(errAgentBusy)
		}
		return tea.Batch(m.toggleInputMode(), finish(proto.RemoteActionDone{}))
	}
	return fail(errors.New("unknown request " + a.Kind))
}
