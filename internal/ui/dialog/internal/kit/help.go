package kit

import (
	"cmp"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// RenderDialogHelp renders keybind hints as a single padded footer line at
// contentWidth (the dialog's inner width: total minus the View border). The
// hints are packed greedily and truncated with an ellipsis so the line never
// wraps or overflows the border, and never ends on a dangling separator.
func RenderDialogHelp(t *styles.Styles, h *help.Model, km help.KeyMap, contentWidth int) string {
	textWidth := max(0, contentWidth-t.Dialog.HelpView.GetHorizontalFrameSize())
	return t.Dialog.HelpView.Render(ShortHelpLine(h, km.ShortHelp(), textWidth))
}

// ShortHelpLine builds a single-line short help view truncated to width.
// It reimplements the bubbles help packing to avoid a component bug where
// items are kept even when they overflow (when the ellipsis itself does not
// fit), and to guarantee the line ends cleanly rather than on a separator.
func ShortHelpLine(h *help.Model, bindings []key.Binding, width int) string {
	if width <= 0 {
		return ""
	}
	sep := h.Styles.ShortSeparator.Inline(true).Render(h.ShortSeparator)
	ellipsis := h.Styles.Ellipsis.Inline(true).Render(cmp.Or(h.Ellipsis, "…"))

	var b strings.Builder
	total := 0
	for _, kb := range bindings {
		if !kb.Enabled() {
			continue
		}
		seg := ""
		if total > 0 {
			seg = sep
		}
		seg += h.Styles.ShortKey.Inline(true).Render(kb.Help().Key) + " " +
			h.Styles.ShortDesc.Inline(true).Render(kb.Help().Desc)
		w := lipgloss.Width(seg)
		if total+w > width {
			// The next item doesn't fit; add an ellipsis if there's room.
			// The separator belongs to this dropped item, so what we've
			// written already ends on a real hint, not a dangling dot. A
			// leading space joins the ellipsis to prior hints, but only
			// when there are prior hints.
			tail := ellipsis
			if total > 0 {
				tail = " " + ellipsis
			}
			if total+lipgloss.Width(tail) <= width {
				b.WriteString(tail)
			}
			break
		}
		total += w
		b.WriteString(seg)
	}
	return b.String()
}
