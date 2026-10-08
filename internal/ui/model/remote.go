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

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
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

// openRemote shows the share, starting it first when it is off.
func (m *UI) openRemote() tea.Cmd {
	if m.remote != nil {
		m.openRemoteDialog()
		return nil
	}
	if m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		m.dialog.BringToFront(dialog.RemoteSetupID)
		return nil
	}
	if m.remoteStarting {
		return nil
	}
	_, ok := m.com.Workspace.(*workspace.AppWorkspace)
	if !ok {
		return util.ReportWarn("Remote Control needs Crush running in this terminal, not as a client")
	}
	m.remoteGeneration++
	if !m.remoteQuiet {
		m.dialog.OpenDialog(dialog.NewRemoteSetup(m.com))
	}
	return m.checkRemoteSetup()
}

func (m *UI) checkRemoteSetup() tea.Cmd {
	if m.remoteStarting || m.remoteSetupBusy {
		return nil
	}
	ws, ok := m.com.Workspace.(*workspace.AppWorkspace)
	if !ok {
		return nil
	}
	m.remoteStarting = true
	generation := m.remoteGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.remoteSetupCancel = cancel
	src := remote.NewSource(ws)
	return func() tea.Msg {
		defer cancel()
		s, err := remote.StartContext(ctx, src)
		return remoteStartedMsg{server: s, err: err, generation: generation}
	}
}

func (m *UI) handleRemoteStarted(msg remoteStartedMsg) tea.Cmd {
	if msg.generation != m.remoteGeneration || (!m.remoteQuiet && !m.dialog.ContainsDialog(dialog.RemoteSetupID)) {
		if msg.server != nil {
			return func() tea.Msg { msg.server.Stop(); return nil }
		}
		return nil
	}
	m.remoteStarting = false
	m.remoteSetupCancel = nil
	if msg.err != nil {
		if m.remoteQuiet {
			m.remoteQuiet = false
			return util.ReportError(errors.New("Remote Control: " + msg.err.Error()))
		}
		info := remote.SetupInfo{Title: "Sharing could not start", Detail: msg.err.Error() + ". Choose Check again to retry."}
		var setupErr *remote.SetupError
		if errors.As(msg.err, &setupErr) {
			info = setupErr.Info
		}
		m.remoteSetupInfo = info
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*dialog.RemoteSetup); ok {
			d.SetInfo(info)
		}
		if setupErr != nil && info.State != remote.SetupUnsupported {
			return m.pollRemoteSetup()
		}
		return nil
	}
	m.dialog.CloseDialog(dialog.RemoteSetupID)
	m.remote = msg.server
	m.remotePresence = remote.Presence{}
	m.remotePhones = nil
	m.syncRemotePresence()
	m.status.SetRemote(true)
	if !m.remoteQuiet {
		m.openRemoteDialog()
	}
	m.remoteQuiet = false
	return m.waitRemote()
}

func (m *UI) pollRemoteSetup() tea.Cmd {
	generation := m.remoteGeneration
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return remoteSetupPollMsg{generation} })
}

func (m *UI) handleRemoteSetupPoll(msg remoteSetupPollMsg) tea.Cmd {
	if msg.generation != m.remoteGeneration || !m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		return nil
	}
	return m.checkRemoteSetup()
}

func (m *UI) closeRemoteSetup() {
	m.remoteGeneration++
	if m.remoteSetupCancel != nil {
		m.remoteSetupCancel()
		m.remoteSetupCancel = nil
	}
	m.remoteStarting = false
	m.remoteSetupBusy = false
	m.remoteQuiet = false
	m.dialog.CloseDialog(dialog.RemoteSetupID)
}

func (m *UI) runRemoteSetup(action string) tea.Cmd {
	if !m.dialog.ContainsDialog(dialog.RemoteSetupID) || m.remoteSetupBusy {
		return nil
	}
	if action == "check" {
		return m.checkRemoteSetup()
	}
	// Only the action currently shown in the local dialog is authorized.
	if action == "" || action != m.remoteSetupInfo.Action {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return util.ReportError(err)
	}
	if m.remoteSetupCancel != nil {
		m.remoteSetupCancel()
		m.remoteSetupCancel = nil
	}
	m.remoteGeneration++
	generation := m.remoteGeneration
	m.remoteStarting, m.remoteSetupBusy = false, true
	// Suspend the TUI so system password prompts and Tailscale's QR code
	// reach the owner directly. No output is captured in a chat or log.
	cmd := exec.Command(exe, "remote", "setup", action)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return remoteSetupFinishedMsg{generation, err} })
}

