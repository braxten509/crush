package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Tree actions and branch copy over the wire (engine-design.md 5.6 and
// 5.11). These tests live outside package server so they can drive the
// routes through the real client, as the desktop app does.

// treeSummary is what the fake model answers a branch summary with.
const treeSummary = "The coast branch was dropped."

// treeLLM is an OpenAI-compatible chat server. A streamed reply says
// "Done."; a held one streams until released. A plain request (the branch
// summary) gets treeSummary.
type treeLLM struct {
	srv       *httptest.Server
	streaming chan struct{} // closed once the held reply streams
	release   chan struct{} // closed to let the held reply end
	once      sync.Once

	mu       sync.Mutex
	holdNext bool
}

func newTreeLLM(t *testing.T) *treeLLM {
	t.Helper()
	l := &treeLLM{streaming: make(chan struct{}), release: make(chan struct{})}
	l.srv = httptest.NewServer(http.HandlerFunc(l.serve))
	t.Cleanup(func() {
		l.finish()
		l.srv.Close()
	})
	return l
}

// hold makes the next streamed reply stream until finish.
func (l *treeLLM) hold() {
	l.mu.Lock()
	l.holdNext = true
	l.mu.Unlock()
}

// finish lets the held reply end.
func (l *treeLLM) finish() { l.once.Do(func() { close(l.release) }) }

func (l *treeLLM) waitStreaming(t *testing.T) {
	t.Helper()
	select {
	case <-l.streaming:
	case <-time.After(30 * time.Second):
		t.Fatal("the reply never started streaming")
	}
}

func (l *treeLLM) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "test-model",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": treeSummary},
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 6, "total_tokens": 16},
		})
		return
	}
	l.mu.Lock()
	held := l.holdNext
	l.holdNext = false
	l.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(delta map[string]any, finish any) {
		data, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "test-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	send(map[string]any{"role": "assistant", "content": "Working"}, nil)
	for i := 0; held; i++ {
		if i == 5 {
			close(l.streaming)
		}
		select {
		case <-l.release:
			held = false
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Millisecond):
			send(map[string]any{"content": "."}, nil)
		}
	}
	send(map[string]any{"content": " Done."}, nil)
	send(map[string]any{}, "stop")
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// treeEngine is a real engine behind a test server, with one workspace
// whose agent talks to a fake model, and the client of one app.
type treeEngine struct {
	c    *client.Client
	ws   *backend.Workspace
	id   string
	info proto.Workspace
	url  string
	hc   *http.Client
	llm  *treeLLM
}

// newTreeEngine starts the engine. HOME and the XDG folders are temporary,
// so a test using it must not run in parallel.
func newTreeEngine(t *testing.T) *treeEngine {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	llm := newTreeLLM(t)

	srv := server.NewServer(nil, "unix", "")
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	c, err := client.NewClient(t.TempDir(), "tcp", hs.Listener.Addr().String())
	require.NoError(t, err)
	created, err := c.CreateWorkspace(t.Context(), prototest.CreateRequest(proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir()}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.DeleteWorkspace(context.Background(), created.ID) })
	ws, err := srv.Backend().GetWorkspace(created.ID)
	require.NoError(t, err)

	const providerID, modelID = "test-openai-compat", "test-model"
	ws.Cfg.Config().Providers.Set(providerID, config.ProviderConfig{
		ID: providerID, Name: "Test", Type: openaicompat.Name,
		BaseURL: llm.srv.URL + "/v1", APIKey: "test",
		Models: []catwalk.Model{{ID: modelID, ContextWindow: 200_000, DefaultMaxTokens: 4096}},
	})
	selected := config.SelectedModel{Provider: providerID, Model: modelID}
	ws.Cfg.OverridePreferredModel(config.SelectedModelTypeLarge, selected)
	ws.Cfg.OverridePreferredModel(config.SelectedModelTypeSmall, selected)
	ws.Cfg.SetupAgents()
	coder := ws.Cfg.Config().Agents[config.AgentCoder]
	coder.AllowedTools = nil // the fake model never calls a tool
	ws.Cfg.Config().Agents[config.AgentCoder] = coder
	require.NoError(t, c.InitiateAgentProcessing(t.Context(), created.ID, false))
	return &treeEngine{c: c, ws: ws, id: created.ID, info: *created, url: hs.URL, hc: hs.Client(), llm: llm}
}

