package model

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// lockedChatNote is shown when a locked chat opens (owner, 2026-11-03).
const lockedChatNote = "This chat is locked. Ctrl+P → Unlock Chat (KDE asks for the vault password)."

// lockChat locks the open chat into the vault, or an unlocked one again
// (owner, 2026-11-03). The engine moves it in the background; the chat
// hides at once and its lock state arrives as a session event.
func (m *UI) lockChat() tea.Cmd {
	if m.session == nil {
		return util.ReportWarn("Open a chat to lock it.")
	}
	if m.isAgentBusy() {
		return util.ReportWarn("Wait for the reply to finish before locking.")
	}
	ws, id := m.com.Workspace, m.session.ID
	return func() tea.Msg {
		if _, err := ws.LockSession(context.Background(), id); err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
		}
		return util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Locking this chat…"}
	}
}

// unlockChat unlocks the open chat; KDE asks for the vault password in its
// own box. With stop the chat becomes a normal chat for good.
func (m *UI) unlockChat(stop bool) tea.Cmd {
	// Unlock needs a hidden chat; Stop Locking works on any locked chat,
	// unlocked ones too.
	if m.session == nil || m.session.LockState == "" || !stop && !session.Hidden(m.session.LockState) {
		return util.ReportWarn("This chat is not locked.")
	}
	ws, id := m.com.Workspace, m.session.ID
	return func() tea.Msg {
		if _, err := ws.UnlockSession(context.Background(), id, stop); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
		}
		if stop {
			return util.InfoMsg{Type: util.InfoTypeInfo, Msg: "This chat is no longer locked."}
		}
		return util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Chat unlocked. It locks again when you close it or quit."}
	}
}
