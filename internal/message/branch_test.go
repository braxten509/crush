package message_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// branchChats is a migrated database with the real message service.
type branchChats struct {
	q    *db.Queries
	msgs message.Service
}

func newBranchChats(t *testing.T) branchChats {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dir) })
	q := db.New(conn)
	return branchChats{q: q, msgs: message.NewService(q, message.WithDebounce(0))}
}

func (c branchChats) chat(t *testing.T, id string) {
	t.Helper()
	_, err := c.q.CreateSession(t.Context(), db.CreateSessionParams{ID: id, Title: id})
	require.NoError(t, err)
}

func (c branchChats) add(t *testing.T, sessionID string, role message.MessageRole, parts ...message.ContentPart) message.Message {
	t.Helper()
	m, err := c.msgs.Create(t.Context(), sessionID, message.CreateMessageParams{Role: role, Parts: parts, Model: "model", Provider: "provider"})
	require.NoError(t, err)
	return m
}

func (c branchChats) branch(ctx context.Context, req message.BranchRequest) (message.BranchResult, error) {
	return c.msgs.(message.BranchService).Branch(ctx, req)
}

// units is where word starts and ends in text, in UTF-16 code units.
func units(text, word string) (int, int) {
	i := strings.Index(text, word)
	if i < 0 {
		return -1, -1
	}
	start := len(utf16.Encode([]rune(text[:i])))
	return start, start + len(utf16.Encode([]rune(word)))
}

