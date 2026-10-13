package agent

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
)

// Handoff budgets, in bytes. The newest messages are kept whole first.
const (
	handoffBudget     = 120_000
	handoffUserMax    = 12_000
	handoffTextMax    = 6_000
	handoffToolArgMax = 300
	handoffToolOutMax = 800
)

// cliHandoff builds the prompt for a CLI turn and picks the native session
// to resume. A native session that has seen the conversation up to some
// point is resumed with only what happened since (turns by other agents);
// otherwise a fresh session gets the whole conversation.
func cliHandoff(history []message.Message, link cliagent.Link, prompt string) (string, string) {
	if link.Native != "" {
		for i, msg := range history {
			if msg.ID != link.Through {
				continue
			}
			missed := history[i+1:]
			if len(missed) == 0 {
				return prompt, link.Native
			}
			return handoffPrompt("Since your last reply, the user continued this conversation with other AI agents. Here is what happened meanwhile:", missed, prompt), link.Native
		}
	}
	if len(history) == 0 {
		return prompt, ""
	}
	return handoffPrompt("This conversation started before you joined; earlier turns were handled by other AI agents. Here it is so far:", history, prompt), ""
}

func handoffPrompt(intro string, msgs []message.Message, prompt string) string {
	return fmt.Sprintf(`<conversation_handoff>
You are running inside Crush, a terminal app where the user can switch between AI agents mid-conversation. %s

%s
Treat this as your own context: build on finished work instead of redoing it, and don't mention the handoff unless asked.
</conversation_handoff>

%s`, intro, transcript(msgs), prompt)
}

// transcript renders messages for a handoff, newest first within the
// budget, then restores reading order.
func transcript(msgs []message.Message) string {
	var blocks []string
	size, omitted := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		block := renderMessage(msgs[i])
		if block == "" {
			continue
		}
		if size+len(block) > handoffBudget {
			omitted = i + 1
			break
		}
		size += len(block)
		blocks = append(blocks, block)
	}
	var b strings.Builder
	if omitted > 0 {
		fmt.Fprintf(&b, "[%d earlier messages omitted]\n\n", omitted)
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		b.WriteString(blocks[i])
		b.WriteString("\n")
	}
	return b.String()
}

func renderMessage(msg message.Message) string {
	var b strings.Builder
	switch msg.Role {
	case message.User:
		if text := msg.Content().Text; text != "" {
			fmt.Fprintf(&b, "[user]\n%s\n", clip(text, handoffUserMax))
		}
	case message.Assistant:
		who := "assistant"
		if msg.Model != "" {
			who = fmt.Sprintf("assistant: %s/%s", msg.Provider, msg.Model)
		}
		if msg.IsSummaryMessage {
			who = "summary of the earlier conversation"
		}
		text := msg.Content().Text
		calls := msg.ToolCalls()
		if text == "" && len(calls) == 0 {
			return ""
		}
		fmt.Fprintf(&b, "[%s]\n", who)
		if text != "" {
			b.WriteString(clip(text, handoffTextMax) + "\n")
		}
		for _, tc := range calls {
			fmt.Fprintf(&b, "-> %s %s\n", tc.Name, clip(tc.Input, handoffToolArgMax))
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			status := "ok"
			if tr.IsError {
				status = "error"
			}
			fmt.Fprintf(&b, "<- %s %s: %s\n", tr.Name, status, clip(strings.TrimSpace(tr.Content), handoffToolOutMax))
		}
	}
	return b.String()
}

// clip shortens s to about max bytes, keeping its start and end.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := max / 2
	return strings.ToValidUTF8(s[:half], "") + "\n[... middle omitted ...]\n" + strings.ToValidUTF8(s[len(s)-half:], "")
}