// send calls a route directly as this engine's app and returns the status
// and, for a refusal, its error body.
func (e *treeEngine) send(t *testing.T, method, path string, body any) (int, proto.Error) {
	t.Helper()
	return e.sendAs(t, e.c.ClientID(), method, path, body)
}

// sendAs is send with clientID as the client_id, or with none for "".
func (e *treeEngine) sendAs(t *testing.T, clientID, method, path string, body any) (int, proto.Error) {
	t.Helper()
	var reader io.Reader = http.NoBody
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	target := e.url + path
	if clientID != "" {
		target += "?client_id=" + url.QueryEscape(clientID)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, target, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.hc.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var refusal proto.Error
	if resp.StatusCode != http.StatusOK {
		_ = json.NewDecoder(resp.Body).Decode(&refusal)
	}
	return resp.StatusCode, refusal
}

// treeChat is a chat with two branches under its first answer, the head
// on the second: u1 → a1 → u2 → a2, and a1 → u3 → a3.
type treeChat struct{ id, u1, a1, u2, a2, u3, a3 string }

func (e *treeEngine) treeChat(t *testing.T) treeChat {
	t.Helper()
	ctx := t.Context()
	chat, err := e.ws.Sessions.Create(ctx, "Trip")
	require.NoError(t, err)
	add := func(role message.MessageRole, text string) string {
		parts := []message.ContentPart{message.TextContent{Text: text}}
		if role == message.Assistant {
			parts = append(parts, message.Finish{Reason: message.FinishReasonEndTurn})
		}
		m, err := e.ws.Messages.Create(ctx, chat.ID, message.CreateMessageParams{Role: role, Parts: parts})
		require.NoError(t, err)
		return m.ID
	}
	c := treeChat{id: chat.ID}
	c.u1 = add(message.User, "Plan a trip")
	c.a1 = add(message.Assistant, "Where to?")
	c.u2 = add(message.User, "The coast")
	c.a2 = add(message.Assistant, "The coast it is.")
	require.NoError(t, e.ws.Messages.(message.TreeService).SwitchTree(ctx, chat.ID, c.a1))
	c.u3 = add(message.User, "The mountains")
	c.a3 = add(message.Assistant, "The mountains it is.")
	return c
}

func (e *treeEngine) tree(t *testing.T, sessionID string) proto.Tree {
	t.Helper()
	tree, err := e.c.Tree(t.Context(), e.id, sessionID)
	require.NoError(t, err)
	return tree
}

// texts lists the text of each message on a chat's current path.
func (e *treeEngine) texts(t *testing.T, sessionID string) []string {
	t.Helper()
	msgs, err := e.c.ListMessages(t.Context(), e.id, sessionID)
	require.NoError(t, err)
	var out []string
	for _, m := range msgs {
		out = append(out, m.Content().Text)
	}
	return out
}

// subscribe opens this app's event stream and waits until it is attached.
func (e *treeEngine) subscribe(t *testing.T) <-chan any {
	t.Helper()
	events, err := e.c.SubscribeEvents(t.Context(), e.id)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return backend.WorkspaceLiveStreamCountForTest(e.ws) > 0 }, 5*time.Second, 10*time.Millisecond)
	return events
}

// waitEvent returns the first event of type pubsub.Event[T] that match
// accepts, skipping the others.
func waitEvent[T any](t *testing.T, events <-chan any, match func(pubsub.Event[T]) bool) pubsub.Event[T] {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			require.True(t, ok, "the event stream ended")
			if e, ok := ev.(pubsub.Event[T]); ok && match(e) {
				return e
			}
		case <-timeout:
			var zero T
			t.Fatalf("no matching %T event", zero)
		}
	}
}

