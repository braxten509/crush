package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// replyLLM is an OpenAI-compatible chat server whose replies stream. A
// held reply sends a word every few milliseconds until it is released.
type replyLLM struct {
	srv       *httptest.Server
	streaming chan struct{} // closed once the held reply streams
	release   chan struct{} // closed to let the held reply end
	once      sync.Once

	mu       sync.Mutex
	holdNext bool
	roles    [][]string // each request's message roles
}

func newReplyLLM(t *testing.T) *replyLLM {
	t.Helper()
	l := &replyLLM{streaming: make(chan struct{}), release: make(chan struct{})}
	l.srv = httptest.NewServer(http.HandlerFunc(l.serve))
	t.Cleanup(func() {
		l.finish()
		l.srv.Close()
	})
	return l
}

// hold makes the next reply stream until finish.
func (l *replyLLM) hold() {
	l.mu.Lock()
	l.holdNext = true
	l.mu.Unlock()
}

// finish lets the held reply end.
func (l *replyLLM) finish() { l.once.Do(func() { close(l.release) }) }

func (l *replyLLM) requests() [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.roles)
}

func (l *replyLLM) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	roles := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		roles = append(roles, m.Role)
	}
	l.mu.Lock()
	l.roles = append(l.roles, roles)
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

// replyWorkspace creates a real workspace whose agent talks to llm. HOME
// and the XDG folders are temporary, so the test must not run in
// parallel.
func replyWorkspace(t *testing.T, llm *replyLLM) (*Backend, *Workspace, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	b := New(context.Background(), nil, func() {})
	b.SetCreateGrace(time.Hour) // the app never opens a stream
	t.Cleanup(func() { drainBackend(t, b) })
	clientID := uuid.New().String()
	ws, _, err := b.CreateWorkspace(protoWS(t.TempDir(), t.TempDir(), clientID))
	require.NoError(t, err)

	const providerID, modelID = "test-openai-compat", "test-model"
	ws.Cfg.Config().Providers.Set(providerID, config.ProviderConfig{
		ID: providerID, Name: "Test", Type: openaicompat.Name,
		BaseURL: llm.srv.URL + "/v1", APIKey: "test",
		Models: []catwalk.Model{{ID: modelID, ContextWindow: 200_000, DefaultMaxTokens: 4096}},
	})
	ws.Cfg.OverrideHelperModel(config.SelectedModel{Provider: providerID, Model: modelID})
	ws.Cfg.SetupAgents()
	coder := ws.Cfg.Config().Agents[config.AgentCoder]
	coder.AllowedTools = nil // no tools: the fake model never calls one
	ws.Cfg.Config().Agents[config.AgentCoder] = coder
	require.NoError(t, b.InitAgent(t.Context(), ws.ID, clientID, false))
	return b, ws, clientID
}

