package model

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

// focusOption narrows which components setFocus brings in line with the
// new focus.
type focusOption uint8

const (
	// keepChatFocus leaves the chat's focus as it is, e.g. for an inline
	// editor that opens over whatever had the keys.
	keepChatFocus focusOption = iota + 1
	// keepTextareaFocus leaves the textarea's focus as it is, for moves
	// between the chat and the sidebar.
	keepTextareaFocus
	// focusRecordOnly records the focus and touches no component, for
	// callers that sync them themselves.
	focusRecordOnly
)

// setFocus moves keyboard focus to f; every focus change goes through it.
// It keeps the components showing focus in step: the editor focuses the
// textarea, unless an inline editor replaces it, and anything else blurs
// it; the chat is focused exactly while it has the keys. It returns the
// textarea's focus command, if any. An inline editor's own focus is left to
// the caller.
func (m *UI) setFocus(f uiFocusState, opts ...focusOption) tea.Cmd {
	m.focus = f
	if slices.Contains(opts, focusRecordOnly) {
		return nil
	}
	var cmd tea.Cmd
	if !slices.Contains(opts, keepTextareaFocus) {
		if f == uiFocusEditor && m.activeInline == nil {
			cmd = m.textarea.Focus()
		} else {
			m.textarea.Blur()
		}
	}
	if m.chat != nil && !slices.Contains(opts, keepChatFocus) {
		if f == uiFocusMain {
			m.chat.Focus()
		} else {
			m.chat.Blur()
		}
	}
	return cmd
}

// openInline replaces the textarea with an inline editor that takes the
// keys. The chat keeps its focus state. Callers update the layout.
func (m *UI) openInline(e dialog.InlineEditor) {
	m.activeInline = e
	m.setFocus(uiFocusEditor, keepChatFocus)
	e.SetFocused(true)
}