// waitAll reads events until each match has accepted one, in any order:
// engine-sent events and the app's own (sessions) reach the stream by
// different paths.
func waitAll(t *testing.T, events <-chan any, matches ...func(any) bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for pending := slices.Clone(matches); len(pending) > 0; {
		select {
		case ev, ok := <-events:
			require.True(t, ok, "the event stream ended")
			pending = slices.DeleteFunc(pending, func(match func(any) bool) bool { return match(ev) })
		case <-timeout:
			t.Fatalf("%d expected events never came", len(pending))
		}
	}
}

// eventOf matches an event of type pubsub.Event[T] that match accepts.
func eventOf[T any](match func(pubsub.Event[T]) bool) func(any) bool {
	return func(ev any) bool {
		e, ok := ev.(pubsub.Event[T])
		return ok && match(e)
	}
}

func byMessage(tree proto.Tree) map[string]proto.TreeEntry {
	out := map[string]proto.TreeEntry{}
	for _, entry := range tree.Entries {
		out[entry.MessageID] = entry
	}
	return out
}

func activeEntries(tree proto.Tree) []string {
	var out []string
	for _, entry := range tree.Entries {
		if entry.Active {
			out = append(out, entry.MessageID)
		}
	}
	return out
}

func textOf(m proto.Message) string { return m.Content().Text }

// List, label, switch, navigate with and without a summary, fork and
// clone, all through the client.
func TestTreeRoutesRoundTrip(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.treeChat(t)

	tree := e.tree(t, chat.id)
	require.Len(t, tree.Entries, 6)
	entries := byMessage(tree)
	require.Empty(t, entries[chat.u1].ParentID)
	require.Equal(t, chat.a1, entries[chat.u2].ParentID)
	require.Equal(t, chat.a1, entries[chat.u3].ParentID)
	require.Equal(t, chat.u3, entries[chat.a3].ParentID)
	require.Equal(t, []string{chat.a3}, activeEntries(tree))
	require.Equal(t, "The coast it is.", textOf(entries[chat.a2].Message))
	require.Equal(t, "assistant", string(entries[chat.a2].Message.Role))

	msg, err := e.c.TreeMessage(ctx, e.id, chat.id, chat.a2)
	require.NoError(t, err)
	require.Equal(t, chat.a2, msg.ID)
	require.Equal(t, "The coast it is.", msg.Content().Text)
	require.True(t, msg.IsFinished(), "the whole message, finish part included")

	labeled, err := e.c.LabelTree(ctx, e.id, chat.id, proto.TreeLabelRequest{ExpectedRevision: tree.Revision, Target: chat.a2, Label: "coast"})
	require.NoError(t, err)
	tree = e.tree(t, chat.id)
	require.Equal(t, labeled.Revision, tree.Revision)
	require.Equal(t, "coast", byMessage(tree)[chat.a2].Label)

	switched, err := e.c.SwitchTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2})
	require.NoError(t, err)
	require.Greater(t, switched.Revision, tree.Revision)
	tree = e.tree(t, chat.id)
	require.Equal(t, switched.Revision, tree.Revision)
	require.Equal(t, []string{chat.a2}, activeEntries(tree))
	require.Equal(t, []string{"Plan a trip", "Where to?", "The coast", "The coast it is."}, e.texts(t, chat.id))

	navigated, err := e.c.NavigateTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a3})
	require.NoError(t, err)
	tree = e.tree(t, chat.id)
	require.Equal(t, navigated.Revision, tree.Revision)
	require.Equal(t, []string{chat.a3}, activeEntries(tree))
	require.Len(t, tree.Entries, 6, "no summary was asked for")

	// With a summary, the fake model sums up the branch left behind, and
	// the note becomes the head under the target.
	summarized, err := e.c.NavigateTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2, Summarize: true})
	require.NoError(t, err)
	tree = e.tree(t, chat.id)
	require.Equal(t, summarized.Revision, tree.Revision)
	require.Len(t, tree.Entries, 7)
	head := activeEntries(tree)
	require.Len(t, head, 1)
	note := byMessage(tree)[head[0]]
	require.Equal(t, chat.a2, note.ParentID)
	require.Contains(t, textOf(note.Message), treeSummary)

	// A fork from the coast prompt ends before it, and hands the prompt
	// back for the typing box.
	forked, err := e.c.CopyTree(ctx, e.id, chat.id, proto.TreeCopyRequest{ExpectedRevision: tree.Revision, Target: chat.u2, Fork: true})
	require.NoError(t, err)
	require.Equal(t, "The coast", forked.Prompt)
	require.Equal(t, []string{"Plan a trip", "Where to?"}, e.texts(t, forked.SessionID))
	after := e.tree(t, chat.id)
	require.Equal(t, after.Revision, forked.Revision)
	require.Equal(t, head, activeEntries(after), "a copy leaves the source's head")

	cloned, err := e.c.CopyTree(ctx, e.id, chat.id, proto.TreeCopyRequest{ExpectedRevision: after.Revision, Target: chat.a3})
	require.NoError(t, err)
	require.Empty(t, cloned.Prompt)
	require.NotEqual(t, forked.SessionID, cloned.SessionID)
	require.Equal(t, []string{"Plan a trip", "Where to?", "The mountains", "The mountains it is."}, e.texts(t, cloned.SessionID))
}

