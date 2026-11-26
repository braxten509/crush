package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

const (
	handoffTokens           = 30_000
	handoffResumeIntro      = "Since your last reply, the user continued this conversation with other AI agents. Here is what happened meanwhile:"
	handoffWrittenCopyIntro = "This conversation started before you joined. Here is its recent history:"
)

// handoffBytes uses the same four-byte estimate as approxTokenCount. Keep at
// least half of a small model's window for its instructions and ongoing work.
func handoffBytes(window, available int64) int {
	tokens := int64(handoffTokens)
	if window > 0 {
		tokens = min(tokens, window/2)
	}
	return int(max(0, min(tokens, available))) * 4
}

type providerHandoff struct {
	history            []message.Message
	target             config.SelectedModel
	dataDir, sessionID string
	budget             int // bytes available for the inline excerpt
}

func (h providerHandoff) forCLI(link cliagent.Link, prompt string) (string, string, error) {
	if link.Native != "" {
		for i, m := range h.history {
			if m.ID == link.Through {
				if i == len(h.history)-1 {
					return prompt, link.Native, nil
				}
				text, err := h.prompt(handoffResumeIntro, h.history[i+1:], prompt)
				return text, link.Native, err
			}
		}
	}
	if len(h.history) == 0 {
		return prompt, "", nil
	}
	text, err := h.prompt(handoffWrittenCopyIntro, h.history, prompt)
	return text, "", err
}

func (h providerHandoff) prompt(intro string, recent []message.Message, prompt string) (string, error) {
	path, _, err := writeSearchableTranscript(h.dataDir, h.sessionID, h.history)
	if err != nil {
		return "", fmt.Errorf("save conversation for the next agent: %w", err)
	}
	body, _ := renderTranscript("", recent)
	heading := "<conversation_handoff>\n" + intro + "\n\n"
	lookup := fmt.Sprintf("\nSearch %q or read it by line for the full conversation, including tool calls and results. Continue the user's work from here.\n</conversation_handoff>\n\n", path)
	return heading + recentExcerpt(body, recent, max(0, h.budget-len(heading)-len(lookup))) + lookup + prompt, nil
}

func recentExcerpt(body string, recent []message.Message, budget int) string {
	if budget <= 2 {
		return tailBytes(body, max(0, budget))
	}
	if len(body) <= budget {
		return body
	}
	// A single large tool result can cover the entire excerpt. Keep the
	// latest user request alongside its tail in that case.
	var request string
	for i := len(recent) - 1; i >= 0; i-- {
		m := recent[i]
		text := m.Content()
		if !m.IsPrompt() || text.Hidden || text.Text == "" {
			continue
		}
		if _, _, generated := ParseTaskNotification(text.Text); generated {
			continue
		}
		if !strings.Contains(body[len(body)-budget:], text.Text) {
			request = tailBytes("Latest user request:\n"+text.Text, min(budget/4, 8000)) + "\n\n"
		}
		break
	}
	return request + tailBytes(body, max(0, budget-len(request)))
}

func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	tail := s[len(s)-n:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return tail
}

// transcriptPrompt includes text files, shell commands and subagent reports
// as the agent saw them. Binary attachments are named instead of base64 encoded.
func transcriptPrompt(m message.Message) string {
	var b strings.Builder
	b.WriteString(m.Content().Text)
	for _, p := range m.Parts {
		switch v := p.(type) {
		case message.BinaryContent:
			fmt.Fprintf(&b, "\n[attachment: %s, %s]", v.Path, v.MIMEType)
			if strings.HasPrefix(v.MIMEType, "text/") {
				b.WriteByte('\n')
				b.Write(v.Data)
			}
		case message.SubAgentReport:
			b.WriteString("\n" + v.PromptText())
		case message.ShellCommand:
			fmt.Fprintf(&b, "\n$ %s\n%s\n(exit code %d)", v.Command, v.Output, v.ExitCode)
		}
	}
	return b.String()
}

// apiHistory keeps the receiving model's own subsequent turns in native
// format. Earlier providers' signed reasoning and tool protocols are not
// portable, so their conversation is replayed as one bounded transcript.
func (h providerHandoff) apiHistory(summaryID string) ([]message.Message, error) {
	foreign, firstLocal, summary := -1, -1, -1
	for i, m := range h.history {
		if m.ID == summaryID && summaryID != "" {
			summary = i
		}
		if m.Role != message.Assistant || m.IsSummaryMessage || m.Provider == "" {
			continue
		}
		if m.Provider != h.target.Provider || m.Model != h.target.Model {
			foreign, firstLocal = i, -1
		} else if firstLocal < 0 {
			firstLocal = i
		}
	}
	if foreign < 0 || firstLocal >= 0 && summary > firstLocal {
		return sinceSummary(h.history, summaryID), nil
	}
	cut := len(h.history)
	if firstLocal >= 0 {
		cut = firstLocal
		for cut > foreign+1 && h.history[cut-1].Role == message.User {
			cut--
		}
	}
	text, err := h.prompt(handoffWrittenCopyIntro, h.history[:cut], "")
	if err != nil {
		return nil, err
	}
	prefix := message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
	return append([]message.Message{prefix}, h.history[cut:]...), nil
}
