package models

import "github.com/charmbracelet/crush/internal/ui/dialog"

// RefreshStyles implements [dialog.StyleRefresher].
func (m *Models) RefreshStyles() {
	m.help.Styles = m.com.Styles.DialogHelpStyles()
	m.input.SetStyles(m.com.Styles.TextInput)
	for i := range m.list.groups {
		g := &m.list.groups[i]
		if g.Versioned != nil {
			g.Bump()
		}
		dialog.RefreshItems(g.Items)
	}
}

// RefreshStyles implements [dialog.StyleRefresher].
func (m *ModelItem) RefreshStyles() {
	m.cache = nil
	if m.Versioned != nil {
		m.Bump()
	}
}
