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
	m.activeInline = dialog.NewSecureQuestionForm(m.com.Styles, form)
	m.textarea.Blur()
	m.focus = uiFocusEditor
	m.activeInline.SetFocused(true)
	m.updateLayoutAndSize()
	m.chat.ScrollToBottom()
	return tea.Batch(cmd, m.playNotificationSound(notification.SoundQuestion), func() tea.Msg { <-form.Done(); return secureQuestionClosedMsg{form: form} })
}
