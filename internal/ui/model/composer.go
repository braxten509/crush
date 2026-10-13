package model

import (
	"math/rand"

	"charm.land/bubbles/v2/textarea"
)

// composer is the prompt editor: the textarea and the bang mode, history
// and placeholders around it. The UI embeds it, so its fields and methods
// read as the UI's own.
type composer struct {
	textarea textarea.Model

	// textareaMouseSelecting tracks whether a mouse selection gesture is
	// currently in progress within the textarea (left button held after a
	// click inside the textarea region).
	textareaMouseSelecting bool

	// bangMode tracks whether the editor is in bang (!) shell mode.
	bangMode     bool
	bangWasEmpty bool // true when bang prompt became empty on last keystroke

	readyPlaceholder   string
	workingPlaceholder string

	// Prompt history for up/down navigation through previous messages.
	promptHistory struct {
		messages []string
		index    int
		draft    string
	}
}

// updateHistoryDraft updates history state when text is modified.
func (c *composer) updateHistoryDraft(oldValue string) {
	if c.textarea.Value() != oldValue {
		c.promptHistory.draft = c.textarea.Value()
		c.promptHistory.index = -1
	}
}

// historyReset resets the history, but does not clear the message
// it just sets the current draft to empty and the position in the history.
func (c *composer) historyReset() {
	c.promptHistory.index = -1
	c.promptHistory.draft = ""
}

// isAtEditorStart returns true if we are at the 0 line and 0 col in the textarea.
func (c *composer) isAtEditorStart() bool {
	return c.textarea.Line() == 0 && c.textarea.LineInfo().ColumnOffset == 0
}

// isAtEditorEnd returns true if we are in the last line and the last column in the textarea.
func (c *composer) isAtEditorEnd() bool {
	lineCount := c.textarea.LineCount()
	if lineCount == 0 {
		return true
	}
	if c.textarea.Line() != lineCount-1 {
		return false
	}
	info := c.textarea.LineInfo()
	return info.CharOffset >= info.CharWidth-1 || info.CharWidth == 0
}

// textareaWord returns the current word at the cursor position.
func (c *composer) textareaWord() string {
	return c.textarea.Word()
}

// randomizePlaceholders selects random placeholder text for the textarea's
// ready and working states.
func (c *composer) randomizePlaceholders() {
	c.workingPlaceholder = workingPlaceholders[rand.Intn(len(workingPlaceholders))]
	c.readyPlaceholder = readyPlaceholders[rand.Intn(len(readyPlaceholders))]
}

// placeholder is the editor's placeholder for the mode: bang mode, YOLO
// coding, a busy agent, planning or ready.
func (c *composer) placeholder(busy, plan, yolo bool) string {
	switch {
	case c.bangMode:
		return "Run a shell command"
	case !plan && yolo:
		return "Go crazy"
	case busy:
		return c.workingPlaceholder
	case plan:
		return "Let's plan"
	default:
		return c.readyPlaceholder
	}
}
