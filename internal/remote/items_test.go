package remote

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func userMsg(id, text string) message.Message {
	return message.Message{ID: id, Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}, CreatedAt: 1}
}

func assistantMsg(id string, parts ...message.ContentPart) message.Message {
	return message.Message{ID: id, Role: message.Assistant, Parts: parts, CreatedAt: 2}
}

func toolMsg(id string, results ...message.ToolResult) message.Message {
	parts := make([]message.ContentPart, len(results))
	for i, r := range results {
		parts[i] = r
	}
	return message.Message{ID: id, Role: message.Tool, Parts: parts, CreatedAt: 3, UpdatedAt: 3}
}

func kinds(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind
	}
	return out
}

func TestBuildItemsFoldsToolCallsLikeTheTUI(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		userMsg("u1", "fix the bug"),
		assistantMsg("a1",
			message.ReasoningContent{Thinking: "Look around first.", FinishedAt: 2},
			message.ToolCall{ID: "tc1", Name: "view", Input: `{"file_path":"/work/main.go"}`, Finished: true},
			message.ToolCall{ID: "tc2", Name: "edit", Input: `{"file_path":"/work/main.go","old_string":"a","new_string":"b"}`, Finished: true},
			message.Finish{Reason: message.FinishReasonToolUse},
		),
		toolMsg("t1",
			message.ToolResult{ToolCallID: "tc1", Name: "view", Content: "package main"},
			message.ToolResult{ToolCallID: "tc2", Name: "edit", Content: "ok", Metadata: `{"additions":2,"removals":1}`},
		),
		assistantMsg("a2", message.TextContent{Text: "Fixed it."}, message.Finish{Reason: message.FinishReasonEndTurn}),
	}

	items := buildItems(msgs, chatState{workingDir: "/work"})
	require.Equal(t, []string{"user", "group", "reply"}, kinds(items))

	group := items[1]
	require.Equal(t, "group:tc1", group.ID)
	require.False(t, group.Live)
	require.Equal(t, "2 actions · edited main.go", group.Summary)
	require.Len(t, group.Steps, 2)
	require.Equal(t, "main.go", group.Steps[0].Target)
	require.Equal(t, "done", group.Steps[0].State)
	require.Equal(t, 2, group.Steps[1].Added)
	require.Equal(t, 1, group.Steps[1].Removed)
	require.Equal(t, "Fixed it.", items[2].Text)
	require.False(t, items[2].Streaming)
}

func TestBuildItemsStepStates(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		userMsg("u1", "run it"),
		assistantMsg("a1", message.ToolCall{ID: "tc1", Name: "bash", Input: `{"command":"go test ./...\necho done"}`, Finished: true}),
	}

	running := buildItems(msgs, chatState{busy: true})
	require.Equal(t, []string{"user", "group"}, kinds(running))
	require.True(t, running[1].Live)
	require.NotEmpty(t, running[1].Activity)
	require.Equal(t, "running", running[1].Steps[0].State)
	require.Equal(t, "go test ./... …", running[1].Steps[0].Target)

	waiting := buildItems(msgs, chatState{busy: true, waitingCall: "tc1"})
	require.Equal(t, "waiting", waiting[1].Steps[0].State)

	stopped := buildItems(msgs, chatState{})
	require.False(t, stopped[1].Live)
	require.Equal(t, "stopped", stopped[1].Steps[0].State)
}

func TestBuildItemsNoticesAndHiddenMessages(t *testing.T) {
	t.Parallel()

	hidden := userMsg("u0", "continue")
	hidden.Parts = []message.ContentPart{message.TextContent{Text: "continue", Hidden: true}}
	msgs := []message.Message{
		hidden,
		userMsg("u1", "<crush-task-result><name>t2</name><status>failed</status></crush-task-result>"),
		assistantMsg("a1", message.TextContent{Text: "Partial"}, message.Finish{Reason: message.FinishReasonCanceled}),
		assistantMsg("a2", message.Finish{Reason: message.FinishReasonError, Message: "Rate limited"}),
	}

	items := buildItems(msgs, chatState{})
	require.Equal(t, []string{"notice", "reply", "notice", "error"}, kinds(items))
	require.Equal(t, "Task t2 failed", items[0].Text)
	require.Equal(t, "stop", items[0].Tone)
	require.Equal(t, "Stopped", items[2].Text)
	require.Equal(t, "Rate limited", items[3].Text)
}

func TestBuildItemsQuestionsAndSubAgents(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		assistantMsg("a1",
			message.ToolCall{ID: "q1", Name: "question", Input: `{"questions":[{"type":"yes_no","question":"Ship it?"}]}`, Finished: true},
			message.ToolCall{ID: "b1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
			message.ToolCall{ID: "s1", Name: "agent", Input: `{"prompt":"look into it"}`, Finished: true},
			message.ToolCall{ID: "b2", Name: "bash", Input: `{"command":"pwd"}`, Finished: true},
		),
		toolMsg("t1",
			message.ToolResult{ToolCallID: "q1", Content: "Yes"},
			message.ToolResult{ToolCallID: "b1", Content: "a"},
			message.ToolResult{ToolCallID: "s1", Content: "done"},
			message.ToolResult{ToolCallID: "b2", Content: "/"},
		),
	}

	items := buildItems(msgs, chatState{})
	require.Equal(t, []string{"ask", "group", "group", "group"}, kinds(items))
	require.Equal(t, "Ship it?", items[0].Text)
	require.Equal(t, "Yes", items[0].Answer)
	// A sub-agent is never folded in with the steps around it.
	require.Equal(t, "agent", items[2].Steps[0].Kind)
	require.Len(t, items[1].Steps, 1)
	require.Len(t, items[3].Steps, 1)
}

func TestStepDetail(t *testing.T) {
	t.Parallel()

	msgs := []message.Message{
		assistantMsg("a1",
			message.ReasoningContent{Thinking: "Hmm."},
			message.ToolCall{ID: "tc1", Name: "bash", Input: `{"command":"make"}`, Finished: true},
			message.ToolCall{ID: "tc2", Name: "edit", Input: `{"file_path":"x.go","old_string":"one\n","new_string":"two\n"}`, Finished: true},
		),
		toolMsg("t1", message.ToolResult{ToolCallID: "tc1", Content: "ignored", Metadata: `{"output":"built"}`}),
	}

	d, ok := stepDetail(msgs, "tc1")
	require.True(t, ok)
	require.Equal(t, "make", d.Input)
	require.Equal(t, "built", d.Output)

	// A pending edit already shows its change.
	d, ok = stepDetail(msgs, "tc2")
	require.True(t, ok)
	var added, removed int
	for _, l := range d.Diff {
		switch l.Kind {
		case "+":
			added++
		case "-":
			removed++
		}
	}
	require.Equal(t, 1, added)
	require.Equal(t, 1, removed)

	d, ok = stepDetail(msgs, "a1:think")
	require.False(t, ok)
	require.Empty(t, d.Output)

	_, ok = stepDetail(msgs, "nope")
	require.False(t, ok)
}

func TestClipKeepsTheTail(t *testing.T) {
	t.Parallel()

	var long []byte
	for i := range detailMaxLines + 10 {
		long = append(long, byte('a'+i%26), '\n')
	}
	out, cut := clip(string(long))
	require.True(t, cut)
	require.Len(t, out, detailMaxLines*2-1)
}