// Every route that changes the tree refuses a stale revision with 409
// revision_conflict, and nothing changes.
func TestTreeStaleRevisionRefused(t *testing.T) {
	e := newTreeEngine(t)
	chat := e.treeChat(t)
	stale := e.tree(t, chat.id).Revision
	_, err := e.c.SwitchTree(t.Context(), e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: stale, Target: chat.a2})
	require.NoError(t, err)
	before := e.tree(t, chat.id)
	require.NotEqual(t, stale, before.Revision)
	sessions, err := e.c.ListSessions(t.Context(), e.id)
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		path string
		body any
	}{
		{"switch", rpc.SwitchTree.URL(e.id, chat.id), proto.TreeMoveRequest{ExpectedRevision: stale, Target: chat.a3}},
		{"navigate", rpc.NavigateTree.URL(e.id, chat.id), proto.TreeMoveRequest{ExpectedRevision: stale, Target: chat.a3, Summarize: true}},
		{"label", rpc.LabelTree.URL(e.id, chat.id), proto.TreeLabelRequest{ExpectedRevision: stale, Target: chat.a3, Label: "late"}},
		{"fork", rpc.CopyTree.URL(e.id, chat.id), proto.TreeCopyRequest{ExpectedRevision: stale, Target: chat.u3, Fork: true}},
		{"clone", rpc.CopyTree.URL(e.id, chat.id), proto.TreeCopyRequest{ExpectedRevision: stale, Target: chat.a3}},
	} {
		status, refusal := e.send(t, http.MethodPost, tc.path, tc.body)
		require.Equal(t, http.StatusConflict, status, tc.name)
		require.Equal(t, "revision_conflict", refusal.Code, tc.name)
	}
	require.Equal(t, before, e.tree(t, chat.id), "head, labels and revision are unchanged")
	after, err := e.c.ListSessions(t.Context(), e.id)
	require.NoError(t, err)
	require.Len(t, after, len(sessions), "no copy was made")
}

