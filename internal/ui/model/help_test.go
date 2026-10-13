package model

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/x/exp/golden"
)

func helpText(binds []key.Binding) string {
	parts := make([]string, 0, len(binds))
	for _, b := range binds {
		if !b.Enabled() {
			parts = append(parts, "-")
			continue
		}
		parts = append(parts, b.Help().Key+"="+b.Help().Desc)
	}
	return strings.Join(parts, ", ")
}

// TestHelpBindings pins the short and full help for every combination of
// state, focus and the conditions the help depends on.
func TestHelpBindings(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	for _, state := range []uiState{uiInitialize, uiLanding, uiChat} {
		for _, focus := range []uiFocusState{uiFocusEditor, uiFocusMain, uiFocusSidebar} {
			for variant := range 64 {
				busy := variant&1 != 0
				queued := variant&2 != 0
				hasSession := variant&4 != 0
				inline := variant&8 != 0
				typed := variant&16 != 0
				focusOnly := variant&32 != 0

				u := newRoutingUI(t)
				u.state, u.focus = state, focus
				u.com.Workspace.(*cmdStateWorkspace).cfg.Options.TUI.ComposerFocusOnly = focusOnly
				if busy {
					setBusy(u)
				}
				if queued {
					u.queue.count = 2
				}
				if hasSession {
					u.session = &session.Session{ID: "s1", Todos: []session.Todo{{Content: "x", Status: session.TodoStatusPending}}}
					u.pillsExpanded = true
					u.attachments.Update(message.Attachment{FileName: "a.txt"})
				} else {
					u.session = nil
				}
				if inline {
					u.activeInline = &recInline{}
				}
				if typed {
					u.textarea.SetValue("hi")
				}
				fmt.Fprintf(&out, "state=%d focus=%d busy=%t queued=%t session=%t inline=%t typed=%t focusOnly=%t\n",
					state, focus, busy, queued, hasSession, inline, typed, focusOnly)
				fmt.Fprintf(&out, "  short: %s\n", helpText(u.ShortHelp()))
				for _, row := range u.FullHelp() {
					fmt.Fprintf(&out, "  full: %s\n", helpText(row))
				}
			}
		}
	}
	golden.RequireEqual(t, []byte(out.String()))
}
