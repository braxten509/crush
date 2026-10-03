package dialog

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/clipboard"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// SecureQuestion is a masked field in a question form. Its buffer is the
// only copy of the value: Response never carries it, it never renders, and
// it is cleared once the form is submitted or cancelled. The form writes it
// to the prepared file only on the final Submit.
type SecureQuestion struct {
	Styles  *styles.Styles
	Request question.Question

	field     *secureentry.Field // nil outside the local terminal
	buffer    *secretBuffer      // behind a pointer so formatting the struct can't show it
	focused   bool
	errorText string

	keyEnter key.Binding
	keyPaste key.Binding
	keyClear key.Binding
}

type secretBuffer struct {
	runes    []rune
	position int
	size     int // UTF-8 bytes
}

func (*secretBuffer) String() string                 { return "(redacted)" }
func (*secretBuffer) GoString() string               { return "(redacted)" }
func (*secretBuffer) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, "(redacted)") }

// clipboardTimeout bounds the off-thread read. Its result is tagged to the
// exact secure field, so a late paste cannot land in another question or chat.
var clipboardTimeout = 500 * time.Millisecond

var clipboardText = func() []byte {
	text, _ := clipboard.Read(clipboard.FormatText)
	return text
}

// readClipboard returns the clipboard text, or nil when it takes too long.
// A late result is cleared and dropped.
func readClipboard() []byte {
	result := make(chan []byte, 1)
	read := clipboardText
	go func() { result <- read() }()
	select {
	case text := <-result:
		return text
	case <-time.After(clipboardTimeout):
		go func() { clear(<-result) }()
		return nil
	}
}

type SecureQuestionPaste struct {
	question *SecureQuestion
	value    []byte
}

func (SecureQuestionPaste) String() string   { return "secure question paste (redacted)" }
func (SecureQuestionPaste) GoString() string { return "secure question paste (redacted)" }
func (SecureQuestionPaste) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "secure question paste (redacted)")
}
func (p SecureQuestionPaste) Discard() { clear(p.value) }

// NewSecureQuestion creates a masked field. Without a prepared field it
// shows that secure entry needs the local terminal and takes no input.
func NewSecureQuestion(sty *styles.Styles, req question.Question, field *secureentry.Field) *SecureQuestion {
	return &SecureQuestion{
		Styles:   sty,
		Request:  req,
		field:    field,
		buffer:   &secretBuffer{},
		keyEnter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
		keyPaste: key.NewBinding(key.WithKeys("ctrl+v", "shift+insert"), key.WithHelp("ctrl+v", "paste")),
		keyClear: key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear")),
	}
}

func (*SecureQuestion) String() string   { return "secure question (redacted)" }
func (*SecureQuestion) GoString() string { return "secure question (redacted)" }
func (*SecureQuestion) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "secure question (redacted)")
}

// usable reports whether the field takes input.
func (s *SecureQuestion) usable() bool { return s.field != nil && s.field.Available() }

// HandleKey edits the masked value. Enter finishes the field once it has a
// value; nothing is saved until the form is submitted.
func (s *SecureQuestion) HandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.field == nil {
		return false, nil
	}
	if !s.usable() {
		return key.Matches(msg, s.keyEnter), nil
	}
	b := s.buffer
	switch {
	case key.Matches(msg, s.keyEnter):
		if len(b.runes) == 0 {
			s.errorText = "Enter a value first."
			return false, nil
		}
		s.errorText = ""
		return true, nil
	case key.Matches(msg, s.keyPaste):
		return false, func() tea.Msg { return SecureQuestionPaste{question: s, value: readClipboard()} }
	case key.Matches(msg, s.keyClear):
		s.Clear()
		s.errorText = ""
		return false, nil
	}
	switch msg.String() {
	case "backspace", "ctrl+h":
		if b.position > 0 {
			b.size -= utf8.RuneLen(b.runes[b.position-1])
			copy(b.runes[b.position-1:], b.runes[b.position:])
			b.runes[len(b.runes)-1] = 0
			b.runes = b.runes[:len(b.runes)-1]
			b.position--
		}
	case "delete":
		if b.position < len(b.runes) {
			b.size -= utf8.RuneLen(b.runes[b.position])
			copy(b.runes[b.position:], b.runes[b.position+1:])
			b.runes[len(b.runes)-1] = 0
			b.runes = b.runes[:len(b.runes)-1]
		}
	case "left":
		b.position = max(0, b.position-1)
	case "right":
		b.position = min(len(b.runes), b.position+1)
	case "home", "ctrl+a":
		b.position = 0
	case "end", "ctrl+e":
		b.position = len(b.runes)
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
			text := []byte(msg.Text)
			defer clear(text)
			s.insert(text)
		}
	}
	return false, nil
}

