package sessions

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/dustin/go-humanize"
	"github.com/sahilm/fuzzy"
)

// SessionItem wraps a [session.Session] to implement the [kit.ListItem] interface.
type SessionItem struct {
	*list.Versioned
	session.Session
	t                *styles.Styles
	sessionsMode     sessionsMode
	m                fuzzy.Match
	cache            map[int]string
	updateTitleInput textinput.Model
	focused          bool
	hideInfo         bool
}

// Finished implements list.Item. Session items are render-stable
// outside of explicit SetFocused / SetMatch calls, both of which
// bump Version() and therefore invalidate the F6 frozen entry.
func (s *SessionItem) Finished() bool {
	return true
}

var _ kit.ListItem = &SessionItem{}

// Filter returns the filterable value of the session.
func (s *SessionItem) Filter() string {
	return s.Title
}

// ID returns the unique identifier of the session.
func (s *SessionItem) ID() string {
	return s.Session.CatalogID()
}

// SetMatch sets the fuzzy match for the session item.
func (s *SessionItem) SetMatch(m fuzzy.Match) {
	if kit.SameFuzzyMatch(s.m, m) {
		return
	}
	s.cache = nil
	s.m = m
	if s.Versioned != nil {
		s.Bump()
	}
}

// InputValue returns the updated title value
func (s *SessionItem) InputValue() string {
	return s.updateTitleInput.Value()
}

// HandleInput forwards input message to the update title input
func (s *SessionItem) HandleInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	s.updateTitleInput, cmd = s.updateTitleInput.Update(msg)
	if s.Versioned != nil {
		s.Bump()
	}
	return cmd
}

// Cursor returns the cursor of the update title input
func (s *SessionItem) Cursor() *tea.Cursor {
	return s.updateTitleInput.Cursor()
}

// InfoText returns the secondary text shown on the right of the item.
func (s *SessionItem) InfoText() string {
	when := humanize.Time(time.Unix(s.UpdatedAt, 0))
	if s.Directory != "" {
		return home.Short(s.Directory) + " · " + when
	}
	return when
}

// SetHideInfo controls whether the timestamp info column is shown. The
// dialog hides it uniformly when it would crowd the title.
func (s *SessionItem) SetHideInfo(v bool) {
	if s.hideInfo == v {
		return
	}
	s.cache = nil
	s.hideInfo = v
	if s.Versioned != nil {
		s.Bump()
	}
}

// Render returns the string representation of the session item.
func (s *SessionItem) Render(width int) string {
	info := s.InfoText()
	if s.hideInfo {
		info = ""
	}
	styles := kit.ListItemStyles{
		ItemBlurred:     s.t.Dialog.NormalItem,
		ItemFocused:     s.t.Dialog.SelectedItem,
		InfoTextBlurred: s.t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: s.t.Dialog.Sessions.InfoFocused,
	}

	switch s.sessionsMode {
	case sessionsModeDeleting:
		styles.ItemBlurred = s.t.Dialog.Sessions.DeletingItemBlurred
		styles.ItemFocused = s.t.Dialog.Sessions.DeletingItemFocused
	case sessionsModeUpdating:
		styles.ItemBlurred = s.t.Dialog.Sessions.RenamingItemBlurred
		styles.ItemFocused = s.t.Dialog.Sessions.RenamingingItemFocused
		if s.focused {
			const cursorPadding = 1
			inputWidth := max(0, width-styles.ItemFocused.GetHorizontalFrameSize()-cursorPadding)
			s.updateTitleInput.SetWidth(inputWidth)
			s.updateTitleInput.Placeholder = ansi.Truncate(s.Title, width, "…")
			return styles.ItemFocused.Render(s.updateTitleInput.View())
		}
	}

	return kit.RenderItem(styles, s.Title, info, s.focused, width, s.cache, &s.m)
}

// SetFocused sets the focus state of the session item.
func (s *SessionItem) SetFocused(focused bool) {
	if s.focused == focused {
		return
	}
	s.cache = nil
	s.focused = focused
	if s.Versioned != nil {
		s.Bump()
	}
}

// sessionItems takes a slice of [session.Session]s and convert them to a slice
// of [kit.ListItem]s.
func sessionItems(t *styles.Styles, mode sessionsMode, sessions ...session.Session) []list.FilterableItem {
	items := make([]list.FilterableItem, len(sessions))
	for i, s := range sessions {
		item := &SessionItem{Versioned: list.NewVersioned(), Session: s, t: t, sessionsMode: mode}
		if mode == sessionsModeUpdating {
			item.updateTitleInput = textinput.New()
			item.updateTitleInput.SetVirtualCursor(false)
			item.updateTitleInput.Prompt = ""
			inputStyle := t.TextInput
			inputStyle.Focused.Placeholder = t.Dialog.Sessions.RenamingPlaceholder
			item.updateTitleInput.SetStyles(inputStyle)
			item.updateTitleInput.SetValue(s.Title)
			item.updateTitleInput.Focus()
		}
		items[i] = item
	}
	return items
}
