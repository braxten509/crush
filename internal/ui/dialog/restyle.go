package dialog

// StyleRefresher is implemented by dialogs, inline editors and list items
// that copy styles or cache renders when they are built. RefreshStyles
// makes them pick up the current styles after the theme was swapped in
// place (a theme switch, a preview, or a host application's theme).
type StyleRefresher interface {
	RefreshStyles()
}

// RefreshStyles has every open dialog pick up the current styles.
func (d *Overlay) RefreshStyles() {
	for _, dialog := range d.dialogs {
		if r, ok := dialog.(StyleRefresher); ok {
			r.RefreshStyles()
		}
	}
}

// RefreshItems has every item that implements [StyleRefresher] pick up the
// current styles, including items a filter hides.
func RefreshItems[T any](items []T) {
	for _, item := range items {
		if r, ok := any(item).(StyleRefresher); ok {
			r.RefreshStyles()
		}
	}
}

// RefreshStyles implements [StyleRefresher].
func (c *Commands) RefreshStyles() {
	c.help.Styles = c.com.Styles.DialogHelpStyles()
	c.input.SetStyles(c.com.Styles.TextInput)
	c.spinner.Style = c.com.Styles.Dialog.Spinner
	RefreshItems(c.list.Items())
}

// RefreshStyles implements [StyleRefresher].
func (r *Reasoning) RefreshStyles() {
	r.help.Styles = r.com.Styles.DialogHelpStyles()
	r.input.SetStyles(r.com.Styles.TextInput)
	RefreshItems(r.list.Items())
}

// RefreshStyles implements [StyleRefresher].
func (n *Notifications) RefreshStyles() {
	n.help.Styles = n.com.Styles.DialogHelpStyles()
	n.input.SetStyles(n.com.Styles.TextInput)
	RefreshItems(n.list.Items())
}

// RefreshStyles implements [StyleRefresher].
func (a *Arguments) RefreshStyles() {
	a.help.Styles = a.com.Styles.DialogHelpStyles()
	for i := range a.inputs {
		a.inputs[i].SetStyles(a.com.Styles.TextInput)
	}
	a.spinner.Style = a.com.Styles.Dialog.Spinner
}

// RefreshStyles implements [StyleRefresher]. The file list is restyled on
// every draw.
func (f *FilePicker) RefreshStyles() {
	f.help.Styles = f.com.Styles.DialogHelpStyles()
}

// RefreshStyles implements [StyleRefresher].
func (c *CommandItem) RefreshStyles() {
	c.cache = nil
	if c.Versioned != nil {
		c.Bump()
	}
}

// RefreshStyles implements [StyleRefresher].
func (r *ReasoningItem) RefreshStyles() {
	r.cache = nil
	if r.Versioned != nil {
		r.Bump()
	}
}

// RefreshStyles implements [StyleRefresher].
func (n *NotificationItem) RefreshStyles() {
	n.cache = nil
	if n.Versioned != nil {
		n.Bump()
	}
}