// HandlePaste takes a terminal paste into the masked value.
func (s *SecureQuestion) HandlePaste(msg tea.PasteMsg) tea.Cmd {
	if s.usable() {
		text := []byte(msg.Content)
		defer clear(text)
		s.insert(text)
	}
	return nil
}

// insert adds text at the cursor. One trailing line break, as copied keys
// often have, is dropped; anything else with control characters is refused.
func (s *SecureQuestion) insert(text []byte) {
	if len(text) == 0 {
		return
	}
	text = bytes.TrimSuffix(text, []byte("\n"))
	text = bytes.TrimSuffix(text, []byte("\r"))
	if !utf8.Valid(text) || bytes.IndexFunc(text, unicode.IsControl) >= 0 {
		s.errorText = "Use a single-line value without control characters."
		return
	}
	b := s.buffer
	if b.size+len(text) > secureentry.MaxValueSize {
		s.errorText = "Value exceeds 64 KiB."
		return
	}
	count := utf8.RuneCount(text)
	grown := make([]rune, len(b.runes)+count)
	copy(grown, b.runes[:b.position])
	at := b.position
	for len(text) > 0 {
		r, n := utf8.DecodeRune(text)
		grown[at] = r
		at++
		text = text[n:]
	}
	copy(grown[at:], b.runes[b.position:])
	clear(b.runes)
	b.runes = grown
	b.position += count
	b.size = 0
	for _, r := range b.runes {
		b.size += utf8.RuneLen(r)
	}
	s.errorText = ""
}

// Entered reports whether the field holds a value or already saved one.
func (s *SecureQuestion) Entered() bool {
	return len(s.buffer.runes) > 0 || (s.field != nil && s.field.Saved())
}

// Saved reports whether the value was written to its file.
func (s *SecureQuestion) Saved() bool { return s.field != nil && s.field.Saved() }

// value returns the entered value as new bytes for saving, or nil. The
// caller clears it.
func (s *SecureQuestion) value() []byte {
	if !s.usable() || len(s.buffer.runes) == 0 {
		return nil
	}
	out := make([]byte, 0, s.buffer.size)
	for _, r := range s.buffer.runes {
		out = utf8.AppendRune(out, r)
	}
	return out
}

// Clear wipes the buffer.
func (s *SecureQuestion) Clear() {
	clear(s.buffer.runes)
	*s.buffer = secretBuffer{}
}

// Response never carries the value.
func (s *SecureQuestion) Response() question.Answer {
	return question.Answer{QuestionID: s.Request.ID}
}

// GetRequest returns the underlying question.
func (s *SecureQuestion) GetRequest() question.Question { return s.Request }

// ShortHelp returns key bindings for the status bar.
func (s *SecureQuestion) ShortHelp() []key.Binding {
	if !s.usable() {
		return nil
	}
	return []key.Binding{s.keyEnter, s.keyPaste, s.keyClear}
}

// summary is the review text: whether a value is entered, never the value.
func (s *SecureQuestion) summary() string {
	switch {
	case s.Saved():
		return "Saved"
	case len(s.buffer.runes) > 0:
		return "Entered · saved on submit"
	}
	return "(not entered)"
}

