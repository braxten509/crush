package permissions

// RefreshStyles implements [dialog.StyleRefresher].
func (p *Permissions) RefreshStyles() {
	p.help.Styles = p.com.Styles.DialogHelpStyles()
	// The viewport keeps the rendered content, diffs included, until it
	// is marked dirty.
	p.viewportDirty = true
}
