package dialog

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// CommandItem wraps a uicmd.Command to implement the kit.ListItem interface.
type CommandItem struct {
	*list.Versioned
	id          string
	title       string
	shortcut    string
	description string
	action      Action
	aliases     []string
	t           *styles.Styles
	m           fuzzy.Match
	cache       map[int]string
	focused     bool
	hideInfo    bool
	muted       bool
}

var _ kit.ListItem = &CommandItem{Versioned: list.NewVersioned()}

// NewCommandItem creates a new CommandItem.
func NewCommandItem(t *styles.Styles, id, title, shortcut string, action Action) *CommandItem {
	return &CommandItem{
		Versioned: list.NewVersioned(),
		id:        id,
		t:         t,
		title:     title,
		shortcut:  shortcut,
		action:    action,
	}
}

// Finished implements list.Item. Command items are render-stable
// outside of explicit SetFocused / SetMatch.
func (c *CommandItem) Finished() bool {
	return true
}

// WithAliases returns the CommandItem with the given aliases for filtering.
func (c *CommandItem) WithAliases(aliases ...string) *CommandItem {
	c.aliases = aliases
	return c
}

// WithDescription returns the CommandItem with a description displayed below
// the title.
func (c *CommandItem) WithDescription(desc string) *CommandItem {
	c.description = desc
	return c
}

func (c *CommandItem) WithMuted(muted bool) *CommandItem {
	c.muted = muted
	return c
}

// Filter implements kit.ListItem.
func (c *CommandItem) Filter() string {
	base := c.title
	if len(c.aliases) > 0 {
		base = c.title + " " + strings.Join(c.aliases, " ")
	}
	if c.description != "" {
		base = base + " " + c.description
	}
	return base
}

// ID implements kit.ListItem.
func (c *CommandItem) ID() string {
	return c.id
}

// SetFocused implements kit.ListItem.
func (c *CommandItem) SetFocused(focused bool) {
	if c.focused == focused {
		return
	}
	c.cache = nil
	c.focused = focused
	if c.Versioned != nil {
		c.Bump()
	}
}

// SetMatch implements kit.ListItem.
func (c *CommandItem) SetMatch(m fuzzy.Match) {
	if kit.SameFuzzyMatch(c.m, m) {
		return
	}
	c.cache = nil
	c.m = m
	if c.Versioned != nil {
		c.Bump()
	}
}

// Action returns the action associated with the command item.
func (c *CommandItem) Action() Action {
	return c.action
}

// Shortcut returns the shortcut associated with the command item.
func (c *CommandItem) Shortcut() string {
	return c.shortcut
}

// InfoText implements kit.InfoColumnItem; the command shortcut is its info.
func (c *CommandItem) InfoText() string {
	return c.shortcut
}

// SetHideInfo controls whether the shortcut hint column is shown. The
// dialog hides it uniformly when it would crowd the command names.
func (c *CommandItem) SetHideInfo(v bool) {
	if c.hideInfo == v {
		return
	}
	c.cache = nil
	c.hideInfo = v
	if c.Versioned != nil {
		c.Bump()
	}
}

// Render implements kit.ListItem.
func (c *CommandItem) Render(width int) string {
	styles := kit.ListItemStyles{
		ItemBlurred:     c.t.Dialog.NormalItem,
		ItemFocused:     c.t.Dialog.SelectedItem,
		InfoTextBlurred: c.t.Dialog.ListItem.InfoBlurred,
		InfoTextFocused: c.t.Dialog.ListItem.InfoFocused,
	}
	if c.muted && !c.focused {
		styles.ItemBlurred = styles.ItemBlurred.Foreground(c.t.Dialog.SecondaryText.GetForeground())
		styles.InfoTextBlurred = styles.InfoTextBlurred.Foreground(c.t.Dialog.SecondaryText.GetForeground())
	}
	shortcut := c.shortcut
	if c.hideInfo {
		shortcut = ""
	}
	rendered := kit.RenderItem(styles, c.title, shortcut, c.focused, width, c.cache, &c.m)
	if c.description != "" {
		descStyle := c.t.Dialog.SecondaryText
		if c.focused {
			descStyle = c.t.Dialog.SelectedItem
		}
		contentWidth := max(0, width-descStyle.GetHorizontalFrameSize()+1)
		description := ansi.Truncate(strings.TrimSpace(c.description), contentWidth, "...")
		descVisWidth := lipgloss.Width(description)
		gap := strings.Repeat(" ", max(0, contentWidth-descVisWidth))
		if description == "" {
			description = " "
		}
		rendered = lipgloss.JoinVertical(lipgloss.Left, rendered, descStyle.Render(description+gap))
	}
	return rendered
}
