package dialog

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/clipboard"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// SecureEntry owns the only input buffer; it is never a question/chat answer.
type SecureEntry struct {
	com       *common.Common
	request   *secureentry.Request
	value     []rune
	position  int
	saving    bool
	errorText string
}

func (*SecureEntry) String() string { return "secure entry (redacted)" }

// SecureEntrySaved contains only a fixed error, never the value.
type SecureEntrySaved struct {
	Request *secureentry.Request
	Err     error
}

// SecureEntryPaste is tagged to prevent a late clipboard result entering chat.
type SecureEntryPaste struct {
	Request *secureentry.Request
	Value   []byte
}

func (SecureEntryPaste) String() string { return "secure paste (redacted)" }

func NewSecureEntry(com *common.Common, request *secureentry.Request) *SecureEntry {
	return &SecureEntry{com: com, request: request}
}

// Handle consumes every keyboard/paste event while secure entry owns focus.
func (s *SecureEntry) Handle(msg tea.Msg) (bool, tea.Cmd) {
	if pasted, ok := msg.(SecureEntryPaste); ok {
		defer clear(pasted.Value)
		if pasted.Request == s.request && !s.saving {
			s.insert(string(pasted.Value))
		}
		return false, nil
	}
	if s.saving {
		return false, nil
	}
	switch msg := msg.(type) {
	case tea.PasteMsg:
		s.insert(msg.Content)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			clear(s.value)
			s.value = nil
			request := s.request
			return true, func() tea.Msg { request.Finish(false); return nil }
		case "enter":
			if len(s.value) == 0 {
				s.errorText = "Enter a value before saving."
				return false, nil
			}
			value := []byte(string(s.value))
			clear(s.value)
			s.value = nil
			s.position = 0
			s.saving = true
			request := s.request
			return false, func() tea.Msg {
				defer clear(value)
				err := request.Save(value)
				return SecureEntrySaved{Request: request, Err: err}
			}
		case "ctrl+v", "shift+insert":
			request := s.request
			return false, func() tea.Msg {
				text, _ := clipboard.Read(clipboard.FormatText)
				return SecureEntryPaste{Request: request, Value: text}
			}
		case "backspace", "ctrl+h":
			if s.position > 0 {
				copy(s.value[s.position-1:], s.value[s.position:])
				s.value[len(s.value)-1] = 0
				s.value = s.value[:len(s.value)-1]
				s.position--
			}
		case "delete":
			if s.position < len(s.value) {
				copy(s.value[s.position:], s.value[s.position+1:])
				s.value[len(s.value)-1] = 0
				s.value = s.value[:len(s.value)-1]
			}
		case "ctrl+u":
			clear(s.value)
			s.value = nil
			s.position = 0
		case "left":
			s.position = max(0, s.position-1)
		case "right":
			s.position = min(len(s.value), s.position+1)
		case "home", "ctrl+a":
			s.position = 0
		case "end", "ctrl+e":
			s.position = len(s.value)
		default:
			if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
				s.insert(msg.Text)
			}
		}
	}
	return false, nil
}

func (s *SecureEntry) insert(text string) {
	if !utf8.ValidString(text) || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		s.errorText = "Use a single-line value without control characters."
		return
	}
	if len(string(s.value))+len(text) > 64<<10 {
		s.errorText = "Value exceeds 64 KiB."
		return
	}
	runes := []rune(text)
	defer clear(runes)
	s.value = append(s.value, make([]rune, len(runes))...)
	copy(s.value[s.position+len(runes):], s.value[s.position:len(s.value)-len(runes)])
	copy(s.value[s.position:], runes)
	s.position += len(runes)
	s.errorText = ""
}

func (s *SecureEntry) Saved(result SecureEntrySaved) bool {
	if result.Request != s.request {
		return false
	}
	if result.Err == nil {
		return true
	}
	s.saving = false
	s.errorText = result.Err.Error()
	return false
}

func (s *SecureEntry) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles
	width := max(0, min(70, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	line := func(text string) string {
		return t.Dialog.SecondaryText.Width(inner).Render(ansi.Truncate(text, inner, "…"))
	}
	// Only mask characters are rendered, even for pasted ANSI/control sequences.
	mask := strings.Repeat("•", min(len(s.value), max(0, inner-4)))
	if len(s.value) == 0 {
		mask = "Enter secret…"
	}
	if s.saving {
		mask = "Saving…"
	}
	// Only the field and its keys: the request's label is the title, and
	// the file, placeholder and privacy notes were noise (user request).
	body := []string{
		"",
		t.Dialog.InputPrompt.Width(inner).Render("> " + mask),
		"",
	}
	if s.errorText != "" {
		body = append(body, line(s.errorText))
	}
	body = append(body, line("enter save  ·  esc cancel  ·  ctrl+u clear"))
	rc := NewRenderContext(t, width)
	rc.Title = "Secure entry"
	if label := strings.TrimSpace(s.request.Label); label != "" {
		rc.Title = label
	}
	rc.AddPart(lipgloss.JoinVertical(lipgloss.Left, body...))
	view := rc.Render()
	// Clip the result to tiny terminals without allowing wrapped frame overflow.
	lines := strings.Split(view, "\n")
	if len(lines) > area.Dy() {
		lines = lines[:max(0, area.Dy())]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], area.Dx(), "")
	}
	DrawCenter(scr, area, strings.Join(lines, "\n"))
	return nil
}
