package message_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/server"
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
		want string
	}{
		{"another chat's message", func(r *message.BranchRequest) { r.MessageID = elsewhere.ID }, "the message belongs to another chat"},
		{"a missing message", func(r *message.BranchRequest) { r.MessageID = uuid.NewString() }, "the message belongs to another chat"},
		{"tool output", func(r *message.BranchRequest) { r.MessageID = tool.ID; r.HighlightStart, r.HighlightEnd = 0, 5 }, "branch from message text, not tool output"},
		{"other words", func(r *message.BranchRequest) { r.HighlightText = "friend" }, "the highlight doesn't match the message"},
		{"byte offsets after an emoji", func(r *message.BranchRequest) {
			r.HighlightStart, r.HighlightEnd = byteStart, byteStart+len("there")
		}, "the highlight doesn't match the message"},
		{"negative start", func(r *message.BranchRequest) { r.HighlightStart = -1 }, "the highlight doesn't match the message"},
		{"empty range", func(r *message.BranchRequest) { r.HighlightEnd = r.HighlightStart }, "the highlight doesn't match the message"},
		{"past the end", func(r *message.BranchRequest) {
			r.HighlightStart, r.HighlightEnd = start, len(utf16.Encode([]rune(text)))+1
		}, "the highlight doesn't match the message"},
		{"no title", func(r *message.BranchRequest) { r.Title = "  \t " }, "the branch needs a title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ok
			tc.edit(&req)
			_, err := c.branch(ctx, req)
			require.ErrorIs(t, err, message.ErrBadCut)
			require.EqualError(t, err, tc.want)
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
	require.EqualError(t, err, "this reply's tools are still running")

	second := c.add(t, "src", message.Tool, message.ToolResult{ToolCallID: "c2", Name: "view", Content: "b"})
	res, err := ask(tools, "two files")
	require.NoError(t, err)
	require.Equal(t, second.ID, res.CopiedThrough)
	require.Equal(t, 4, res.Copied)

	// A reply still being written has no finish part yet.
	writing := c.add(t, "src", message.Assistant, message.TextContent{Text: "Both files say hello."})
	_, err = ask(writing, "hello")
	require.ErrorIs(t, err, message.ErrCutNotFinished)
	require.EqualError(t, err, "this reply is still being written")
	writing.AddFinish(message.FinishReasonEndTurn, "", "")
	require.NoError(t, c.msgs.Update(ctx, writing))
	_, err = ask(writing, "hello")
	require.NoError(t, err)
}

func TestHighlightOverlapRefused(t *testing.T) {
	c := newBranchChats(t)
	ctx := t.Context()
	c.chat(t, "src")
	c.add(t, "src", message.User, message.TextContent{Text: "Name two colors"})
	reply := c.add(t, "src", message.Assistant, message.TextContent{Text: "amber, cobalt"}, message.Finish{Reason: message.FinishReasonEndTurn})
	at := func(start, end int) message.BranchRequest {
		text := utf16.Encode([]rune(reply.Content().Text))
		return message.BranchRequest{SessionID: "src", MessageID: reply.ID, HighlightText: string(utf16.Decode(text[start:end])), HighlightStart: start, HighlightEnd: end, Title: "colors"}
	}
	_, err := c.branch(ctx, at(0, 5))
	require.NoError(t, err)
	// Ranges are half-open: [0,5) and [5,9) only touch.
	_, err = c.branch(ctx, at(5, 9))
	require.NoError(t, err)
	for _, r := range [][2]int{{4, 6}, {0, 5}, {2, 3}, {8, 13}, {0, 13}} {
		_, err = c.branch(ctx, at(r[0], r[1]))
		require.ErrorIs(t, err, message.ErrHighlightOverlapsLink, "[%d,%d)", r[0], r[1])
	}
	_, err = c.branch(ctx, at(9, 13))
	require.NoError(t, err)

	// Over HTTP the refusal is 409 highlight_overlaps_link.
	t.Run("http", func(t *testing.T) { highlightOverlapOverHTTP(t) })
}

// highlightOverlapOverHTTP drives the branch route of a real engine.
func highlightOverlapOverHTTP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := t.Context()
	srv := server.NewServer(nil, "unix", "")
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	clientID := uuid.NewString()
	post := func(path string, body any) *http.Response {
		t.Helper()
		data, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL+path+"?client_id="+clientID, bytes.NewReader(data))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := hs.Client().Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	resp := post("/v1/workspaces", prototest.CreateRequest(proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir(), ClientID: clientID}))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var created proto.Workspace
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	t.Cleanup(func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, hs.URL+"/v1/workspaces/"+created.ID+"?client_id="+clientID, nil)
		if err == nil {
			if resp, err := hs.Client().Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}
	})
	ws, err := srv.Backend().GetWorkspace(created.ID)
	require.NoError(t, err)
	chat, err := ws.Sessions.Create(ctx, "Colors")
	require.NoError(t, err)
	_, err = ws.Messages.Create(ctx, chat.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Name two colors"}}})
	require.NoError(t, err)
	reply, err := ws.Messages.Create(ctx, chat.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{
		message.TextContent{Text: "amber, cobalt"}, message.Finish{Reason: message.FinishReasonEndTurn},
	}})
	require.NoError(t, err)

	branch := func(start, end int, word string) *http.Response {
		return post("/v1/workspaces/"+created.ID+"/sessions/"+chat.ID+"/branches", proto.BranchRequest{
			MessageID: reply.ID, HighlightText: word, HighlightStart: start, HighlightEnd: end, Title: word,
		})
	}
	require.Equal(t, http.StatusOK, branch(0, 5, "amber").StatusCode)
	require.Equal(t, http.StatusOK, branch(5, 9, ", co").StatusCode, "touching ranges are allowed")
	resp = branch(3, 7, "er, ")
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	var body proto.Error
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "highlight_overlaps_link", body.Code)
}