// A reply in progress makes the tree routes answer 409 tree_busy with the
// guard's own text.
func TestTreeBusyIs409(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.treeChat(t)
	tree := e.tree(t, chat.id)

	e.llm.hold()
	require.NoError(t, e.c.SendMessage(ctx, e.id, chat.id, "", "", "And the lakes?"))
	e.llm.waitStreaming(t)
	const busy = "wait for the reply and queued prompts to finish, or recall queued prompts first"
	for _, tc := range []struct {
		name string
		path string
		body any
	}{
		{"switch", rpc.SwitchTree.URL(e.id, chat.id), proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2}},
		{"navigate", rpc.NavigateTree.URL(e.id, chat.id), proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2}},
		{"clone", rpc.CopyTree.URL(e.id, chat.id), proto.TreeCopyRequest{ExpectedRevision: tree.Revision, Target: chat.a2}},
	} {
		status, refusal := e.send(t, http.MethodPost, tc.path, tc.body)
		require.Equal(t, http.StatusConflict, status, tc.name)
		require.Equal(t, "tree_busy", refusal.Code, tc.name)
		require.Equal(t, busy, refusal.Message, tc.name)
	}

	// Once the reply is done, the same switch goes through.
	e.llm.finish()
	require.Eventually(t, func() bool {
		_, err := e.c.SwitchTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2})
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, []string{chat.a2}, activeEntries(e.tree(t, chat.id)))
}

// Every app's stream gets tree_changed with the new revision after a switch
// and after a label.
func TestTreeChangedEvent(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.treeChat(t)
	events := e.subscribe(t)
	tree := e.tree(t, chat.id)

	switched, err := e.c.SwitchTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: tree.Revision, Target: chat.a2})
	require.NoError(t, err)
	changed := waitEvent(t, events, func(ev pubsub.Event[proto.TreeChanged]) bool { return ev.Payload.SessionID == chat.id })
	require.Equal(t, pubsub.UpdatedEvent, changed.Type)
	require.Equal(t, switched.Revision, changed.Payload.Revision)
	require.Greater(t, changed.Payload.Revision, tree.Revision)

	labeled, err := e.c.LabelTree(ctx, e.id, chat.id, proto.TreeLabelRequest{ExpectedRevision: switched.Revision, Target: chat.a3, Label: "mountains"})
	require.NoError(t, err)
	changed = waitEvent(t, events, func(ev pubsub.Event[proto.TreeChanged]) bool { return ev.Payload.SessionID == chat.id })
	require.Equal(t, labeled.Revision, changed.Payload.Revision)
}

// A fork or clone, written straight to the database, still reaches every
// app's stream as a created session.
func TestCopyAnnouncesNewChat(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.treeChat(t)
	events := e.subscribe(t)
	tree := e.tree(t, chat.id)

	cloned, err := e.c.CopyTree(ctx, e.id, chat.id, proto.TreeCopyRequest{ExpectedRevision: tree.Revision, Target: chat.a2})
	require.NoError(t, err)
	var created pubsub.Event[proto.Session]
	waitAll(t, events,
		eventOf(func(ev pubsub.Event[proto.Session]) bool {
			if ev.Type != pubsub.CreatedEvent || ev.Payload.ID != cloned.SessionID {
				return false
			}
			created = ev
			return true
		}),
		eventOf(func(ev pubsub.Event[proto.TreeChanged]) bool {
			return ev.Payload.SessionID == chat.id && ev.Payload.Revision == cloned.Revision
		}),
	)
	require.Empty(t, created.Payload.ParentSessionID)

	forked, err := e.c.CopyTree(ctx, e.id, chat.id, proto.TreeCopyRequest{ExpectedRevision: cloned.Revision, Target: chat.u3, Fork: true})
	require.NoError(t, err)
	waitAll(t, events, eventOf(func(ev pubsub.Event[proto.Session]) bool {
		return ev.Type == pubsub.CreatedEvent && ev.Payload.ID == forked.SessionID
	}))
}

// notesChat is a chat whose AI changed notes.txt from v1 to v2, which the
// file now holds: first is the prompt, edit the answer whose tool made the
// change.
type notesChat struct{ id, first, edit, path string }

