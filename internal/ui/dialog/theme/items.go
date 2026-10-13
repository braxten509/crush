package theme

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// ThemeSectionHeader is a non-selectable section divider in the theme
// list. It renders as a titled horizontal rule via common.Section.
type ThemeSectionHeader struct {
	*list.Versioned
	title string
	t     *styles.Styles
}

func (h *ThemeSectionHeader) Filter() string { return "" }
func (h *ThemeSectionHeader) Finished() bool { return true }
func (h *ThemeSectionHeader) Render(width int) string {
	return common.Section(h.t, " "+h.title+" ", width)
}

// themeSpacer is a filterable spacer that adds vertical space between
// sections in the theme list. Unlike list.SpacerItem it satisfies
// FilterableItem so it can live in a FilterableList.
type themeSpacer struct {
	*list.Versioned
	height int
}

func (s *themeSpacer) Filter() string          { return "" }
func (s *themeSpacer) Finished() bool          { return true }
func (s *themeSpacer) Render(width int) string { return strings.Repeat("\n", s.height) }

// ThemeItem represents a single theme entry in the picker.
type ThemeItem struct {
	*list.Versioned
	name       string
	label      string
	isCurrent  bool
	overridden bool
	t          *styles.Styles
	m          fuzzy.Match
	cache      map[int]string
	focused    bool
	hideInfo   bool

	mode        themesMode
	renameInput textinput.Model
}

// Finished implements list.Item. Theme items are render-stable outside
// of explicit SetFocused / SetMatch calls.
func (r *ThemeItem) Finished() bool {
	return true
}

func (th *Theme) setThemeItems() {
	currentTheme := th.currentThemeName()
	allThemes := styles.ListAllThemes()

	// Separate builtin (including overridden) from user-only themes.
	var systemThemes, userThemes []styles.ThemeInfo
	for _, info := range allThemes {
		if info.Source == styles.ThemeSourceBuiltin || info.Overridden {
			systemThemes = append(systemThemes, info)
		} else {
			userThemes = append(userThemes, info)
		}
	}

	items := make([]list.FilterableItem, 0, len(allThemes)+5)

	// "New Theme..." sentinel — no divider above it.
	items = append(items, &ThemeItem{
		Versioned: &list.Versioned{},
		name:      newThemeItemName,
		label:     "New Theme...",
		t:         th.com.Styles,
		mode:      th.mode,
	})

	// Spacer after "New Theme...".
	items = append(items, &themeSpacer{Versioned: &list.Versioned{}, height: 1})

	// System section.
	items = append(items, &ThemeSectionHeader{
		Versioned: &list.Versioned{},
		title:     "System",
		t:         th.com.Styles,
	})
	for _, info := range systemThemes {
		items = append(items, th.newThemeItem(info, currentTheme))
	}

	// User section — only shown when custom themes exist.
	if len(userThemes) > 0 {
		items = append(items, &themeSpacer{Versioned: &list.Versioned{}, height: 1})
		items = append(items, &ThemeSectionHeader{
			Versioned: &list.Versioned{},
			title:     "User",
			t:         th.com.Styles,
		})
		for _, info := range userThemes {
			items = append(items, th.newThemeItem(info, currentTheme))
		}
	}

	th.list.SetItems(items...)

	// Restore selection or default to the currently active theme, matching
	// the behavior of the session and model pickers.
	switch {
	case (th.mode == themesModeRenaming || th.mode == themesModeDeleting) &&
		th.selectedIndex >= 0 && th.selectedIndex < len(items):
		th.list.SetSelected(th.selectedIndex)
	default:
		selected := 0
		for i, it := range items {
			if ti, ok := it.(*ThemeItem); ok && strings.EqualFold(ti.name, currentTheme) && ti.name != newThemeItemName {
				selected = i
				break
			}
		}
		th.list.SetSelected(selected)
	}
	th.list.ScrollToSelected()
}

