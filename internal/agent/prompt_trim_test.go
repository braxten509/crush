package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestTrimPreparedPromptShortensOldToolOutputsAndWritesSearchableTranscript(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{
		Sessions: env.sessions,
		Messages: env.messages,
	}).(*sessionAgent)

	model := Model{
		CatwalkCfg: catwalk.Model{
			ContextWindow:    40_000,
			DefaultMaxTokens: 4_000,
		},
	}

	var msgs []message.Message
	for i := range 8 {
		userText := fmt.Sprintf("Turn %d question with marker UNIQUE_TURN_%d", i+1, i+1)
		msgs = append(msgs, message.Message{
			ID:    fmt.Sprintf("u-%d", i+1),
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: userText}},
		})

		callID := fmt.Sprintf("call-%d", i+1)
		inputObj := map[string]string{
			"file_path": fmt.Sprintf("/repo/file_%d.cpp", i+1),
			"content":   fmt.Sprintf("INPUT_MARKER_%d\n", i+1) + strings.Repeat("x = y + 1;\n", 400),
		}
		inputBytes, err := json.Marshal(inputObj)
		require.NoError(t, err)

		msgs = append(msgs, message.Message{
			ID:       fmt.Sprintf("a-%d", i+1),
			Role:     message.Assistant,
			Provider: "claude-code",
			Model:    "opus",
			Parts: []message.ContentPart{
				message.TextContent{Text: fmt.Sprintf("Checking turn %d", i+1)},
				message.ToolCall{
					ID:       callID,
					Name:     "view",
					Input:    string(inputBytes),
					Finished: true,
				},
			},
		})

		toolOut := fmt.Sprintf("SECRET_NEEDLE_%d_START\n", i+1) +
			strings.Repeat(fmt.Sprintf("line of disassembly for turn %d: mov eax, ebx\n", i+1), 500) +
			fmt.Sprintf("SECRET_NEEDLE_%d_MIDDLE\n", i+1) +
			strings.Repeat(fmt.Sprintf("more disassembly for turn %d: ret\n", i+1), 500) +
			fmt.Sprintf("SECRET_NEEDLE_%d_END", i+1)

		msgs = append(msgs, message.Message{
			ID:   fmt.Sprintf("t-%d", i+1),
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{
					ToolCallID: callID,
					Name:       "view",
					Content:    toolOut,
				},
			},
		})
	}

	history, _ := sa.preparePrompt(msgs, true)
	rawTokens := estimateMessageTokens(history)
	budget := sa.historyTokenBudget(model, 1_000)
	require.Greater(t, rawTokens, budget, "raw history should exceed the model budget before trimming")

	trimmed := sa.trimPreparedPrompt("long-switch-session", msgs, history, model, 1_000)
	trimmedTokens := estimateMessageTokens(trimmed)
	require.LessOrEqual(t, trimmedTokens, budget, "trimmed history must fit within the model budget")

	// Verify the searchable transcript notice was inserted and points to an existing file on disk.
	var noticeText string
	for _, m := range trimmed {
		for _, part := range m.Content {
			if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && strings.Contains(tp.Text, "<conversation_history_notice>") {
				noticeText = tp.Text
				break
			}
		}
	}
	require.NotEmpty(t, noticeText, "expected a searchable transcript notice in trimmed history")
	require.Contains(t, noticeText, "grep")
	require.Contains(t, noticeText, "view")

	transcriptPath, idx, err := writeSearchableTranscript(sa.transcriptDataDir(), "long-switch-session", msgs)
	require.NoError(t, err)
	require.Contains(t, noticeText, transcriptPath)
	require.Greater(t, idx.totalLines, 100)

	fullTranscriptBytes, err := os.ReadFile(transcriptPath)
	require.NoError(t, err)
	fullTranscript := string(fullTranscriptBytes)
	// Every needle (including the middle that was shortened in the prompt) must be searchable in the transcript file.
	for i := range 8 {
		require.Contains(t, fullTranscript, fmt.Sprintf("SECRET_NEEDLE_%d_MIDDLE", i+1))
		require.Contains(t, fullTranscript, fmt.Sprintf("UNIQUE_TURN_%d", i+1))
	}

	// Verify every ToolCallPart still has valid JSON input and every Tool message follows an Assistant message.
	for i, m := range trimmed {
		if m.Role == fantasy.MessageRoleTool {
			require.Positive(t, i, "tool message cannot be the first message")
			require.Equal(t, fantasy.MessageRoleAssistant, trimmed[i-1].Role, "tool message must immediately follow its assistant message")
		}
		for _, part := range m.Content {
			if tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
				var parsed map[string]any
				require.NoError(t, json.Unmarshal([]byte(tc.Input), &parsed), "shortened tool call input must remain valid JSON")
			}
		}
	}
}

