package dialog

import (
	"image"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// newQuestionTextarea creates a configured textarea for question
// input. All question textareas share the same base configuration;
// only placeholder and char limit vary.
func newQuestionTextarea(sty *styles.Styles, placeholder string, charLimit int) textarea.Model {
	ta := textarea.New()
	taStyles := sty.Editor.Textarea
	taStyles.Cursor.Color = sty.Editor.QuestionCursorBar.GetForeground()
	ta.SetStyles(taStyles)
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.CharLimit = charLimit
	ta.MaxWidth = choiceListMaxWidth
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 3
	ta.SetHeight(1)
	ta.SetPromptFunc(0, func(textarea.PromptInfo) string { return "" })
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithDisabled())
	ta.Blur()
	return ta
}

// questionEditor owns the fill-in textarea, note editor, and notes
// map shared across all question component types. Components embed
// this struct and call its methods instead of reimplementing editor
// logic.
type questionEditor struct {
	Styles *styles.Styles

	fillIn        textarea.Model
	noteEditor    textarea.Model
	activeNoteKey string // non-empty when a note editor is open
	notes         map[string]string

	keyNote key.Binding
	navUp   key.Binding
	navDown key.Binding
}

// fillInMinHeight and fillInMaxHeight bound the fill-in textarea.
const (
	fillInMinHeight = 1
	fillInMaxHeight = 4
)

// questionBarWidth is the width of the "┃ " gutter bar in front of every
// choice-list row.
const questionBarWidth = 2

// newQuestionEditor creates a questionEditor with configured
// fill-in and note textareas.
func newQuestionEditor(sty *styles.Styles) questionEditor {
	fillIn := newQuestionTextarea(sty, "Something else?", 500)
	fillIn.MinHeight = fillInMinHeight
	fillIn.MaxHeight = fillInMaxHeight
	fillIn.SetHeight(fillInMinHeight)
	return questionEditor{
		Styles:     sty,
		fillIn:     fillIn,
		noteEditor: newQuestionTextarea(sty, "Add a note...", 300),
		notes:      make(map[string]string),
		keyNote:    key.NewBinding(key.WithKeys("alt+n"), key.WithHelp("alt+n", "note")),
		navUp:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up")),
		navDown:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down")),
	}
}

// openNote opens the note editor for the given key, pre-populating
// it with any existing note text.
func (e *questionEditor) openNote(noteKey string) tea.Cmd {
	e.activeNoteKey = noteKey
	if existing, ok := e.notes[noteKey]; ok {
		e.noteEditor.SetValue(existing)
	} else {
		e.noteEditor.Reset()
	}
	return e.noteEditor.Focus()
}

// closeNote saves the current note text and closes the editor.
func (e *questionEditor) closeNote(noteKey string) {
	e.activeNoteKey = ""
	val := strings.TrimSpace(e.noteEditor.Value())
	if val != "" {
		e.notes[noteKey] = val
	} else {
		delete(e.notes, noteKey)
	}
	e.noteEditor.Blur()
}

// handleNoteKey processes keys when the note editor is focused.
// Returns (cmd, handled). When handled is true the caller should
// not process the key further. onClose is called for the close
// key so the caller can control what happens after closing.
func (e *questionEditor) handleNoteKey(msg tea.KeyPressMsg, closeKey key.Binding, onClose func()) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, closeKey):
		onClose()
		return nil, true
	case key.Matches(msg, e.navUp), key.Matches(msg, e.navDown):
		onClose()
		return nil, false
	default:
		if key.Matches(msg, key.NewBinding(key.WithKeys("enter"))) {
			onClose()
			return nil, true
		}
		var cmd tea.Cmd
		e.noteEditor, cmd = e.noteEditor.Update(msg)
		return cmd, true
	}
}

// handlePaste forwards a paste message to the currently focused
// textarea (note editor or fill-in). Returns nil if no textarea
// is focused.
func (e *questionEditor) handlePaste(msg tea.PasteMsg) tea.Cmd {
	if e.activeNoteKey != "" && e.noteEditor.Focused() {
		var cmd tea.Cmd
		e.noteEditor, cmd = e.noteEditor.Update(msg)
		return cmd
	}
	if e.fillIn.Focused() {
		var cmd tea.Cmd
		e.fillIn, cmd = e.fillIn.Update(msg)
		return cmd
	}
	return nil
}

// drawFillIn appends fill-in rows to lines. The fill-in is a thin answer
// box (see answerPad), drawn whether or not it is being edited, so the
// form keeps its height when typing starts. When focused, it renders the
// live textarea; otherwise the saved text or the placeholder. styleFilled
// controls whether non-empty fill-in text gets the selected (pink)
// style. Pass true for single-choice where the fill-in IS the answer;
// false for multi-choice where it's supplementary.
func (e *questionEditor) drawFillIn(lines *[]contentLine, innerWidth int, bar, fillPrefix string, isActive bool, styleFilled bool) {
	prefixWidth := lipgloss.Width(fillPrefix)
	boxWidth := innerWidth - questionBarWidth
	textWidth := boxWidth - prefixWidth - 1 // one cell of padding on the right
	editing := isActive && e.fillIn.Focused()

	var rows []string
	if editing {
		e.fillIn.SetWidth(textWidth)
		rows = strings.Split(e.fillIn.View(), "\n")
	} else {
		style := e.Styles.Editor.QuestionUnselected
		if styleFilled {
			style = e.Styles.Editor.QuestionSelected
		}
		text := e.Styles.Editor.QuestionBody.Render("Something else?")
		if val := strings.TrimSpace(e.fillIn.Value()); val != "" {
			text = style.Render(ansi.Wrap(val, textWidth, ""))
		}
		rows = strings.Split(text, "\n")
		for len(rows) < fillInMinHeight {
			rows = append(rows, "")
		}
	}

	*lines = append(*lines, contentLine{text: bar + answerPad(e.Styles, boxWidth, true), cursorItem: isActive, choiceIdx: -1})
	indent := strings.Repeat(" ", prefixWidth)
	for j, row := range rows {
		prefix := fillPrefix
		if j > 0 {
			prefix = indent
		}
		*lines = append(*lines, contentLine{text: bar + prefix + row, band: true, fillInRow: editing && j == 0, cursorItem: isActive, choiceIdx: -1})
	}
	*lines = append(*lines, contentLine{text: bar + answerPad(e.Styles, boxWidth, false), cursorItem: isActive, choiceIdx: -1})
}