// notesChat makes the chat. File history skips temporary folders, so the
// file lives in a folder of the test's own.
func (e *treeEngine) notesChat(t *testing.T) notesChat {
	t.Helper()
	ctx := t.Context()
	root, err := os.MkdirTemp(".", "tree-restore-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	c := notesChat{path: filepath.Join(root, "notes.txt")}
	require.NoError(t, os.WriteFile(c.path, []byte("v2\n"), 0o644))
	require.NoError(t, os.Chmod(c.path, 0o644))

	chat, err := e.ws.Sessions.Create(ctx, "Notes")
	require.NoError(t, err)
	c.id = chat.ID
	add := func(role message.MessageRole, parts ...message.ContentPart) string {
		m, err := e.ws.Messages.Create(ctx, chat.ID, message.CreateMessageParams{Role: role, Parts: parts})
		require.NoError(t, err)
		return m.ID
	}
	state := func(text string) *filechange.State {
		return &filechange.State{Content: text, Size: int64(len(text)), Mode: 0o644}
	}
	c.first = add(message.User, message.TextContent{Text: "Edit the notes"})
	c.edit = add(message.Assistant, message.ToolCall{ID: "c1", Name: "edit", Input: "{}", Finished: true}, message.Finish{Reason: message.FinishReasonToolUse})
	add(message.Tool, message.ToolResult{ToolCallID: "c1", Name: "edit", Content: "edited", Review: &filechange.Review{
		Root: root, Changes: []filechange.Change{{Path: c.path, Order: 1, Before: state("v1\n"), After: state("v2\n")}},
	}})
	add(message.Assistant, message.TextContent{Text: "Done."}, message.Finish{Reason: message.FinishReasonEndTurn})
	return c
}

// read returns what notes.txt holds.
func (c notesChat) read(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(c.path)
	require.NoError(t, err)
	return string(data)
}

// A file preview's plan is applied by its plan_id once; a used or unknown
// plan gives 409 plan_expired, and a plan older than the chat's last move
// 409 revision_conflict. The undo of the restore works the same way.
func TestFilePreviewUndoByPlanID(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.notesChat(t)

	plan, err := e.c.PreviewFiles(ctx, e.id, chat.id, proto.FilePreviewRequest{Target: chat.edit})
	require.NoError(t, err)
	require.NotEmpty(t, plan.PlanID)
	require.Equal(t, chat.id, plan.SessionID)
	require.Equal(t, chat.edit, plan.Target)
	require.Len(t, plan.Entries, 1)
	require.Equal(t, chat.path, plan.Entries[0].Path)
	require.Equal(t, "restore", plan.Entries[0].Action)
	require.Empty(t, plan.Entries[0].Conflict)
	require.Empty(t, plan.Entries[0].Unavailable)

	restored, err := e.c.UndoFiles(ctx, e.id, chat.id, plan.PlanID)
	require.NoError(t, err)
	require.Equal(t, 1, restored.Restored)
	require.NotEmpty(t, restored.UndoID)
	require.Equal(t, "Restored 1 files. Use Undo Last File Restore in the command palette to put them back.", restored.Notice)
	require.Equal(t, "v1\n", chat.read(t))

	// A plan is used once; an unknown one is refused the same way.
	for _, id := range []string{plan.PlanID, "no-such-plan"} {
		status, refusal := e.send(t, http.MethodPost, rpc.UndoFiles.URL(e.id, chat.id), proto.FileUndoRequest{PlanID: id})
		require.Equal(t, http.StatusConflict, status, id)
		require.Equal(t, "plan_expired", refusal.Code, id)
	}
	require.Equal(t, "v1\n", chat.read(t))

	undo, err := e.c.PreviewUndoFiles(ctx, e.id, chat.id)
	require.NoError(t, err)
	require.Equal(t, restored.UndoID, undo.UndoID)
	require.Len(t, undo.Entries, 1)
	undone, err := e.c.UndoFiles(ctx, e.id, chat.id, undo.PlanID)
	require.NoError(t, err)
	require.Equal(t, 1, undone.Restored)
	require.Equal(t, "v2\n", chat.read(t))

	// A preview made before the chat moved is refused, and kept files stay.
	stale, err := e.c.PreviewFiles(ctx, e.id, chat.id, proto.FilePreviewRequest{Target: chat.edit})
	require.NoError(t, err)
	_, err = e.c.SwitchTree(ctx, e.id, chat.id, proto.TreeMoveRequest{ExpectedRevision: stale.Revision, Target: chat.first})
	require.NoError(t, err)
	status, refusal := e.send(t, http.MethodPost, rpc.UndoFiles.URL(e.id, chat.id), proto.FileUndoRequest{PlanID: stale.PlanID})
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "revision_conflict", refusal.Code)
	require.Equal(t, "v2\n", chat.read(t))
}

