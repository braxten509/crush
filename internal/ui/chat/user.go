package chat

import (
	"encoding/xml"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// skillInvocation represents the XML structure for a loaded skill.
type skillInvocation struct {
	Name         string `xml:"name"`
	Description  string `xml:"description"`
	Location     string `xml:"location"`
	Instructions string `xml:"instructions"`
}

// UserMessageItem represents a user message in the chat UI.
type UserMessageItem struct {
	*list.Versioned
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	attachments *attachments.Renderer
	message     *message.Message
	sty         *styles.Styles
}

// NewUserMessageItem creates a new UserMessageItem.
func NewUserMessageItem(sty *styles.Styles, message *message.Message, attachments *attachments.Renderer) MessageItem {
	v := list.NewVersioned()
	return &UserMessageItem{
		Versioned:                v,
		highlightableMessageItem: defaultHighlighter(sty, v),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     newFocusableMessageItem(v),
		attachments:              attachments,
		message:                  message,
		sty:                      sty,
	}
}

// Finished implements list.Item. User messages are immutable once
// submitted, so the entry is always safe to freeze.
func (m *UserMessageItem) Finished() bool {
	return true
}

func (m *UserMessageItem) SubmissionID() string { return m.message.Content().SubmissionID }

// RawRender implements [MessageItem].
func (m *UserMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := m.getCachedRender(cappedWidth)
	// cache hit
	if ok {
		return m.renderHighlighted(content, cappedWidth, height)
	}

	msgContent := strings.TrimSpace(m.message.Content().Text)

	// A background task's result is for the agent; show a one-line notice.
	if name, status, ok := agent.ParseTaskNotification(msgContent); ok {
		content = m.renderTaskNotification(name, status, cappedWidth)
		height = lipgloss.Height(content)
		m.setCachedRender(content, cappedWidth, height)
		return m.renderHighlighted(content, cappedWidth, height)
	}

	// Check if this is a skill invocation (loaded_skill XML)
	if strings.HasPrefix(msgContent, "<loaded_skill>") {
		content = m.renderSkillInvocation(msgContent, cappedWidth)
		height = lipgloss.Height(content)
		m.setCachedRender(content, cappedWidth, height)
		return m.renderHighlighted(content, cappedWidth, height)
	}

	content = m.renderPlain(msgContent, cappedWidth)

	if len(m.message.BinaryContent()) > 0 {
		attachmentsStr := m.renderAttachments(cappedWidth)
		if content == "" {
			content = attachmentsStr
		} else {
			content = strings.Join([]string{content, "", attachmentsStr}, "\n")
		}
	}

	height = lipgloss.Height(content)
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

// renderSkillInvocation renders a loaded_skill XML as a special UI element.
func (m *UserMessageItem) renderSkillInvocation(content string, width int) string {
	var skill skillInvocation
	if err := xml.Unmarshal([]byte(content), &skill); err != nil {
		return m.renderPlain(content, width)
	}

	return toolOutputSkillContent(m.sty, skill.Name, skill.Description)
}

// renderPlain shows text exactly as the user sent it: no Markdown, so
// nothing like <tags>, *stars* or `backticks` is hidden or restyled. Only
// escape sequences are dropped (they'd drive the terminal) and tabs become
// spaces so wrapping measures them.
func (m *UserMessageItem) renderPlain(text string, width int) string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(ansi.Strip(text))
	style := lipgloss.NewStyle()
	if c := m.sty.Markdown.Document.Color; c != nil {
		style = style.Foreground(lipgloss.Color(*c))
	}
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

func (m *UserMessageItem) renderTaskNotification(name, status string, width int) string {
	icon := m.sty.Tool.IconSuccess.Render()
	if name == agent.AskName && (status == agent.AskAnswered || status == agent.AskCancelled) {
		if status == agent.AskCancelled {
			icon = m.sty.Tool.IconCancelled.Render()
		}
		line := icon + " " + m.sty.Pills.HelpText.Render("Your answers to the questions: ") + m.sty.Pills.TodoLabel.Render(status)
		return ansi.Truncate(line, width, "…")
	}
	verb := "finished"
	switch agent.TaskStatus(status) {
	case agent.TaskFailed:
		icon, verb = m.sty.Tool.IconError.Render(), "failed"
	case agent.TaskStopped:
		icon, verb = m.sty.Tool.IconCancelled.Render(), "stopped"
	}
	line := icon + " " + m.sty.Pills.HelpText.Render("Task ") + m.sty.Pills.TodoLabel.Render(name) + m.sty.Pills.HelpText.Render(" "+verb)
	return ansi.Truncate(line, width, "…")
}

// Render implements MessageItem.
func (m *UserMessageItem) Render(width int) string {
	// Bypass the prefix cache while a highlight range is active so
	// selection drags reflect immediately without invalidating the
	// cache. Highlight changes are intentionally applied "above" the
	// prefix cache.
	useCache := !m.isHighlighted()
	var key uint64
	if m.focused {
		key = 1
	}
	if useCache {
		if cached, ok := m.getCachedPrefixedRender(width, key); ok {
			return cached
		}
	}
	lines := strings.Split(m.RawRender(width), "\n")
	if m.isNotice() {
		// A task's result is a notice for the agent, not something the
		// user typed: it lines up with replies, without the band.
		prefix := m.sty.Messages.AssistantBlurred.Render()
		for i, line := range lines {
			lines[i] = prefix + line
		}
	} else {
		marker := m.sty.Messages.UserBlurred
		if m.focused {
			marker = m.sty.Messages.UserFocused
		}
		for i, line := range lines {
			lead := "  "
			if i == 0 {
				lead = "› "
			}
			lines[i] = common.OnBand(marker.Render(lead)+line, width, m.sty.Messages.UserBackground)
		}
		// A row of padding above and below, like the composer's band.
		pad := common.OnBand("", width, m.sty.Messages.UserBackground)
		lines = append(append([]string{pad}, lines...), pad)
	}
	out := strings.Join(lines, "\n")
	if useCache {
		m.setCachedPrefixedRender(out, width, key)
	}
	return out
}

// RawTop is how many rendered rows sit above the raw content: the band's
// top padding row.
func (m *UserMessageItem) RawTop() int {
	if m.isNotice() {
		return 0
	}
	return 1
}

// SetHighlight implements list.Highlightable. Rows count from the band's
// top padding row, which the highlighted content does not have.
func (m *UserMessageItem) SetHighlight(startLine, startCol, endLine, endCol int) {
	if top := m.RawTop(); top > 0 {
		if startLine > 0 {
			startLine -= top
		}
		if endLine > 0 {
			endLine -= top
		}
	}
	m.highlightableMessageItem.SetHighlight(startLine, startCol, endLine, endCol)
}

// isNotice reports whether the message is a background task's result.
func (m *UserMessageItem) isNotice() bool {
	_, _, ok := agent.ParseTaskNotification(strings.TrimSpace(m.message.Content().Text))
	return ok
}

// ID implements MessageItem.
func (m *UserMessageItem) ID() string {
	return m.message.ID
}

// renderAttachments renders attachments.
func (m *UserMessageItem) renderAttachments(width int) string {
	var attachments []message.Attachment
	for _, at := range m.message.BinaryContent() {
		attachments = append(attachments, message.Attachment{
			FileName: at.Path,
			MimeType: at.MIMEType,
		})
	}
	// This message is already posted, so the attachment can't be removed;
	// don't render the remove button.
	return m.attachments.Render(attachments, false, false, width)
}

// HandleKeyEvent implements KeyEventHandler.
func (m *UserMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := m.message.Content().Text
		return true, common.CopyToClipboard(text, "Message copied to clipboard")
	}
	return false, nil
}
