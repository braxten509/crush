package model

import (
	"cmp"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"time"

	"github.com/charmbracelet/crush/internal/ui/dialog/remotecontrol"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Remote Control shares the chat this window shows with the Pocket Agents
// phone app. The server runs beside the TUI; what the phone asks for comes
// back here as remoteActionMsg and runs through the same code as the keys,
// so the window and the phone never disagree.

type (
	remoteStartedMsg struct {
		server     *remote.Server
		err        error
		generation uint64
	}
	remoteSetupPollMsg     struct{ generation uint64 }
	remoteSetupFinishedMsg struct {
		generation uint64
		err        error
	}
	remoteActionMsg struct{ action *remote.Action }
	remotePhonesMsg struct{}
)

var errAgentBusy = errors.New("Agent is busy, please wait...") //nolint:staticcheck

// remoteShare is the window's Remote Control share: the server while it
// runs, and the setup that starts it.
type remoteShare struct {
	// server is the running share, nil while off.
	server *remote.Server
	// presence is what the phone was last told the window shows.
	presence remote.Presence
	// phones are the phones connected as of the last change.
	phones []string

	// starting is set while a start attempt is in flight.
	starting bool
	// generation is bumped whenever setup restarts or ends, so the result
	// of an older attempt is ignored.
	generation  uint64
	setupCancel context.CancelFunc
	// setupBusy is set while a setup step runs in the foreground.
	setupBusy bool
	setupInfo remote.SetupInfo
	// quiet: the share was asked for by the phone's launcher, so it starts
	// without opening the Remote Control dialog over the new window.
	quiet bool

	// engine holds the phone shares the shared engine runs that this
	// window sees, by chat ("" for its new chat): one per chat, whichever
	// app started it (remote_engine.go).
	engine map[string]proto.RemoteStatus
	// reports sends what the window shows to the engine, in order.
	reports *presenceSender
	// changes counts what this window heard of the engine's shares, and
	// changed holds the count at each chat's last change: a list read
	// before a change does not undo it.
	changes uint64
	changed map[string]uint64
}

// heard records a change to the share of chat: st, or none when off.
func (r *remoteShare) heard(chat string, st proto.RemoteStatus, on bool) {
	// The engine's list may have made engine already (handleEngineShares).
	if r.engine == nil {
		r.engine = make(map[string]proto.RemoteStatus)
	}
	if r.changed == nil {
		r.changed = make(map[string]uint64)
	}
	r.changes++
	r.changed[chat] = r.changes
	if on {
		r.engine[chat] = st
	} else {
		delete(r.engine, chat)
	}
}

// remoteActive reports whether the phone shares the chat this window
// shows, or a share is starting.
func (m *UI) remoteActive() bool {
	return m.remote.server != nil || m.remote.starting || m.engineShared()
}

// shownChat is the chat this window shows; "" for its new chat.
func (m *UI) shownChat() string {
	if m.hasSession() {
		return m.session.ID
	}
	return ""
}

// engineShared reports whether the engine shares the chat this window
// shows.
func (m *UI) engineShared() bool {
	_, ok := m.remote.engine[m.shownChat()]
	return ok
}

// beginStart marks a start attempt in flight and returns its generation
// and context.
func (r *remoteShare) beginStart() (uint64, context.Context, context.CancelFunc) {
	r.starting = true
	ctx, cancel := context.WithCancel(context.Background())
	r.setupCancel = cancel
	return r.generation, ctx, cancel
}

// cancelStart stops any start attempt and setup step in flight.
func (r *remoteShare) cancelStart() {
	if r.setupCancel != nil {
		r.setupCancel()
		r.setupCancel = nil
	}
}

// endSetup abandons setup: in-flight results are ignored from now on.
func (r *remoteShare) endSetup() {
	r.generation++
	r.cancelStart()
	r.starting = false
	r.setupBusy = false
	r.quiet = false
}

// started records the running server.
func (r *remoteShare) started(s *remote.Server) {
	r.server = s
	r.presence = remote.Presence{}
	r.phones = nil
}

// stop turns the share off, returning the command that stops the server,
// or nil when it is not running.
func (r *remoteShare) stop() tea.Cmd {
	s := r.server
	if s == nil {
		return nil
	}
	r.server = nil
	return func() tea.Msg {
		s.Stop()
		return nil
	}
}

// newPhone records the connected phones and returns the first one that was
// not connected before.
func (r *remoteShare) newPhone(phones []string) (string, bool) {
	defer func() { r.phones = phones }()
	for _, p := range phones {
		if !slices.Contains(r.phones, p) {
			return p, true
		}
	}
	return "", false
}

// setPresence tells the phone what the window shows, reaching the server
// only on a change.
func (r *remoteShare) setPresence(p remote.Presence) {
	if r.server == nil || p == r.presence {
		return
	}
	r.presence = p
	r.server.SetPresence(p)
}

// wait waits for the next request or phone change of the share.
func (r *remoteShare) wait() tea.Cmd {
	s := r.server
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case a := <-s.Actions():
			return remoteActionMsg{action: a}
		case <-s.PhonesChanged():
			return remotePhonesMsg{}
		case <-s.Stopped():
			return nil
		}
	}
}

