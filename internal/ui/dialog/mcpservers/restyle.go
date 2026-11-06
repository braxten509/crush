package mcpservers

// RefreshStyles implements [dialog.StyleRefresher].
func (m *MCPAuth) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
	m.spinner.Style = m.com.Styles.Dialog.OAuth.Spinner
}

// RefreshStyles implements [dialog.StyleRefresher].
func (m *MCPToggles) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
}