func TestBranchRefusesToolOutputAndBadHighlight(t *testing.T) {
	c := newBranchChats(t)
	ctx := t.Context()
	c.chat(t, "src")
	c.chat(t, "other")
	const text = "Hi 😀 there friend"
	u := c.add(t, "src", message.User, message.TextContent{Text: "Say hello"})
	a := c.add(t, "src", message.Assistant, message.TextContent{Text: text},
		message.ToolCall{ID: "c1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
		message.Finish{Reason: message.FinishReasonToolUse})
	tool := c.add(t, "src", message.Tool, message.ToolResult{ToolCallID: "c1", Name: "bash", Content: "there friend"})
	elsewhere := c.add(t, "other", message.User, message.TextContent{Text: "there friend"})

	start, end := units(text, "there")
	require.Equal(t, 6, start, "the emoji is two UTF-16 code units")
	ok := message.BranchRequest{SessionID: "src", MessageID: a.ID, HighlightText: "there", HighlightStart: start, HighlightEnd: end, Title: "There"}
	byteStart := strings.Index(text, "there")
	require.Equal(t, 8, byteStart, "and four bytes")

	for _, tc := range []struct {
		name string
		edit func(*message.BranchRequest)
	}{
		{"another chat's message", func(r *message.BranchRequest) { r.MessageID = elsewhere.ID; r.HighlightStart, r.HighlightEnd = 0, 5 }},
		{"a missing message", func(r *message.BranchRequest) { r.MessageID = uuid.NewString() }},
		{"tool output", func(r *message.BranchRequest) { r.MessageID = tool.ID; r.HighlightStart, r.HighlightEnd = 0, 5 }},
		{"other words", func(r *message.BranchRequest) { r.HighlightText = "friend" }},
		{"byte offsets after an emoji", func(r *message.BranchRequest) {
			r.HighlightStart, r.HighlightEnd = byteStart, byteStart+len("there")
		}},
		{"negative start", func(r *message.BranchRequest) { r.HighlightStart = -1 }},
		{"empty range", func(r *message.BranchRequest) { r.HighlightEnd = r.HighlightStart }},
		{"past the end", func(r *message.BranchRequest) {
			r.HighlightStart, r.HighlightEnd = start, len(utf16.Encode([]rune(text)))+1
		}},
		{"no title", func(r *message.BranchRequest) { r.Title = "  \t " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ok
			tc.edit(&req)
			_, err := c.branch(ctx, req)
			require.ErrorIs(t, err, message.ErrBadCut)
		})
	}
	links, err := c.q.ChatLinks(ctx)
	require.NoError(t, err)
	require.Empty(t, links, "a refused branch writes nothing")

	// UTF-16 offsets after the emoji are accepted, and so is a highlight
	// of the emoji itself.
	res, err := c.branch(ctx, ok)
	require.NoError(t, err)
	require.Equal(t, tool.ID, res.CopiedThrough)
	emoji := ok
	emoji.HighlightText, emoji.HighlightStart, emoji.HighlightEnd = "😀", 3, 5
	_, err = c.branch(ctx, emoji)
	require.NoError(t, err)

	// A user message branches too, and a long title is cut to 200 characters.
	long := strings.Repeat("é", 250)
	res, err = c.branch(ctx, message.BranchRequest{SessionID: "src", MessageID: u.ID, HighlightText: "hello", HighlightStart: 4, HighlightEnd: 9, Title: "  " + long + "  "})
	require.NoError(t, err)
	branch, err := c.q.GetSessionByID(ctx, res.SessionID)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", 200), branch.Title)
}

func TestBranchRefusesUnfinished(t *testing.T) {
	c := newBranchChats(t)
	ctx := t.Context()
	c.chat(t, "src")
	c.add(t, "src", message.User, message.TextContent{Text: "Look around"})
	tools := c.add(t, "src", message.Assistant, message.TextContent{Text: "Looking at two files."},
		message.ToolCall{ID: "c1", Name: "view", Input: `{"file_path":"a"}`, Finished: true},
		message.ToolCall{ID: "c2", Name: "view", Input: `{"file_path":"b"}`, Finished: true},
		message.ToolCall{ID: "web", Name: "web_search", Input: `{}`, Finished: true, ProviderExecuted: true},
		message.Finish{Reason: message.FinishReasonToolUse})
	c.add(t, "src", message.Tool, message.ToolResult{ToolCallID: "c1", Name: "view", Content: "a"})

	ask := func(m message.Message, word string) (message.BranchResult, error) {
		start, end := units(m.Content().Text, word)
		return c.branch(ctx, message.BranchRequest{SessionID: "src", MessageID: m.ID, HighlightText: word, HighlightStart: start, HighlightEnd: end, Title: word})
	}

	// One of the reply's tools has no result yet (the one the provider
	// ran needs none).
	_, err := ask(tools, "two files")
	require.ErrorIs(t, err, message.ErrCutNotFinished)

	second := c.add(t, "src", message.Tool, message.ToolResult{ToolCallID: "c2", Name: "view", Content: "b"})
	res, err := ask(tools, "two files")
	require.NoError(t, err)
	require.Equal(t, second.ID, res.CopiedThrough)
	require.Equal(t, 4, res.Copied)

	// A reply still being written has no finish part yet.
	writing := c.add(t, "src", message.Assistant, message.TextContent{Text: "Both files say hello."})
	_, err = ask(writing, "hello")
	require.ErrorIs(t, err, message.ErrCutNotFinished)
	writing.AddFinish(message.FinishReasonEndTurn, "", "")
	require.NoError(t, c.msgs.Update(ctx, writing))
	_, err = ask(writing, "hello")
	require.NoError(t, err)
}

// The same words, or words inside them, may open any number of branches
// (owner, 2026-11-19).
func TestSameWordsBranchMoreThanOnce(t *testing.T) {
	c := newBranchChats(t)
	ctx := t.Context()
	c.chat(t, "src")
	c.add(t, "src", message.User, message.TextContent{Text: "Name two colors"})
	reply := c.add(t, "src", message.Assistant, message.TextContent{Text: "amber, cobalt"}, message.Finish{Reason: message.FinishReasonEndTurn})
	at := func(start, end int) message.BranchRequest {
		text := utf16.Encode([]rune(reply.Content().Text))
		return message.BranchRequest{SessionID: "src", MessageID: reply.ID, HighlightText: string(utf16.Decode(text[start:end])), HighlightStart: start, HighlightEnd: end, Title: "colors"}
	}
	for _, r := range [][2]int{{0, 5}, {0, 5}, {2, 3}, {0, 13}} {
		_, err := c.branch(ctx, at(r[0], r[1]))
		require.NoError(t, err, "[%d,%d)", r[0], r[1])
	}
}
