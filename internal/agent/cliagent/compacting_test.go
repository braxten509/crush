package cliagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCompactionStatus(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"system","subtype":"status","status":"compacting"}
{"type":"system","subtype":"status","status":null}
{"type":"system","subtype":"compact_boundary"}
{"type":"result","result":"done"}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{EventSession, EventCompacting, EventCompacting, EventCompacting}, types(events))
	require.True(t, events[1].Compacting)
	require.False(t, events[2].Compacting)
	require.False(t, events[3].Compacting)
}

func TestCodexCompactionStatus(t *testing.T) {
	fakeCLI(t, "codex", `{"id":"1","result":{}}
{"id":"2","result":{"thread":{"id":"th1"}}}
{"id":"3","result":{"turn":{"id":"tu1"}}}
{"method":"item/started","params":{"item":{"type":"contextCompaction","id":"compact1"}}}
{"method":"item/started","params":{"item":{"type":"reasoning","id":"hidden-reasoning"}}}
{"method":"item/reasoning/summaryTextDelta","params":{"delta":"private compaction notes"}}
{"method":"item/started","params":{"item":{"type":"agentMessage","id":"hidden-summary"}}}
{"method":"item/agentMessage/delta","params":{"delta":"private compaction summary"}}
{"method":"item/completed","params":{"item":{"type":"contextCompaction","id":"compact1"}}}
{"method":"thread/compacted","params":{"threadId":"th1","turnId":"tu1"}}
{"method":"item/agentMessage/delta","params":{"delta":"Back to work"}}
{"method":"turn/completed","params":{"turn":{"id":"tu1","status":"completed"}}}
`)
	events, err := collect(t, "codex", "")
	require.NoError(t, err)
	events = withoutActivity(events)
	require.Equal(t, []EventType{EventSession, EventCompacting, EventCompacting, EventCompacting, EventText}, types(events))
	require.True(t, events[1].Compacting)
	require.False(t, events[2].Compacting)
	require.False(t, events[3].Compacting)
	require.Equal(t, "Back to work", events[4].Text)
}
