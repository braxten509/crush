//go:build !windows

package sessionhost

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/openchats"
	"github.com/stretchr/testify/require"
)

func chatIDs(l openchats.List) []string {
	out := []string{}
	for _, c := range l.Chats() {
		out = append(out, c.ID)
	}
	return out
}

func rowTitles(h *Host) []string {
	out := []string{}
	for _, s := range h.sessions {
		out = append(out, s.snapshot().Title)
	}
	return out
}

// The rows and the list shared with Crush Desktop stay in step both ways
// (owner, 2026-10-20: "if I open one in the terminal it shows in desktop
// and vice versa").
func TestRowsFollowTheSharedChatList(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	list := openchats.At(filepath.Join(t.TempDir(), "open-chats.json"))
	leave, err := list.Join("terminal")
	require.NoError(t, err)
	defer leave()
	h.chats = &list

	// Each row's chat is listed once it reports it.
	for id, chat := range map[int]string{1: "c1", 2: "c2"} {
		s := h.byID(id)
		s.status.Session, s.status.Dir = chat, "/p"
		h.statusChanged(id)
	}
	require.Equal(t, []string{"c1", "c2"}, chatIDs(list))

	// A chat opened in the desktop gets a row that waits to start.
	require.NoError(t, list.Add(openchats.Chat{ID: "c3", Dir: "/q", Title: "Three"}))
	h.Update(openChatsTickMsg{})
	require.Equal(t, []string{"One", "Two", "Three"}, rowTitles(h))
	require.True(t, h.sessions[2].waiting)
	require.Contains(t, screenText(h), "Three")

	// A chat closed in the desktop closes its row here.
	require.NoError(t, list.Remove("c1"))
	h.Update(openChatsTickMsg{})
	require.True(t, h.byID(1).stopped, "its Crush is asked to exit")
	h.Update(exitedMsg{id: 1})
	require.Equal(t, []string{"Two", "Three"}, rowTitles(h))
	require.Equal(t, []string{"c2", "c3"}, chatIDs(list))

	// Closing a row here takes its chat off the list.
	h.confirm = &confirmClose{id: h.sessions[1].id}
	h.answerClose(true)
	require.Equal(t, []string{"Two"}, rowTitles(h))
	require.Equal(t, []string{"c2"}, chatIDs(list))

	// The last row ending is the host quitting: its chat stays for the desktop.
	h.Update(exitedMsg{id: 2})
	require.Equal(t, []string{"c2"}, chatIDs(list))
}
