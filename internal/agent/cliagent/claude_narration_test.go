package cliagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeNarrationIsText(t *testing.T) {
	// Claude sends narration as thinking deltas, then identifies it on the
	// completed assistant frame. Indexes refer to that frame, not the stream.
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check the build."}}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Check the build."}]}}
{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"I'm about "}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"40% through."}}}
{"type":"assistant","narration_block_indexes":[0],"message":{"content":[{"type":"thinking","thinking":"I'm about 40% through."}]}}
{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"t1","name":"Bash"}}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Check the result."}}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Check the result."}]}}
{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Tests passed."}}}
{"type":"result","subtype":"success"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{
		EventSession, EventReasoning, EventText, EventText,
		EventToolStart, EventToolCall, EventReasoning, EventText, EventText,
	}, types(events))
	require.Equal(t, "Check the build.", events[1].Text)
	require.Equal(t, TextBreak, events[2].Text)
	require.Equal(t, "I'm about 40% through.", events[3].Text)
	require.Equal(t, "Check the result.", events[6].Text)
	require.Equal(t, "Tests passed.", events[8].Text)
}

func TestClaudeNarrationFrameIndexes(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"assistant","narration_block_indexes":[1,2],"message":{"content":[{"type":"thinking","thinking":"Reasoning."},{"type":"thinking","thinking":"First update."},{"type":"thinking","thinking":"Second update."},{"type":"thinking","thinking":""}]}}
{"type":"result","subtype":"success"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []Event{
		{Type: EventReasoning, Text: "Reasoning."},
		{Type: EventText, Text: TextBreak},
		{Type: EventText, Text: "First update."},
		{Type: EventText, Text: TextBreak},
		{Type: EventText, Text: "Second update."},
	}, events)
}

func TestClaudeUnfinishedThinkingIsPreserved(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Partial reasoning."}}}
{"type":"result","subtype":"success"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []Event{{Type: EventReasoning, Text: "Partial reasoning."}}, events)
}
