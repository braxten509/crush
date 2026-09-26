package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestCLIHandoff(t *testing.T) {
	msg := func(id string, role message.MessageRole, text string) message.Message {
		return message.Message{ID: id, Role: role, Provider: "codex-cli", Model: "gpt", Parts: []message.ContentPart{message.TextContent{Text: text}}}
	}
	history := []message.Message{
		msg("1", message.User, "remember PINEAPPLE"),
		msg("2", message.Assistant, "ok"),
		msg("3", message.User, "what now"),
		msg("4", message.Assistant, "codex answer"),
	}

	// New session: everything is handed over, nothing resumed.
	prompt, resume := cliHandoff(history, cliagent.Link{}, "next")
	require.Empty(t, resume)
	require.Contains(t, prompt, "PINEAPPLE")
	require.Contains(t, prompt, "[assistant: codex-cli/gpt]\ncodex answer")
	require.True(t, len(prompt) > len("next") && prompt[len(prompt)-4:] == "next")

	// Resumed session that saw up to message 2: only 3 and 4 are new.
	prompt, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "2"}, "next")
	require.Equal(t, "n1", resume)
	require.NotContains(t, prompt, "PINEAPPLE")
	require.Contains(t, prompt, "codex answer")

	// Resumed session that is up to date: just the prompt.
	prompt, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "4"}, "next")
	require.Equal(t, "n1", resume)
	require.Equal(t, "next", prompt)

	// Its last-seen message was summarized away: start over with everything.
	_, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "gone"}, "next")
	require.Empty(t, resume)
}
