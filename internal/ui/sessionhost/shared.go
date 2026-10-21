package sessionhost

import (
	"log/slog"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/openchats"
)

// The chat rows follow the open-chats list the host shares with Crush
// Desktop (internal/openchats; owner, 2026-10-20: "the two should be in
// sync"). A chat a row shows (its Status.Session) is listed; a row closed
// here takes its chat off the list; a chat listed elsewhere gets a row
// that starts its Crush when first shown; a chat taken off the list
// elsewhere closes its row. Quitting the host leaves the list as it is:
// the chats close only once every app is closed (openchats.Join).

// openChatsPoll is how often the host reads the list for changes made
// elsewhere.
const openChatsPoll = time.Second

type openChatsTickMsg struct{}

func (h *Host) pollOpenChats() tea.Cmd {
	if h.chats == nil {
		return nil
	}
	return tea.Tick(openChatsPoll, func(time.Time) tea.Msg { return openChatsTickMsg{} })
}

// syncOpenChats brings the rows in line with the list. It reads the list
// on the update loop, after this host's own writes, so a chat it just
// listed is never taken for one removed elsewhere.
func (h *Host) syncOpenChats() {
	if h.chats == nil {
		return
	}
	chats := h.chats.Chats()
	listed := make(map[string]bool, len(chats))
	for _, c := range chats {
		listed[c.ID] = true
	}
	for _, s := range slices.Clone(h.sessions) {
		if s.kind != chatSession || s.chatID == "" || listed[s.chatID] {
			continue
		}
		if s.waiting {
			h.dropRow(s.id)
			continue
		}
		s.chatID = "" // already off the list
		s.stop()
	}
	for _, c := range chats {
		if !slices.ContainsFunc(h.sessions, func(s *session) bool { return s.chatID == c.ID }) {
			h.addWaitingRow(c)
		}
	}
}

// addWaitingRow lists a chat opened elsewhere without starting its Crush,
// after the other chats; showing it starts it (wake).
func (h *Host) addWaitingRow(c openchats.Chat) {
	h.nextID++
	s := &session{id: h.nextID, kind: chatSession, waiting: true, chatID: c.ID, chatDir: c.Dir, chatTitle: c.Title}
	s.status = Status{Title: c.Title, Dir: c.Dir, Session: c.ID}
	i := len(h.sessions)
	for i > 0 && h.sessions[i-1].kind == terminalSession {
		i--
	}
	h.sessions = slices.Insert(h.sessions, i, s)
	if i <= h.active && len(h.sessions) > 1 {
		h.active++
	}
}

// wake starts the Crush of a waiting row on its chat, in its place. False
// when it can't start; the row stays waiting and the notice says why.
func (h *Host) wake(i int) bool {
	s := h.sessions[i]
	if !s.waiting {
		return true
	}
	if h.opts.NewArgs == nil {
		return false
	}
	w, ht := h.sessionSize()
	h.nextID++
	args := append(h.opts.NewArgs(s.chatDir), "--session", s.chatID)
	l := launch{exe: h.opts.Exe, args: args, dir: s.chatDir, env: h.opts.Env}
	started, err := startSession(h.nextID, l, w, ht, h.fg, h.bg, h.send, func(n *session) {
		n.kitty.Store(h.kitty)
		h.shareCellSize(n)
	})
	if err != nil {
		slog.Error("Could not start a Crush session", "error", err)
		h.notice = "Couldn't start a session."
		return false
	}
	started.chatID, started.chatDir, started.chatTitle = s.chatID, s.chatDir, s.chatTitle
	started.status = s.status
	h.sessions[i] = started
	return true
}

// dropRow takes a row off the list without touching the shared list.
func (h *Host) dropRow(id int) {
	i := slices.IndexFunc(h.sessions, func(s *session) bool { return s.id == id })
	if i < 0 {
		return
	}
	h.sessions = slices.Delete(h.sessions, i, i+1)
	if h.confirm != nil && h.confirm.id == id {
		h.confirm = nil
	}
	if i < h.active || h.active >= len(h.sessions) {
		h.active = max(0, h.active-1)
	}
}

// listChat keeps the list in step with the chat a row reports: a new chat
// is added, a row that moved to another chat swaps it in, and a title or
// folder change is written. A waiting row for the same chat goes.
func (h *Host) listChat(s *session, st Status) {
	if h.chats == nil || s.kind != chatSession || st.Session == "" {
		return
	}
	if st.Session == s.chatID && st.Title == s.chatTitle && st.Dir == s.chatDir {
		return
	}
	c := openchats.Chat{ID: st.Session, Dir: st.Dir, Title: st.Title}
	var err error
	if s.chatID != "" && s.chatID != st.Session {
		err = h.chats.Swap(s.chatID, c)
	} else {
		err = h.chats.Add(c)
	}
	if err != nil {
		slog.Warn("Could not update the open chats", "error", err)
	}
	s.chatID, s.chatTitle, s.chatDir = st.Session, st.Title, st.Dir
	for _, other := range slices.Clone(h.sessions) {
		if other != s && other.waiting && other.chatID == st.Session {
			h.dropRow(other.id)
		}
	}
}

// unlistExited takes a closed row's chat off the list: closed here, or
// ended while other rows remain. The last row ending is the host quitting,
// which leaves the list alone.
func (h *Host) unlistExited(s *session) {
	if h.chats == nil || s == nil || s.kind != chatSession || s.chatID == "" || h.leaving {
		return
	}
	if !s.closedHere && len(h.sessions) <= 1 {
		return
	}
	if err := h.chats.Remove(s.chatID); err != nil {
		slog.Warn("Could not update the open chats", "error", err)
	}
}
