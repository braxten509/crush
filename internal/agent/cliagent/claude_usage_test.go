package cliagent

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestClaudeUsageMergesStreamingSnapshots(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":25,"output_tokens":1,"cache_creation_input_tokens":10,"cache_read_input_tokens":100}}}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":5}}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":15}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"assistant","message":{"usage":{"input_tokens":25,"output_tokens":15,"cache_creation_input_tokens":10,"cache_read_input_tokens":100},"content":[]}}
{"type":"result","subtype":"success","usage":{"input_tokens":25,"output_tokens":15,"cache_creation_input_tokens":10,"cache_read_input_tokens":100}}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{EventSession, EventUsage}, types(events))
	require.Equal(t, fantasy.Usage{InputTokens: 25, OutputTokens: 15, CacheCreationTokens: 10, CacheReadTokens: 100, TotalTokens: 150}, events[1].Usage)
}

func TestClaudeUsageFlushesAtResultWithoutMessageStop(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":25,"output_tokens":1,"cache_read_input_tokens":100}}}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":15}}}
{"type":"result","subtype":"success","usage":{"input_tokens":25,"output_tokens":15,"cache_read_input_tokens":100}}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{EventSession, EventUsage}, types(events))
	require.Equal(t, fantasy.Usage{InputTokens: 25, OutputTokens: 15, CacheReadTokens: 100, TotalTokens: 140}, events[1].Usage)
}

func TestClaudeUsageCountsEachRequestOnce(t *testing.T) {
	for _, stream := range []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":20}}}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":5}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":20},"content":[]}}
{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":30,"cache_read_input_tokens":40}}}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"output_tokens":7}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"assistant","message":{"usage":{"input_tokens":30,"output_tokens":7,"cache_read_input_tokens":40},"content":[]}}
`,
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":20},"content":[]}}
{"type":"assistant","message":{"usage":{"input_tokens":30,"output_tokens":7,"cache_read_input_tokens":40},"content":[]}}
`,
	} {
		fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
`+stream+`{"type":"result","subtype":"success","usage":{"input_tokens":40,"output_tokens":12,"cache_read_input_tokens":60}}
`)
		events, err := collect(t, "claude", "")
		require.NoError(t, err)
		require.Equal(t, []EventType{EventSession, EventUsage, EventUsage}, types(events))
		require.Equal(t, fantasy.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 20, TotalTokens: 35}, events[1].Usage)
		require.Equal(t, fantasy.Usage{InputTokens: 30, OutputTokens: 7, CacheReadTokens: 40, TotalTokens: 77}, events[2].Usage)
	}
}

func TestClaudeUsageFallsBackToResult(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"result","subtype":"success","usage":{"input_tokens":25,"output_tokens":15,"cache_read_input_tokens":100}}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{EventSession, EventUsage}, types(events))
	require.Equal(t, fantasy.Usage{InputTokens: 25, OutputTokens: 15, CacheReadTokens: 100, TotalTokens: 140}, events[1].Usage)
}
