package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// memoryCheckMsg carries one memory check, made off the UI thread.
type memoryCheckMsg struct {
	status memwatch.Status
}

func (m *UI) memoryCheckTick() tea.Cmd {
	return tea.Tick(memwatch.CheckEvery, func(time.Time) tea.Msg {
		return memoryCheckMsg{status: memwatch.Default.Check()}
	})
}

// handleMemoryCheck keeps the memory warning in the status bar while memory
// is high, and shows one desktop pop-up when it gets high.
func (m *UI) handleMemoryCheck(msg memoryCheckMsg) tea.Cmd {
	s := msg.status
	rose := s.High && !m.memoryHigh
	m.memoryHigh = s.High
	cmds := []tea.Cmd{m.memoryCheckTick()}
	showing := m.memoryWarning != "" && m.status.msg.Msg == m.memoryWarning
	switch {
	case s.High:
		// The warning has no expiry of its own: it is refreshed on every
		// check, and comes back within one check after another message
		// replaced it.
		if rose || showing || m.status.msg.IsEmpty() {
			m.memoryWarning = s.Message()
			m.status.SetInfoMsg(util.InfoMsg{Type: util.InfoTypeWarn, Msg: m.memoryWarning})
		}
		if rose {
			cmds = append(cmds, m.memoryPopup(s))
		}
	case showing:
		m.status.ClearInfoMsg()
		m.memoryWarning = ""
	}
	return tea.Batch(cmds...)
}

// memoryPopup sends the desktop pop-up even while Crush is focused: the
// warning matters most when the user is looking elsewhere, and the window
// may be one of several. Only one Crush window shows it per
// memwatch.NotifyEvery.
func (m *UI) memoryPopup(s memwatch.Status) tea.Cmd {
	cfg := m.com.Config()
	if cfg != nil && cfg.Options != nil && cfg.Options.Notifications == "disabled" {
		return nil
	}
	backend := m.notifyBackend
	if _, noop := backend.(notification.NoopBackend); noop && notification.NativeSupported {
		// Auto mode turns pop-ups off without focus reports, because
		// they can't be held back while the window is focused. This one
		// is meant to show either way.
		backend = notification.NewNativeBackend(notification.Icon)
	}
	n := notification.Notification{Title: "Crush · Memory almost full", Message: s.Message()}
	dir := config.GlobalCacheDir()
	return func() tea.Msg {
		if !memwatch.ClaimNotify(dir) {
			return nil
		}
		if send := backend.Send(n); send != nil {
			return send()
		}
		return nil
	}
}