// openRemote shows the share of the chat this window shows, starting it
// first when it is off.
func (m *UI) openRemote() tea.Cmd {
	if m.remote.server != nil || m.engineShared() {
		m.openRemoteDialog()
		return nil
	}
	if m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		m.dialog.BringToFront(dialog.RemoteSetupID)
		return nil
	}
	if m.remote.starting {
		return nil
	}
	_, local := m.com.Workspace.(*workspace.AppWorkspace)
	_, shared := m.engineWorkspace()
	if !local && !shared {
		return util.ReportWarn("Remote Control needs Crush running in this terminal or on the project's engine, not on a server")
	}
	m.remote.generation++
	if !m.remote.quiet {
		m.dialog.OpenDialog(remotecontrol.NewRemoteSetup(m.com))
	}
	return m.checkRemoteSetup()
}

func (m *UI) checkRemoteSetup() tea.Cmd {
	if m.remote.starting || m.remote.setupBusy {
		return nil
	}
	if cws, shared := m.engineWorkspace(); shared {
		return m.startEngineRemote(cws)
	}
	ws, ok := m.com.Workspace.(*workspace.AppWorkspace)
	if !ok {
		return nil
	}
	generation, ctx, cancel := m.remote.beginStart()
	src := remote.NewSource(ws)
	return func() tea.Msg {
		defer cancel()
		s, err := remote.StartContext(ctx, src)
		return remoteStartedMsg{server: s, err: err, generation: generation}
	}
}

func (m *UI) handleRemoteStarted(msg remoteStartedMsg) tea.Cmd {
	if msg.generation != m.remote.generation || (!m.remote.quiet && !m.dialog.ContainsDialog(dialog.RemoteSetupID)) {
		if msg.server != nil {
			return func() tea.Msg { msg.server.Stop(); return nil }
		}
		return nil
	}
	m.remote.starting = false
	m.remote.setupCancel = nil
	if msg.err != nil {
		if m.remote.quiet {
			m.remote.quiet = false
			return util.ReportError(errors.New("Remote Control: " + msg.err.Error()))
		}
		info := remote.SetupInfo{Title: "Sharing could not start", Detail: msg.err.Error() + ". Choose Check again to retry."}
		var setupErr *remote.SetupError
		if errors.As(msg.err, &setupErr) {
			info = setupErr.Info
		}
		m.remote.setupInfo = info
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*remotecontrol.RemoteSetup); ok {
			d.SetInfo(info)
		}
		if setupErr != nil && info.State != remote.SetupUnsupported {
			return m.pollRemoteSetup()
		}
		return nil
	}
	m.dialog.CloseDialog(dialog.RemoteSetupID)
	m.remote.started(msg.server)
	m.syncRemotePresence()
	m.status.SetRemote(true)
	if !m.remote.quiet {
		m.openRemoteDialog()
	}
	m.remote.quiet = false
	return m.remote.wait()
}

func (m *UI) pollRemoteSetup() tea.Cmd {
	generation := m.remote.generation
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return remoteSetupPollMsg{generation} })
}

func (m *UI) handleRemoteSetupPoll(msg remoteSetupPollMsg) tea.Cmd {
	if msg.generation != m.remote.generation || !m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		return nil
	}
	return m.checkRemoteSetup()
}