// answerBand is the background of an answer box: the composer's band
// color.
func answerBand(sty *styles.Styles) color.Color {
	return sty.Editor.Textarea.Focused.Base.GetBackground()
}

// answerPad returns a half-row of padding, width cells wide, for the top
// (above the text) or bottom of an answer box. Half blocks in the band
// color make the box thinner than the composer's full blank rows, so a
// question's answer field doesn't read as the chat box.
func answerPad(sty *styles.Styles, width int, top bool) string {
	block := "▀"
	if top {
		block = "▄"
	}
	return lipgloss.NewStyle().Foreground(answerBand(sty)).Render(strings.Repeat(block, max(0, width)))
}

// answerBar is the gutter bar beside an answer box, lit while the box is
// the active item.
func answerBar(sty *styles.Styles, active bool) string {
	if active {
		return sty.Editor.QuestionCursorBar.Render("┃ ")
	}
	return strings.Repeat(" ", questionBarWidth)
}

// drawNote appends note rows to lines for the given key. When the
// note editor is active, renders the live textarea; otherwise shows
// saved note text or nothing.
func (e *questionEditor) drawNote(lines *[]contentLine, innerWidth int, bar, barInactive, noteKey string, isActive bool) {
	noteStyle := e.Styles.Editor.QuestionNote
	isEditing := e.activeNoteKey == noteKey && e.noteEditor.Focused()
	const notePrefix = "> "

	if isEditing && e.noteEditor.Focused() {
		prefixWidth := lipgloss.Width(notePrefix)
		e.noteEditor.SetWidth(innerWidth - questionBarWidth - prefixWidth)
		indent := strings.Repeat(" ", prefixWidth)
		for j, tl := range strings.Split(e.noteEditor.View(), "\n") {
			text := bar + notePrefix + tl
			if j > 0 {
				text = barInactive + indent + tl
			}
			*lines = append(*lines, contentLine{text: text, noteRow: j == 0, cursorItem: true, choiceIdx: -1})
		}
		return
	}

	if saved, ok := e.notes[noteKey]; ok && saved != "" {
		dimmed := noteStyle.Render(saved)
		for _, ln := range strings.Split(dimmed, "\n") {
			*lines = append(*lines, contentLine{text: bar + notePrefix + ln, cursorItem: isActive, choiceIdx: -1})
		}
	}
}

// fillInCursor returns the cursor position, relative to the content
// area, for the fill-in textarea when it's focused. screenRow is the
// row of its first line; prefixWidth is the width of the prompt between
// the gutter bar and the text.
func (e *questionEditor) fillInCursor(screenRow, prefixWidth int) *tea.Cursor {
	if !e.fillIn.Focused() {
		return nil
	}
	return textareaCursor(e.fillIn.Cursor(), screenRow, prefixWidth)
}

// noteCursor returns the cursor position for the note editor when it's
// focused, like fillInCursor.
func (e *questionEditor) noteCursor(screenRow, prefixWidth int) *tea.Cursor {
	if !e.noteEditor.Focused() {
		return nil
	}
	return textareaCursor(e.noteEditor.Cursor(), screenRow, prefixWidth)
}

// textareaCursor moves a textarea's cursor past the gutter bar and the
// prompt to where its text is drawn.
func textareaCursor(tc *tea.Cursor, screenRow, prefixWidth int) *tea.Cursor {
	if tc == nil {
		return nil
	}
	tc.X += questionBarWidth + prefixWidth
	tc.Y += screenRow
	return tc
}

// drawStandaloneNote draws a note editor or saved note directly
// onto the screen (not via line list). Used by YesNo which doesn't
// use the line-list model. Returns the cursor or nil.
func (e *questionEditor) drawStandaloneNote(scr uv.Screen, area uv.Rectangle, y int, noteKey string) (*tea.Cursor, int) {
	const notePrefix = "> "

	if e.activeNoteKey != "" && e.noteEditor.Focused() {
		y++
		prefixWidth := lipgloss.Width(notePrefix)
		e.noteEditor.SetWidth(area.Dx() - 2 - prefixWidth)
		noteView := e.noteEditor.View()
		var cur *tea.Cursor
		for j, ln := range strings.Split(noteView, "\n") {
			text := notePrefix + ln
			if j > 0 {
				text = strings.Repeat(" ", prefixWidth) + ln
			}
			lines := drawStyledText(scr, image.Rect(area.Min.X, y, area.Max.X, y+1), text)
			if j == 0 {
				if tc := e.noteEditor.Cursor(); tc != nil {
					tc.X += prefixWidth
					tc.Y += y - area.Min.Y
					cur = tc
				}
			}
			y += lines
		}
		return cur, y
	}

	if saved, ok := e.notes[noteKey]; ok && saved != "" {
		y++
		noteStyle := e.Styles.Editor.QuestionNote
		drawStyledText(scr, image.Rect(area.Min.X, y, area.Max.X, y+1), notePrefix+noteStyle.Render(saved))
		y++
	}

	return nil, y
}
