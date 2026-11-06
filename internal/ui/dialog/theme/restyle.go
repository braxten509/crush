package theme

// RefreshStyles implements [dialog.StyleRefresher].
func (ed *ThemeEditor) RefreshStyles() {
	ed.help.Styles = ed.com.Styles.DialogHelpStyles()
	ed.input.SetStyles(ed.com.Styles.TextInput)
}

// RefreshStyles implements [dialog.StyleRefresher].
func (d *ThemeNew) RefreshStyles() {
	d.help.Styles = d.com.Styles.DialogHelpStyles()
	d.input.SetStyles(d.com.Styles.TextInput)
}
