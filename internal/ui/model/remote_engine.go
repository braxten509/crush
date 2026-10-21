package model

import (
	"context"
	"errors"
	"log/slog"
	"slices"
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
// 5.13): the engine runs one share per chat, whichever app started it
// (owner, 2026-10-21), and several chats at once. This window starts and
// stops the share of the chat it shows, shows REMOTE while that chat is
// shared, tells the engine what it shows, and runs the three phone
// requests that change the window itself. Everything else the phone asks
// for runs in the engine.

type (
	remoteEngineStartedMsg struct {
		status     proto.RemoteStatus
		err        error
		generation uint64
	}
	// remoteEngineSharesMsg is the engine's list of the shares this window
	// sees, read after the window had heard of changes changes.
	remoteEngineSharesMsg struct {
		shares  []proto.RemoteStatus
		changes uint64
	}
)

func (m *UI) engineWorkspace() (*workspace.ClientWorkspace, bool) {
	ws, ok := m.com.Workspace.(*workspace.ClientWorkspace)
	return ws, ok
}

// startEngineRemote asks the engine to share the chat this window shows.
func (m *UI) startEngineRemote(ws *workspace.ClientWorkspace) tea.Cmd {
	generation, ctx, cancel := m.remote.beginStart()
	launch := remote.LaunchID()
	chat := m.shownChat()
	m.remote.hadShare = m.engineShared()
	return func() tea.Msg {
		defer cancel()
		st, err := ws.StartRemote(ctx, chat, launch)
		return remoteEngineStartedMsg{status: st, err: err, generation: generation}
	}
}

// loadEngineShares reads which chats the engine shares, at start and after
// a reconnect: the events tell every change after that.
func (m *UI) loadEngineShares() tea.Cmd {
	ws, ok := m.engineWorkspace()
	if !ok {
		return nil
	}
	changes := m.remote.changes
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shares, err := ws.RemoteShares(ctx)
		if err != nil {
			slog.Debug("Failed to read the phone shares", "error", err)
			return nil
		}
		return remoteEngineSharesMsg{shares: shares, changes: changes}
	}
}

// handleEngineShares takes the engine's list of shares, except for chats
// whose share changed since it was asked for.
func (m *UI) handleEngineShares(msg remoteEngineSharesMsg) {
	shares := make(map[string]proto.RemoteStatus, len(msg.shares))
	for _, st := range msg.shares {
		if m.remote.changed[st.SessionID] <= msg.changes {
			shares[st.SessionID] = st
		}
	}
	for chat, st := range m.remote.engine {
		if m.remote.changed[chat] > msg.changes {
			shares[chat] = st
		}
	}
	m.remote.engine = shares
	m.status.SetRemote(m.engineShared())
}

func (m *UI) handleRemoteEngineStarted(msg remoteEngineStartedMsg) tea.Cmd {
	ws, ok := m.engineWorkspace()
	if !ok {
		return nil
	}
	if msg.generation != m.remote.generation || (!m.remote.quiet && !m.dialog.ContainsDialog(dialog.RemoteSetupID)) {
		// Cancelled meanwhile: a share that came up anyway is stopped,
		// unless the chat was shared already.
		if msg.err == nil && msg.status.State == "on" && !m.remote.hadShare {
			chat := msg.status.SessionID
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = ws.StopRemote(ctx, chat)
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
	// The status names the chat it shares, which is the one this window
	// shows unless the window moved on from its new chat meanwhile.
	m.remote.heard(msg.status.SessionID, msg.status, true)
	m.status.SetRemote(m.engineShared())
	if !m.remote.quiet && m.engineShared() {
		m.openRemoteDialog()
	}
	m.remote.quiet = false
	return nil
}

// stopEngineRemote turns off the share of the chat this window shows,
// whichever app started it.
func (m *UI) stopEngineRemote(ws *workspace.ClientWorkspace) tea.Cmd {
	chat := m.shownChat()
	m.remote.heard(chat, proto.RemoteStatus{}, false)
	m.status.SetRemote(false)
	// A stop that failed reads the list again: REMOTE comes back while the
	// share still runs.
	reload := m.loadEngineShares()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ws.StopRemote(ctx, chat); err != nil {
			return tea.BatchMsg{util.ReportError(err), reload}
		}
		return util.NewInfoMsg("Remote Control is off")
	}
}

// handleRemoteEngineStatus follows the engine's shares: one that starts
// or ends, for any chat and from any app, and phones that connect to the
// share of the chat this window shows.
func (m *UI) handleRemoteEngineStatus(st proto.RemoteStatus) tea.Cmd {
	before, had := m.remote.engine[st.SessionID]
	shown := st.SessionID == m.shownChat()
	m.remote.heard(st.SessionID, st, st.State == "on")
	if st.State != "on" {
		if shown {
			m.status.SetRemote(false)
			m.dialog.CloseDialog(dialog.RemoteID)
		}
		return nil
	}
	if !shown {
		return nil
	}
	m.status.SetRemote(true)
	for _, p := range st.Phones {
		if had && !slices.Contains(before.Phones, p) {
			return util.ReportInfo("Remote Control: " + p + " connected")
		}
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

// engineRemoteStatus is what the Remote Control dialog shows for the share
// of the chat this window shows.
func (m *UI) engineRemoteStatus() remotecontrol.RemoteStatus {
	st := m.remote.engine[m.shownChat()]
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
