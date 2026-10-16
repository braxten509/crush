package chat

import (
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// thoughtLineSetting reports whether busy lines show the newest line of
// the model's thinking; unset means they do.
var thoughtLineSetting atomic.Pointer[func() bool]

// SetThoughtLineSetting points busy lines at the user's thought line
// setting, read on every render so a change shows at once.
func SetThoughtLineSetting(show func() bool) {
	thoughtLineSetting.Store(&show)
}

func showThoughtLine() bool {
	if show := thoughtLineSetting.Load(); show != nil {
		return (*show)()
	}
	return true
}

// thoughtLine is the newest line of the thinking msg is doing now, or "".
// A CLI that holds its thinking back until the thought ends sends the line
// itself; otherwise it comes from the thinking streamed so far.
func thoughtLine(msg *message.Message) string {
	if msg == nil || msg.IsFinished() || !showThoughtLine() {
		return ""
	}
	if msg.ActivityDetail != "" {
		return cleanThoughtLine(msg.ActivityDetail)
	}
	if msg.IsThinking() {
		return cleanThoughtLine(msg.ReasoningContent().Thinking)
	}
	return ""
}

// cleanThoughtLine keeps the last non-blank line of text, without the
// markdown marks models put around their thinking headings.
func cleanThoughtLine(text string) string {
	text = strings.TrimRight(ansi.Strip(text), " \t\r\n")
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return strings.Trim(text, " \t*_#`")
}

// renderThoughtLine renders line to follow a busy status, or "" for none.
func renderThoughtLine(sty *styles.Styles, line string) string {
	if line == "" {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(sty.WorkingTimerColor)
	return dim.Render(" · ") + dim.Italic(true).Render(line)
}
