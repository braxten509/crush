package kit

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/list"
)

// Maximum share of a list row width the secondary info column may take
// before it is hidden entirely, so it never crowds out the item name.
// Command shortcuts are small and non-essential, so they yield sooner
// than the larger, more useful session timestamps.
const (
	SessionInfoMaxPercent = 35
	CommandInfoMaxPercent = 25
)

// InfoColumnItem is a list item with a secondary info column (a session
// timestamp, a command shortcut) that can be hidden when space is tight.
type InfoColumnItem interface {
	// InfoText returns the raw info string, or "" when there is none.
	InfoText() string
	// SetHideInfo toggles whether the info column is rendered.
	SetHideInfo(bool)
}

// ApplyInfoColumnVisibility hides the secondary info column across every
// item uniformly when its widest entry would take more than maxPercent of
// rowWidth, so item names keep their room. It returns once rowWidth grows
// enough for the widest entry to fit within the budget again.
func ApplyInfoColumnVisibility(items []list.Item, rowWidth, maxPercent int) {
	widest := 0
	for _, it := range items {
		if ic, ok := it.(InfoColumnItem); ok {
			if info := ic.InfoText(); info != "" {
				widest = max(widest, lipgloss.Width(" "+info+" "))
			}
		}
	}
	hide := rowWidth > 0 && widest*100 > rowWidth*maxPercent
	for _, it := range items {
		if ic, ok := it.(InfoColumnItem); ok {
			ic.SetHideInfo(hide)
		}
	}
}
