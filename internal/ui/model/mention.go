package model

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/completions"
	uv "github.com/charmbracelet/ultraviolet"
)

// mentionCompletions is the popup that completes an @ mention typed in the
// editor with a file or an MCP resource.
type mentionCompletions struct {
	popup *completions.Completions
	open  bool
	// startIndex is where in the editor text the @ was typed, query what
	// follows it.
	startIndex int
	query      string
	// position is the screen position where the user typed '@'.
	position image.Point
}

// triggers reports whether typing key into the editor holding value opens
// the popup: an @ at the beginning of the prompt or after whitespace.
func (c *mentionCompletions) triggers(key, value string) bool {
	if key != "@" || c.open {
		return false
	}
	idx := len(value)
	return idx == 0 || isWhitespace(value[idx-1])
}

// begin opens the popup for an @ typed at startIndex, shown at position.
func (c *mentionCompletions) begin(startIndex int, position image.Point, depth, limit int) tea.Cmd {
	c.open = true
	c.query = ""
	c.startIndex = startIndex
	c.position = position
	return c.popup.Open(depth, limit)
}

// follow keeps an open popup in step with the editor after key changed its
// text to value, with word the word at the cursor: the popup filters on the
// word after the @, and closes once the cursor moves before the @, on a
// space, or when the word no longer is a mention. The @ keystroke that
// opened the popup is skipped since its items are still loading.
func (c *mentionCompletions) follow(key, value, word string) {
	if !c.open || key == "@" {
		return
	}
	switch {
	case len(value) <= c.startIndex:
		// The cursor moved before the start.
		c.close()
	case key == "space":
		c.close()
	case strings.HasPrefix(word, "@"):
		c.query = word[1:]
		c.popup.Filter(c.query)
	default:
		c.close()
	}
}

// setItems fills an open popup with the items loaded for it.
func (c *mentionCompletions) setItems(msg completions.CompletionItemsLoadedMsg) {
	if c.open {
		c.popup.SetItems(msg.Files, msg.Resources)
	}
}

// close closes the popup and resets its state.
func (c *mentionCompletions) close() {
	c.open = false
	c.query = ""
	c.startIndex = 0
	c.popup.Close()
}

// draw draws an open popup with items just above where the @ was typed,
// kept within a screen screenW cells wide.
func (c *mentionCompletions) draw(scr uv.Screen, screenW int) {
	if !c.open || !c.popup.HasItems() {
		return
	}
	w, h := c.popup.Size()
	x := c.position.X
	y := c.position.Y - h

	if x+w > screenW {
		x = screenW - w
	}
	x = max(0, x)
	y = max(0, y+1) // Offset for attachments row

	completionsView := uv.NewStyledString(c.popup.Render())
	completionsView.Draw(scr, image.Rectangle{
		Min: image.Pt(x, y),
		Max: image.Pt(x+w, y+h),
	})
}