// Height returns the content height: header, description, then the field,
// destination and status rows, which are always reserved so errors don't
// shift the layout.
func (s *SecureQuestion) Height(width int) int {
	if width <= 0 {
		width = choiceListMaxWidth
	}
	return len(s.lines(width, false))
}

// lines renders the full content; the input row is the third from the end
// (before the status and padding rows). The status row stays reserved,
// blank unless there is something to report, so the form never jumps.
func (s *SecureQuestion) lines(width int, focused bool) []string {
	t := s.Styles
	iconPrompt := questionIconPrompt(t, focused)
	var out []string
	header := iconPrompt + t.Editor.QuestionUnselected.Render(ansi.Wrap(s.Request.Text, max(1, width-lipgloss.Width(iconPrompt)), ""))
	out = append(out, strings.Split(header, "\n")...)
	out = append(out, "")
	if s.Request.Description != "" {
		out = append(out, strings.Split(questionDescription(t, s.Request.Description, width), "\n")...)
		out = append(out, "")
	}

	bar := "  "
	if focused {
		bar = t.Editor.QuestionCursorBar.Render("┃ ")
	}
	const prompt = "> "
	inner := max(1, width-lipgloss.Width(bar)-lipgloss.Width(prompt))
	var input, status string
	switch {
	case s.field == nil:
		input = t.Editor.QuestionBody.Render("Unavailable")
		status = t.Tool.WarnMessage.Render("Secure entry works only in the local Crush terminal.")
	case s.field.Saved():
		input = t.Editor.QuestionSelected.Render("Saved")
		status = t.Editor.QuestionNote.Render("Written to the file. The agent gets only this status.")
	case !s.field.Available():
		input = t.Editor.QuestionBody.Render("Not entered")
		status = t.Editor.QuestionNote.Render("This file was already saved. Request this entry again to add it.")
	case len(s.buffer.runes) == 0:
		input = t.Editor.QuestionBody.Render("Enter secret…")
	default:
		input = t.Editor.QuestionUnselected.Render(strings.Repeat("•", min(len(s.buffer.runes), inner-1)))
	}
	if s.errorText != "" {
		status = t.Tool.WarnMessage.Render(s.errorText)
	}
	rowWidth := max(1, width-2)
	out = append(out,
		bar+prompt+input,
		"  "+ansi.Truncate(status, rowWidth, "…"),
		"",
	)
	return out
}

// Draw renders the field, scrolled so the input rows stay visible.
func (s *SecureQuestion) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	lines := s.lines(area.Dx(), s.focused)
	viewport := area.Dy()
	inputRow := len(lines) - 3
	offset := 0
	if len(lines) > viewport {
		offset = min(max(0, inputRow+2-viewport), len(lines)-viewport)
	}
	for row := range viewport {
		idx := offset + row
		if idx >= len(lines) {
			break
		}
		y := area.Min.Y + row
		drawStyledText(scr, image.Rect(area.Min.X, y, area.Max.X, y+1), lines[idx])
	}
	if !s.focused || !s.usable() || inputRow < offset || inputRow-offset >= viewport {
		return nil
	}
	prefix := 4 // bar + "> "
	inner := max(1, area.Dx()-prefix)
	return tea.NewCursor(prefix+min(s.buffer.position, inner-1), inputRow-offset)
}

// HeightChanged is always false: the rows are reserved.
func (s *SecureQuestion) HeightChanged() bool { return false }

// SetFocused updates focus state.
func (s *SecureQuestion) SetFocused(focused bool) { s.focused = focused }

// SetHover is a no-op.
func (s *SecureQuestion) SetHover(x, y int) {}

// HandleMouseClick is a no-op.
func (s *SecureQuestion) HandleMouseClick(x, y int) (bool, bool) { return false, false }
