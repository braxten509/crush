package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/notification"
)

type secureQuestionClosedMsg struct{ form *secureentry.Form }

func (m *UI) secureQuestionFormOpen() bool {
	form, ok := m.activeInline.(*dialog.QuestionForm)
	return ok && form.HasSecureEntry()
}

func (m *UI) dropQuestionForm() {
	if form, ok := m.activeInline.(*dialog.QuestionForm); ok && form != nil {
		form.Dismiss()
		m.activeInline = nil
	}
}

// handleSecureEntryMsg handles the secure entry's own messages, and drops
// clipboard input while a secure entry holds a secret but something else
// has the keys. ok reports that msg goes no further.
func (m *UI) handleSecureEntryMsg(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	switch typed := msg.(type) {
	case secureQuestionClosedMsg:
		if form, ok := m.activeInline.(*dialog.QuestionForm); ok && form.OwnsSecureForm(typed.form) {
			m.dropQuestionForm()
			m.textarea.Focus()
			m.updateLayoutAndSize()
		}
		return nil, true
	case dialog.SecureQuestionPaste:
		if form, ok := m.activeInline.(*dialog.QuestionForm); ok {
			form.HandleSecurePaste(typed)
		} else {
			typed.Discard()
		}
		m.frame.invalidate()
		return nil, true
	case *secureentry.Form:
		return m.openSecureQuestionForm(typed), true
	}
	if m.inputOwner() != ownerSecureEntry {
		return nil, false
	}
	switch typed := msg.(type) {
	case tea.PasteMsg:
		return nil, true
	case tea.KeyPressMsg:
		if typed.String() == "ctrl+v" || typed.String() == "shift+insert" {
			return nil, true
		}
	}
	return nil, false
}

func (m *UI) openSecureQuestionForm(form *secureentry.Form) tea.Cmd {
	if form.Closed() {
		return nil
	}
	var cmd tea.Cmd
	if m.cliUpdatePromptOpen() {
		cmd = m.movePromptToStatus()
		m.activeInline = nil
	}
	m.dropQuestionForm()
	m.openInline(dialog.NewSecureQuestionForm(m.com.Styles, form))
	m.updateLayoutAndSize()
	m.chat.ScrollToBottom()
	return tea.Batch(cmd, m.playNotificationSound(notification.SoundQuestion), func() tea.Msg { <-form.Done(); return secureQuestionClosedMsg{form: form} })
}