func (m *UI) handleRemoteSetupFinished(msg remoteSetupFinishedMsg) tea.Cmd {
	if msg.generation != m.remoteGeneration || !m.dialog.ContainsDialog(dialog.RemoteSetupID) {
		return nil
	}
	m.remoteSetupBusy = false
	if msg.err != nil {
		info := m.remoteSetupInfo
		info.Title = "Setup did not finish"
		info.Detail = "The step was cancelled or failed. You can retry it, choose Check again if you finished elsewhere, or cancel setup."
		if d, ok := m.dialog.Dialog(dialog.RemoteSetupID).(*dialog.RemoteSetup); ok {
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
	s := m.remote
	m.dialog.OpenDialog(dialog.NewRemote(m.com, func() dialog.RemoteStatus {
		st := s.Status()
		_, port, _ := net.SplitHostPort(st.Addr)
		return dialog.RemoteStatus{Host: st.Host, Port: port, Phones: st.Phones}
	}))
}

// stopRemote turns the share off and disconnects the phones.
func (m *UI) stopRemote() tea.Cmd {
	m.closeRemoteSetup()
	m.dialog.CloseDialog(dialog.RemoteID)
	if m.remote == nil {
		return nil
	}
	s := m.remote
	m.remote = nil
	m.status.SetRemote(false)
	return tea.Sequence(func() tea.Msg {
		s.Stop()
		return nil
	}, util.ReportInfo("Remote Control is off"))
}

// handleRemotePhones says in the status bar when a phone connects.
func (m *UI) handleRemotePhones() tea.Cmd {
	if m.remote == nil {
		return nil
	}
	phones := m.remote.Status().Phones
	var cmd tea.Cmd
	for _, p := range phones {
		if !slices.Contains(m.remotePhones, p) {
			cmd = util.ReportInfo("Remote Control: " + p + " connected")
			break
		}
	}
	m.remotePhones = phones
	return tea.Batch(cmd, m.waitRemote())
}

// waitRemote waits for the next request or phone change of the share.
func (m *UI) waitRemote() tea.Cmd {
	s := m.remote
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

// syncRemotePresence tells the phone what the window shows. It runs after
// every update and only reaches the server on a change.
func (m *UI) syncRemotePresence() {
	if m.remote == nil {
		return
	}
	p := remote.Presence{
		Focused: m.caps.ReportFocusEvents && m.notifyWindowFocused,
		Plan:    m.mode == uiInputModePlan,
	}
	if m.hasSession() {
		p.SessionID = m.session.ID
	}
	if p != m.remotePresence {
		m.remotePresence = p
		m.remote.SetPresence(p)
	}
}

// handleRemoteAction runs a phone request as if its key had been pressed.
func (m *UI) handleRemoteAction(a *remote.Action) tea.Cmd {
	cmd, err := m.runRemoteAction(a)
	a.Done(err)
	return tea.Batch(cmd, m.waitRemote())
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
		return m.sendMessage(a.Text, a.Attachments...), nil
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
		return m.setReasoningEffort(a.Effort, nil)
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

func (m *UI) coderProvider() *config.ProviderConfig {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil
	}
	return cfg.GetProviderForModel(agentCfg.Model)
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
	var model *catwalk.Model
	for i := range pc.Models {
		if pc.Models[i].ID == modelID {
			model = &pc.Models[i]
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
			Models: pc.Models,
		},
		Model: config.SelectedModel{
			Model:           model.ID,
			Provider:        pc.ID,
			ReasoningEffort: model.DefaultReasoningEffort,
			MaxTokens:       model.DefaultMaxTokens,
		},
		ModelType: cmp.Or(cfg.Agents[config.AgentCoder].Model, config.SelectedModelTypeLarge),
	}), nil
}
