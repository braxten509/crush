package auth

// RefreshStyles implements [dialog.StyleRefresher]. The input is restyled
// on every draw, for the state it is in.
func (m *APIKeyInput) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
	m.spinner.Style = m.com.Styles.Dialog.APIKey.Spinner
}

// RefreshStyles implements [dialog.StyleRefresher].
func (m *AuthMethod) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
}

// RefreshStyles implements [dialog.StyleRefresher].
func (m *AWSSSO) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
	m.spinner.Style = m.com.Styles.Dialog.OAuth.Spinner
}

// RefreshStyles implements [dialog.StyleRefresher].
func (m *OAuth) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
	m.spinner.Style = m.com.Styles.Dialog.OAuth.Spinner
	if m.oAuthProvider.supportsCodeEntry() {
		m.codeInput.SetStyles(m.com.Styles.TextInput)
	}
}
