package cliagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeHeldThinkingSendsStatus(t *testing.T) {
	// Claude's thinking is held until its block ends, so the status gets
	// "thinking" as the block starts and the newest line as it grows.
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"**Planning**\n\nCheck how the sandbox asks for su."}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" Then"}}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"**Planning**\n\nCheck how the sandbox asks for su. Then"}]}}
{"type":"result","subtype":"success"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	var status []Event
	for _, e := range events {
		if e.Type == EventActivity {
			status = append(status, e)
		}
	}
	require.Equal(t, []Event{
		{Type: EventActivity, Activity: ActivityThinking},
		{Type: EventActivity, Activity: ActivityThinking, Text: "Check how the sandbox asks for su."},
		{Type: EventActivity, Activity: ActivityThinking, Text: "Check how the sandbox asks for su. Then"},
	}, status, "the block start says thinking at once, then each delta brings the newest line")
	require.Equal(t, []Event{
		{Type: EventSession, Session: "s1"},
		{Type: EventReasoning, Text: "**Planning**\n\nCheck how the sandbox asks for su. Then"},
	}, withoutActivity(events))
}

func TestClaudeThinkingLineIsTheNewestLine(t *testing.T) {
	r := &claudeRun{}
	var got []Event
	r.t = Turn{Emit: func(e Event) error { got = append(got, e); return nil }}
	r.recordUsage = func(claudeLine) error { return nil }
	delta := func(text string) claudeLine {
		ev := &claudeEvent{Type: "content_block_delta"}
		ev.Delta.Type, ev.Delta.Thinking = "thinking_delta", text
		return claudeLine{Event: ev}
	}
	require.NoError(t, r.handleStreamEvent(delta("First idea.\nSecond idea.")))
	require.NoError(t, r.handleStreamEvent(delta("")))
	require.NoError(t, r.handleStreamEvent(delta("\n\nThird idea.\n")))
	require.Equal(t, []Event{
		{Type: EventActivity, Activity: ActivityThinking, Text: "Second idea."},
		{Type: EventActivity, Activity: ActivityThinking, Text: "Third idea."},
	}, got, "empty deltas, as omitted thinking sends, change nothing")
	require.Equal(t, "First idea.\nSecond idea.\n\nThird idea.\n", r.thinking.String(), "the thinking itself is still held whole")
}