func TestDropOldestMessagesKeepsToolPairsIntact(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{
		Sessions: env.sessions,
		Messages: env.messages,
	}).(*sessionAgent)

	model := Model{
		CatwalkCfg: catwalk.Model{
			ContextWindow:    16_000,
			DefaultMaxTokens: 2_000,
		},
	}

	var msgs []message.Message
	for i := range 30 {
		msgs = append(msgs,
			message.Message{
				ID:    fmt.Sprintf("u-%d", i),
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("User turn %d: %s", i, strings.Repeat("word ", 200))}},
			},
			message.Message{
				ID:   fmt.Sprintf("a-%d", i),
				Role: message.Assistant,
				Parts: []message.ContentPart{
					message.TextContent{Text: fmt.Sprintf("Assistant turn %d: %s", i, strings.Repeat("reply ", 200))},
					message.ToolCall{ID: fmt.Sprintf("c-%d", i), Name: "bash", Input: `{"command":"ls"}`, Finished: true},
				},
			},
			message.Message{
				ID:   fmt.Sprintf("t-%d", i),
				Role: message.Tool,
				Parts: []message.ContentPart{
					message.ToolResult{ToolCallID: fmt.Sprintf("c-%d", i), Name: "bash", Content: "ok"},
				},
			},
		)
	}

	history, _ := sa.preparePrompt(msgs, false)
	budget := sa.historyTokenBudget(model, 500)
	trimmed := sa.trimPreparedPrompt("drop-oldest-session", msgs, history, model, 500)

	require.LessOrEqual(t, estimateMessageTokens(trimmed), budget)
	// Newest turn must still be present.
	lastAssistantFound := false
	for i, m := range trimmed {
		if m.Role == fantasy.MessageRoleTool {
			require.Positive(t, i)
			require.Equal(t, fantasy.MessageRoleAssistant, trimmed[i-1].Role)
		}
		for _, part := range m.Content {
			if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && strings.Contains(tp.Text, "Assistant turn 29:") {
				lastAssistantFound = true
			}
		}
	}
	require.True(t, lastAssistantFound, "newest turn must be preserved")
}

func TestCLIHandoffWithTranscriptIncludesSearchablePathWhenClipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	longOutput := "HEAD_MARKER\n" + strings.Repeat("disassembly line\n", 200) + "MIDDLE_SECRET_42\n" + strings.Repeat("footer line\n", 200)
	history := []message.Message{
		{
			ID:    "1",
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "inspect binary"}},
		},
		{
			ID:       "2",
			Role:     message.Assistant,
			Provider: "claude-code",
			Model:    "opus",
			Parts: []message.ContentPart{
				message.ToolCall{ID: "tc1", Name: "bash", Input: `{"command":"objdump"}`, Finished: true},
			},
		},
		{
			ID:   "3",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "tc1", Name: "bash", Content: longOutput},
			},
		},
	}

	prompt, resume := cliHandoffWithTranscript(history, cliagent.Link{}, "continue work", dir, "cli-switch")
	require.Empty(t, resume)
	require.Contains(t, prompt, "transcripts/cli-switch.md")

	saved, err := os.ReadFile(dir + "/transcripts/cli-switch.md")
	require.NoError(t, err)
	require.Contains(t, string(saved), "MIDDLE_SECRET_42")
}

func TestPreparePrompt_PreservesErrorFinish(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)

	errMsg := message.Message{
		ID:       "2",
		Role:     message.Assistant,
		Provider: "gemini",
		Model:    "gemini-2.5-flash",
	}
	errMsg.AddFinish(message.FinishReasonError, "Payment Required", "prepayment credits depleted")

	msgs := []message.Message{
		{ID: "1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "do something"}}},
		errMsg,
		{ID: "3", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "continue"}}},
	}

	history, _ := sa.preparePrompt(msgs, false)
	found := false
	for _, m := range history {
		if m.Role == fantasy.MessageRoleAssistant {
			for _, part := range m.Content {
				if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && strings.Contains(tp.Text, "[error: Payment Required]") {
					found = true
					break
				}
			}
		}
	}
	require.True(t, found, "error finish must be preserved in assistant message for native models")
}