func (m *UI) closeRemoteSetup() {
	m.remote.endSetup()
	m.dialog.CloseDialog(dialog.RemoteSetupID)
}

func (m *UI) runRemoteSetup(action string) tea.Cmd {
	if !m.dialog.ContainsDialog(dialog.RemoteSetupID) || m.remote.setupBusy {
		return nil
	}
	if action == "check" {
		return m.checkRemoteSetup()
	}
	// Only the action currently shown in the local dialog is authorized.
	if action == "" || action != m.remote.setupInfo.Action {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return util.ReportError(err)
	}
	m.remote.cancelStart()
	m.remote.generation++
	generation := m.remote.generation
	m.remote.starting, m.remote.setupBusy = false, true
	// Suspend the TUI so system password prompts and Tailscale's QR code
	// reach the owner directly. No output is captured in a chat or log.
	cmd := exec.Command(exe, "remote", "setup", action)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return remoteSetupFinishedMsg{generation, err} })
}

func (m *UI) handleRemoteSetupFinished(msg remoteSetupFinishedMsg) tea.Cmd {
	if msg.generation != m.remote.generation || !m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		return nil
	}
	m.remote.setupBusy = false
	if msg.err != nil {
		info := m.remote.setupInfo
		info.Title = "Setup did not finish"
		info.Detail = "The step was cancelled or failed. You can retry it, choose Check again if you finished elsewhere, or cancel setup."
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*remotecontrol.RemoteSetup); ok {
			d.SetInfo(info)
		}
		return nil
	}
	return m.checkRemoteSetup()
}

func (m *UI) openRemoteDialog() {
	if m.dialog.ContainsDialog(dialog.RemoteID) {
		m.dialog.BringToFront(dialog.RemoteID)
		return
	}
	if m.engineShared() {
		m.dialog.OpenDialog(remotecontrol.NewRemote(m.com, m.engineRemoteStatus))
		return
	}
	s := m.remote.server
	m.dialog.OpenDialog(remotecontrol.NewRemote(m.com, func() remotecontrol.RemoteStatus {
		st := s.Status()
		_, port, _ := net.SplitHostPort(st.Addr)
		return remotecontrol.RemoteStatus{Host: st.Host, Port: port, Phones: st.Phones}
	}))
}

// stopRemote turns the share of the chat this window shows off and
// disconnects its phones.
func (m *UI) stopRemote() tea.Cmd {
	m.closeRemoteSetup()
	m.dialog.CloseDialog(dialog.RemoteID)
	if ws, shared := m.engineWorkspace(); shared && m.engineShared() {
		return m.stopEngineRemote(ws)
	}
	stop := m.remote.stop()
	if stop == nil {
		return nil
	}
	m.status.SetRemote(false)
	return tea.Sequence(stop, util.ReportInfo("Remote Control is off"))
}

// handleRemotePhones says in the status bar when a phone connects.
func (m *UI) handleRemotePhones() tea.Cmd {
	if m.remote.server == nil {
		return nil
	}
	var cmd tea.Cmd
	if p, ok := m.remote.newPhone(m.remote.server.Status().Phones); ok {
		cmd = util.ReportInfo("Remote Control: " + p + " connected")
	}
	return tea.Batch(cmd, m.remote.wait())
}

// syncRemotePresence tells the phone what the window shows. It runs after
// every update and only reaches the server on a change. A window on the
// shared engine always tells the engine, which shares chats whichever app
// started it, and shows REMOTE while the chat it shows is shared.
func (m *UI) syncRemotePresence() {
	if m.remote.engine != nil {
		m.status.SetRemote(m.engineShared())
	}
	_, shared := m.engineWorkspace()
	if m.remote.server == nil && !shared {
		return
	}
	p := remote.Presence{
		Focused: m.caps.ReportFocusEvents && m.notifyWindowFocused,
		Plan:    m.mode == uiInputModePlan,
	}
	if m.hasSession() {
		p.SessionID = m.session.ID
	}
	if shared {
		m.syncEnginePresence(p)
		return
	}
	m.remote.setPresence(p)
}

// errRemoteReplyPending is returned by runRemoteAction when the action's
// command answers the phone itself once its work finished.
var errRemoteReplyPending = errors.New("remote reply pending")