// A preview made before a new message is refused with 409 plan_expired: the
// message moved the head but not the revision, so the plan can never apply.
// No file is restored and the chat stays as it is.
func TestFilePlanStaleAfterNewMessage(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.notesChat(t)

	plan, err := e.c.PreviewFiles(ctx, e.id, chat.id, proto.FilePreviewRequest{Target: chat.edit})
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)

	require.NoError(t, e.c.SendMessage(ctx, e.id, chat.id, "", "", "Thanks"))
	require.Eventually(t, func() bool {
		tree, err := e.c.Tree(ctx, e.id, chat.id)
		if err != nil || e.ws.AgentCoordinator.IsSessionBusy(chat.id) {
			return false
		}
		last := tree.Entries[len(tree.Entries)-1]
		return last.Active && textOf(last.Message) == "Working Done."
	}, 10*time.Second, 20*time.Millisecond, "the reply never finished")
	before := e.tree(t, chat.id)
	require.Equal(t, plan.Revision, before.Revision, "a new message keeps the revision")
	require.NotEqual(t, []string{plan.Source}, activeEntries(before), "a new message moves the head")

	status, refusal := e.send(t, http.MethodPost, rpc.UndoFiles.URL(e.id, chat.id), proto.FileUndoRequest{PlanID: plan.PlanID})
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, "plan_expired", refusal.Code)
	require.Equal(t, "v2\n", chat.read(t), "no file was restored")
	require.Equal(t, before, e.tree(t, chat.id), "the chat did not change")
}

// A fork through a ClientWorkspace with an approved file restore takes the
// restore's undo record along, as in-process: the new chat can undo the
// restore, and the source chat has none left.
func TestForkMovesUndoRecord(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.notesChat(t)
	ws := workspace.NewClientWorkspace(e.c, e.info)

	plan, err := ws.PreviewFiles(ctx, chat.id, chat.first, true)
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	request := &message.FileRestoreRequest{Plan: plan}
	forked, prompt, err := ws.CopyTree(message.WithFileRestore(ctx, request), chat.id, chat.first, true)
	require.NoError(t, err)
	require.Equal(t, "Edit the notes", prompt)
	require.Equal(t, 1, request.Result.Restored)
	require.NotEmpty(t, request.Result.UndoID)
	require.Equal(t, "v1\n", chat.read(t))

	undo, err := ws.PreviewUndoFiles(ctx, forked)
	require.NoError(t, err)
	require.Equal(t, request.Result.UndoID, undo.UndoID)
	_, err = ws.PreviewUndoFiles(ctx, chat.id)
	require.ErrorContains(t, err, "There is no file restore to undo in this chat")
}

