package sessions

import "github.com/charmbracelet/crush/internal/ui/dialog"

// RefreshStyles implements [dialog.StyleRefresher].
func (s *Session) RefreshStyles() {
	s.help.Styles = s.com.Styles.DialogHelpStyles()
	s.input.SetStyles(s.com.Styles.TextInput)
	dialog.RefreshItems(s.list.Items())
}

// RefreshStyles implements [dialog.StyleRefresher].
func (s *SessionItem) RefreshStyles() {
	s.cache = nil
	if s.sessionsMode == sessionsModeUpdating {
		s.updateTitleInput.SetStyles(sessionRenameInputStyles(s.t))
	}
	if s.Versioned != nil {
		s.Bump()
	}
}