// handleRemoteAction runs a phone request as if its key had been pressed.
func (m *UI) handleRemoteAction(a *remote.Action) tea.Cmd {
	cmd, err := m.runRemoteAction(a)
	if !errors.Is(err, errRemoteReplyPending) {
		a.Done(err)
	}
	return tea.Batch(cmd, m.remote.wait())
}

func (m *UI) runRemoteAction(a *remote.Action) (tea.Cmd, error) {
	// No session is the window's new chat: the phone's first message starts
	// it, as typing it here would.
	var current string
	if m.hasSession() {
		current = m.session.ID
	}
	if a.SessionID != current {
		return nil, errors.New("the Crush window switched to another chat")
	}
	switch a.Kind {
	case remote.ActSend:
		if !m.agentReady {
			return nil, errors.New("the agent is not ready yet")
		}
		return m.sendPrompt(a.Text, a.Attachments), nil
	case remote.ActCancel:
		return m.cancelAgent(), nil
	case remote.ActInterrupt:
		return m.interruptAgent(), nil
	case remote.ActBackground:
		g := m.chat.BackgroundableGroup()
		if g == nil || current == "" || !m.com.Workspace.AgentBackground(current) {
			return nil, errors.New("no running command can move to the background")
		}
		g.Backgrounded()
		return nil, nil
	case remote.ActClearQueue:
		if current != "" {
			m.clearPromptQueue()
		}
		return nil, nil
	case remote.ActYolo:
		if m.mode == uiInputModePlan && (m.isAgentBusy() || m.modeSwitching) {
			return nil, errAgentBusy
		}
		return m.toggleYoloCommand(), nil
	}

	// The rest change the model or mode, which the TUI refuses mid-turn.
	if m.isAgentBusy() || m.modeSwitching {
		return nil, errAgentBusy
	}
	switch a.Kind {
	case remote.ActModel:
		return m.remoteSelectModel(a.Provider, a.Model)
	case remote.ActEffort:
		// The phone hears back once the effort was saved, off the Update
		// goroutine.
		cmd, err := m.setReasoningEffort(a.Effort, nil, func(err error) tea.Cmd {
			a.Done(err)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return cmd, errRemoteReplyPending
	case remote.ActThink:
		return m.toggleThinking(), nil
	case remote.ActFast:
		if p := m.coderProvider(); p == nil || p.Type != config.TypeCodexCLI {
			return nil, errors.New("FAST mode requires a Codex model")
		}
		return m.toggleFastMode(), nil
	case remote.ActUltracode:
		return m.toggleUltracode(), nil
	case remote.ActMode:
		return m.toggleInputMode(), nil
	case remote.ActSummarize:
		if current == "" {
			return nil, errors.New("a new chat has nothing to summarize yet")
		}
		return m.summarizeSession(current), nil
	}
	return nil, errors.New("unknown request " + strconv.Itoa(int(a.Kind)))
}

// coderProvider is the provider of the shown chat's model, or nil.
func (m *UI) coderProvider() *config.ProviderConfig {
	_, provider, _, _ := m.shownModelInfo()
	return provider
}

// remoteSelectModel switches the coder to a model of a set-up provider,
// like picking it in the models dialog.
func (m *UI) remoteSelectModel(providerID, modelID string) (tea.Cmd, error) {
	cfg := m.com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}
	pc, ok := cfg.Providers.Get(providerID)
	if !ok || pc.Disable {
		return nil, errors.New("that CLI is not set up on the computer")
	}
	models := pc.AvailableModels()
	var model *catwalk.Model
	for i := range models {
		if models[i].ID == modelID {
			model = &models[i]
			break
		}
	}
	if model == nil {
		return nil, errors.New("that model is not available on the computer")
	}
	return m.handleSelectModel(dialog.ActionSelectModel{
		Provider: catwalk.Provider{
			ID:     catwalk.InferenceProvider(pc.ID),
			Name:   cmp.Or(pc.Name, pc.ID),
			Type:   pc.Type,
			Models: models,
		},
		Model: config.SelectedModel{
			Model:           model.ID,
			Provider:        pc.ID,
			ReasoningEffort: model.DefaultReasoningEffort,
			MaxTokens:       model.DefaultMaxTokens,
		},
		ModelType: config.SelectedModelTypeChat,
	}), nil
}