// The optional client_id of the tree routes: none passes, a malformed one
// is a 400, and one whose app is not on the workspace a 409. A refused
// call changes nothing.
func TestTreeClientID(t *testing.T) {
	e := newTreeEngine(t)
	chat := e.treeChat(t)
	for _, tc := range []struct {
		name, clientID string
		status         int
		code           string
	}{
		{"malformed", "not-a-uuid", http.StatusBadRequest, "bad_request"},
		{"app not on the workspace", uuid.NewString(), http.StatusConflict, "client_not_attached"},
		{"none", "", http.StatusOK, ""},
	} {
		label := "label for " + tc.name
		req := proto.TreeLabelRequest{ExpectedRevision: e.tree(t, chat.id).Revision, Target: chat.a2, Label: label}
		status, refusal := e.sendAs(t, tc.clientID, http.MethodPost, rpc.LabelTree.URL(e.id, chat.id), req)
		require.Equal(t, tc.status, status, tc.name)
		require.Equal(t, tc.code, refusal.Code, tc.name)
		labeled := byMessage(e.tree(t, chat.id))[chat.a2].Label == label
		require.Equal(t, tc.status == http.StatusOK, labeled, tc.name)
	}
}

// A tree message read names its chat: a message of another chat is 404.
func TestTreeMessageOtherChat404(t *testing.T) {
	e := newTreeEngine(t)
	chat := e.treeChat(t)
	other := e.treeChat(t)

	status, refusal := e.send(t, http.MethodGet, rpc.GetTreeMessage.URL(e.id, chat.id, other.a2), nil)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "not_found", refusal.Code)
	_, err := e.c.TreeMessage(t.Context(), e.id, chat.id, other.a2)
	require.ErrorIs(t, err, client.ErrNotFound)

	msg, err := e.c.TreeMessage(t.Context(), e.id, other.id, other.a2)
	require.NoError(t, err)
	require.Equal(t, other.a2, msg.ID)
}

// A branch is a top-level chat: parent_session_id stays "" (the column
// that hides helper chats from chat lists), and its link to the source is
// a chat_link origin.
func TestBranchParentSessionIDEmpty(t *testing.T) {
	e := newTreeEngine(t)
	ctx := t.Context()
	chat := e.treeChat(t)
	events := e.subscribe(t)

	const words = "mountains"
	start := strings.Index("The mountains it is.", words)
	branch, err := e.c.CreateBranch(ctx, e.id, chat.id, proto.BranchRequest{
		MessageID: chat.a3, HighlightText: words, HighlightStart: start, HighlightEnd: start + len(words), Title: "Mountains",
	})
	require.NoError(t, err)
	require.Empty(t, branch.Session.ParentSessionID)
	require.Equal(t, "Mountains", branch.Session.Title)
	require.Equal(t, 4, branch.Copied)
	require.Equal(t, chat.a3, branch.CopiedThrough)
	link := branch.Link
	require.True(t, link.Origin)
	require.Equal(t, branch.Session.ID, link.SessionID)
	require.Equal(t, chat.id, link.ParentSessionID)
	require.Equal(t, chat.a3, link.SourceMessageID)
	require.Equal(t, words, link.HighlightText)
	require.Equal(t, start, link.HighlightStart)

	read, err := e.c.GetSession(ctx, e.id, branch.Session.ID)
	require.NoError(t, err)
	require.Empty(t, read.ParentSessionID)
	listed, err := e.c.ListSessions(ctx, e.id)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(listed, func(s proto.Session) bool { return s.ID == branch.Session.ID }), "chat lists show the branch")
	links, err := e.c.ChatLinks(ctx, e.id)
	require.NoError(t, err)
	require.Contains(t, links, link)

	var linked pubsub.Event[proto.ChatLink]
	waitAll(t, events,
		eventOf(func(ev pubsub.Event[proto.Session]) bool {
			return ev.Type == pubsub.CreatedEvent && ev.Payload.ID == branch.Session.ID
		}),
		eventOf(func(ev pubsub.Event[proto.ChatLink]) bool {
			if ev.Payload.ID != link.ID {
				return false
			}
			linked = ev
			return true
		}),
	)
	require.Equal(t, pubsub.CreatedEvent, linked.Type)
	require.Equal(t, link, linked.Payload)
}