func (th *Theme) newThemeItem(info styles.ThemeInfo, currentTheme string) *ThemeItem {
	label := info.Name
	if info.Overridden {
		label += " (overridden)"
	}
	item := &ThemeItem{
		Versioned:  &list.Versioned{},
		name:       info.Name,
		label:      label,
		isCurrent:  strings.EqualFold(info.Name, currentTheme),
		overridden: info.Overridden,
		t:          th.com.Styles,
		mode:       th.mode,
	}
	if th.mode == themesModeRenaming && th.selectedIndex >= 0 {
		filteredItems := th.list.FilteredItems()
		if th.selectedIndex < len(filteredItems) {
			if si, ok := filteredItems[th.selectedIndex].(*ThemeItem); ok && si.name == info.Name {
				item.renameInput = textinput.New()
				item.renameInput.SetVirtualCursor(false)
				item.renameInput.Prompt = ""
				inputStyle := th.com.Styles.TextInput
				inputStyle.Focused.Placeholder = th.com.Styles.Dialog.Sessions.RenamingPlaceholder
				item.renameInput.SetStyles(inputStyle)
				item.renameInput.SetValue(info.Name)
				item.renameInput.Focus()
			}
		}
	}
	return item
}

func (r *ThemeItem) Filter() string {
	return r.label
}

func (r *ThemeItem) ID() string {
	return r.name
}

func (r *ThemeItem) SetFocused(focused bool) {
	if r.focused != focused {
		r.cache = nil
		r.Bump()
	}
	r.focused = focused
}

func (r *ThemeItem) SetMatch(m fuzzy.Match) {
	if !kit.SameFuzzyMatch(r.m, m) {
		r.cache = nil
		r.Bump()
	}
	r.m = m
}

// InfoText returns the secondary text shown on the right of the item.
func (r *ThemeItem) InfoText() string {
	if r.isCurrent {
		return "current"
	}
	return ""
}

// SetHideInfo controls whether the info column is shown.
func (r *ThemeItem) SetHideInfo(v bool) {
	if r.hideInfo == v {
		return
	}
	r.cache = nil
	r.hideInfo = v
	if r.Versioned != nil {
		r.Bump()
	}
}

func (r *ThemeItem) InputValue() string {
	return r.renameInput.Value()
}

func (r *ThemeItem) HandleInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	r.renameInput, cmd = r.renameInput.Update(msg)
	if r.Versioned != nil {
		r.Bump()
	}
	return cmd
}

func (r *ThemeItem) Cursor() *tea.Cursor {
	return r.renameInput.Cursor()
}

func (r *ThemeItem) Render(width int) string {
	info := r.InfoText()
	if r.hideInfo {
		info = ""
	}
	s := kit.ListItemStyles{
		ItemBlurred:     r.t.Dialog.NormalItem,
		ItemFocused:     r.t.Dialog.SelectedItem,
		InfoTextBlurred: r.t.Dialog.Sessions.InfoBlurred,
		InfoTextFocused: r.t.Dialog.Sessions.InfoFocused,
	}

	switch r.mode {
	case themesModeDeleting:
		s.ItemBlurred = r.t.Dialog.Sessions.DeletingItemBlurred
		s.ItemFocused = r.t.Dialog.Sessions.DeletingItemFocused
	case themesModeRenaming:
		s.ItemBlurred = r.t.Dialog.Sessions.RenamingItemBlurred
		s.ItemFocused = r.t.Dialog.Sessions.RenamingingItemFocused
		if r.focused {
			const cursorPadding = 1
			inputWidth := max(0, width-s.ItemFocused.GetHorizontalFrameSize()-cursorPadding)
			r.renameInput.SetWidth(inputWidth)
			r.renameInput.Placeholder = ansi.Truncate(r.label, width, "…")
			return s.ItemFocused.Render(r.renameInput.View())
		}
	}

	return kit.RenderItem(s, r.label, info, r.focused, width, r.cache, &r.m)
}