// A branch made while the AI replies in the source chat copies only
// finished messages and returns while the reply streams, without waiting
// for it. Then one send in the branch, which ends with the user's
// message, goes through the provider.
func TestBranchWhileReplying(t *testing.T) {
	llm := newReplyLLM(t)
	b, ws, clientID := replyWorkspace(t, llm)
	ctx := t.Context()
	// The held reply never ends by itself, so a call that waits for it
	// runs into this limit.
	const patience = 10 * time.Second

	source, err := ws.Sessions.Create(ctx, "Source")
	require.NoError(t, err)
	_, err = ws.Sessions.SetModel(ctx, source.ID, session.ModelChoice(`{"provider":"test-openai-compat","model":"test-model"}`))
	require.NoError(t, err)
	const pairs = 5
	for i := 1; i <= pairs; i++ {
		_, err := ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{
			Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Question %d", i)}},
		})
		require.NoError(t, err)
		_, err = ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Answer %d", i)}, message.Finish{Reason: message.FinishReasonEndTurn}},
		})
		require.NoError(t, err)
	}

	llm.hold()
	require.NoError(t, b.SendMessage(ws.ID, clientID, proto.AgentMessage{SessionID: source.ID, Prompt: "Tell me about step 6"}))
	select {
	case <-llm.streaming:
	case <-time.After(patience):
		t.Fatal("the reply never started streaming")
	}
	msgs, err := ws.Messages.List(ctx, source.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2*pairs+2)
	prompt, reply := msgs[2*pairs], msgs[2*pairs+1]
	require.Equal(t, message.User, prompt.Role)
	require.Equal(t, message.Assistant, reply.Role)
	require.True(t, ws.AgentCoordinator.IsSessionBusy(source.ID))

	type result struct {
		branch proto.Branch
		err    error
	}
	done := make(chan result, 1)
	go func() {
		const highlight = "step 6"
		start := strings.Index(prompt.Content().Text, highlight) // ASCII: bytes are UTF-16 units
		branch, err := b.CreateBranch(ctx, ws.ID, source.ID, proto.BranchRequest{
			MessageID: prompt.ID, HighlightText: highlight, HighlightStart: start, HighlightEnd: start + len(highlight), Title: "Step 6",
		})
		done <- result{branch, err}
	}()
	var mid result
	select {
	case mid = <-done:
	case <-time.After(patience):
		t.Fatal("the branch call waited for the reply")
	}
	require.NoError(t, mid.err)
	require.True(t, ws.AgentCoordinator.IsSessionBusy(source.ID), "the call returned while the reply streams")

	// The reply ends in the source.
	llm.finish()
	require.Eventually(t, func() bool {
		_ = ws.Messages.Flush(ctx, reply.ID)
		m, err := ws.Messages.Get(ctx, reply.ID)
		return err == nil && m.IsFinished() && strings.HasSuffix(m.Content().Text, " Done.")
	}, patience, 10*time.Millisecond, "the reply finishes in the source")

	// The branch holds the chat through the prompt, finished messages only.
	require.Empty(t, mid.branch.Session.ParentSessionID)
	require.Equal(t, 2*pairs+1, mid.branch.Copied)
	copied, err := ws.Messages.List(ctx, mid.branch.Session.ID)
	require.NoError(t, err)
	require.Len(t, copied, 2*pairs+1)
	for _, m := range copied {
		require.True(t, m.IsFinished(), "only finished messages are copied")
		require.NotContains(t, m.Content().Text, "Working")
	}
	require.Equal(t, message.User, copied[2*pairs].Role)
	require.Equal(t, prompt.Content().Text, copied[2*pairs].Content().Text)

	// One send in the branch, whose last message is the user's.
	require.NoError(t, b.SendMessage(ws.ID, clientID, proto.AgentMessage{SessionID: mid.branch.Session.ID, Prompt: "And step 7?"}))
	require.Eventually(t, func() bool {
		_ = ws.Messages.FlushAll(ctx)
		msgs, err := ws.Messages.List(ctx, mid.branch.Session.ID)
		if err != nil || len(msgs) != 2*pairs+3 {
			return false
		}
		answer := msgs[2*pairs+2]
		return answer.Role == message.Assistant && answer.IsFinished() && answer.Content().Text == "Working Done."
	}, patience, 10*time.Millisecond, "the branch answers its first send")
	requests := llm.requests()
	require.NotEmpty(t, requests)
	roles := requests[len(requests)-1]
	require.NotEmpty(t, roles)
	require.Equal(t, "user", roles[len(roles)-1], "the branch's request ends with the user's message")
}

// TestBranchStartsOnItsSourceModel: a branch is a copy, and a copy keeps
// its chat's model with its effort and FAST setting (owner, 2026-10-24),
// not the last pick a new chat starts on.
func TestBranchStartsOnItsSourceModel(t *testing.T) {
	b, ws, _ := replyWorkspace(t, newReplyLLM(t))
	ctx := t.Context()
	require.NoError(t, config.WriteLastPick(config.SelectedModel{Provider: "test-openai-compat", Model: "other-model"}))

	const own = `{"provider":"test-openai-compat","model":"test-model","reasoning_effort":"high","service_tier":"fast"}`
	source, err := ws.Sessions.Create(ctx, "Source")
	require.NoError(t, err)
	_, err = ws.Sessions.SetModel(ctx, source.ID, session.ModelChoice(own))
	require.NoError(t, err)
	_, err = ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Plan a trip"}}})
	require.NoError(t, err)
	answer, err := ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "Lisbon in May"}, message.Finish{Reason: message.FinishReasonEndTurn}},
	})
	require.NoError(t, err)

	branch, err := b.CreateBranch(ctx, ws.ID, source.ID, proto.BranchRequest{MessageID: answer.ID, HighlightText: "Lisbon", HighlightStart: 0, HighlightEnd: 6, Title: "Lisbon"})
	require.NoError(t, err)
	require.JSONEq(t, own, string(branch.Session.Model), "the apps are told the branch's own model")
	stored, err := ws.Sessions.Get(ctx, branch.Session.ID)
	require.NoError(t, err)
	require.JSONEq(t, own, string(stored.Model), "the branch keeps its source's model")
}
