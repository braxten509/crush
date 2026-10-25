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

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
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

// messageLog records when each message update was published.
type messageLog struct {
	mu      sync.Mutex
	updates map[string][]time.Time
}

func watchMessages(ctx context.Context, ws *Workspace) *messageLog {
	l := &messageLog{updates: map[string][]time.Time{}}
	events := ws.Messages.Subscribe(ctx)
	go func() {
		for ev := range events {
			if ev.Type != pubsub.UpdatedEvent {
				continue
			}
			l.mu.Lock()
			l.updates[ev.Payload.ID] = append(l.updates[ev.Payload.ID], time.Now())
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *messageLog) of(id string) []time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.updates[id])
}

// longestGap is the longest time between two writes in a row that spans
// any part of [from, to].
func longestGap(writes []time.Time, from, to time.Time) time.Duration {
	var longest time.Duration
	for i := 1; i < len(writes); i++ {
		if writes[i].Before(from) || writes[i-1].After(to) {
			continue
		}
		longest = max(longest, writes[i].Sub(writes[i-1]))
	}
	return longest
}

// A branch made while the AI replies in the source chat copies only
// finished messages and returns while the reply streams. The copy holds
// the single database connection briefly: neither the copy nor the
// reply's writes during it take longer than the bound (engine-design.md
// 5.11, Risks). Then one send in the branch, which ends with the user's
// message, goes through the provider.
func TestBranchWhileReplying(t *testing.T) {
	llm := newReplyLLM(t)
	b, ws, clientID := replyWorkspace(t, llm)
	ctx := t.Context()
	updates := watchMessages(ctx, ws)

	source, err := ws.Sessions.Create(ctx, "Source")
	require.NoError(t, err)
	_, err = ws.Sessions.SetModel(ctx, source.ID, session.ModelChoice(`{"provider":"test-openai-compat","model":"test-model"}`))
	require.NoError(t, err)
	words := strings.Repeat("and then some more words ", 10)
	var lastAnswer message.Message
	for i := 1; i <= 500; i++ {
		_, err := ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{
			Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Question %d %s", i, words)}},
		})
		require.NoError(t, err)
		lastAnswer, err = ws.Messages.Create(ctx, source.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Answer %d %s", i, words)}, message.Finish{Reason: message.FinishReasonEndTurn}},
		})
		require.NoError(t, err)
	}

	branchAt := func(m message.Message, highlight, title string) (proto.Branch, time.Duration, error) {
		start := strings.Index(m.Content().Text, highlight) // ASCII: bytes are UTF-16 units
		began := time.Now()
		branch, err := b.CreateBranch(ctx, ws.ID, source.ID, proto.BranchRequest{
			MessageID: m.ID, HighlightText: highlight, HighlightStart: start, HighlightEnd: start + len(highlight), Title: title,
		})
		return branch, time.Since(began), err
	}

	// The baseline: the same chat copied three times with no reply running,
	// each to a new branch from its own words, since highlights may not
	// overlap. The fastest copy is the least disturbed by other work on the
	// machine.
	var baselines []time.Duration
	for i, highlight := range []string{"Answer 500", "and then", "some more"} {
		branch, took, err := branchAt(lastAnswer, highlight, fmt.Sprintf("Baseline %d", i+1))
		require.NoError(t, err)
		require.Equal(t, 1000, branch.Copied)
		baselines = append(baselines, took)
	}
	baseline := slices.Min(baselines)
	// The bound is loose: the baseline and the copy made mid-reply are taken
	// at different moments, and the machine's load can change in between.
	// It still catches a copy that holds the database for long.
	bound := max(2*time.Second, 4*baseline)
	t.Logf("baseline copies of 1,000 messages took %v; the fastest, %v, makes the bound %v", baselines, baseline, bound)
	// Every wait from here on outlasts the bound: a machine slow enough to
	// stretch the baseline is as slow for the rest. The held reply never
	// ends by itself, so a call that waits for it still fails.
	patience := bound + 10*time.Second

	llm.hold()
	require.NoError(t, b.SendMessage(ws.ID, clientID, proto.AgentMessage{SessionID: source.ID, Prompt: "Tell me about step 501"}))
	select {
	case <-llm.streaming:
	case <-time.After(max(30*time.Second, patience)):
		t.Fatal("the reply never started streaming")
	}
	msgs, err := ws.Messages.List(ctx, source.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1002)
	prompt, reply := msgs[1000], msgs[1001]
	require.Equal(t, message.User, prompt.Role)
	require.Equal(t, message.Assistant, reply.Role)
	require.Eventually(t, func() bool { return len(updates.of(reply.ID)) >= 3 }, patience, 5*time.Millisecond,
		"the reply writes as it streams")
	require.True(t, ws.AgentCoordinator.IsSessionBusy(source.ID))

	type result struct {
		branch proto.Branch
		took   time.Duration
		err    error
	}
	done := make(chan result, 1)
	copyStart := time.Now()
	go func() {
		branch, took, err := branchAt(prompt, "step 501", "Step 501")
		done <- result{branch, took, err}
	}()
	var mid result
	select {
	case mid = <-done:
	case <-time.After(patience):
		t.Fatal("the branch call waited for the reply")
	}
	copyEnd := time.Now()
	require.NoError(t, mid.err)
	require.True(t, ws.AgentCoordinator.IsSessionBusy(source.ID), "the call returned while the reply streams")
	require.Less(t, mid.took, bound, "the copy made mid-reply")
	t.Logf("the copy made mid-reply took %v", mid.took)

	// The reply writes on past the copy, then ends in the source.
	time.Sleep(200 * time.Millisecond)
	llm.finish()
	require.Eventually(t, func() bool {
		_ = ws.Messages.Flush(ctx, reply.ID)
		m, err := ws.Messages.Get(ctx, reply.ID)
		return err == nil && m.IsFinished() && strings.HasSuffix(m.Content().Text, " Done.")
	}, patience, 10*time.Millisecond, "the reply finishes in the source")
	writes := updates.of(reply.ID)
	require.True(t, writes[0].Before(copyStart) && writes[len(writes)-1].After(copyEnd), "the reply wrote before and after the copy")
	stall := longestGap(writes, copyStart, copyEnd)
	t.Logf("the reply's longest wait between writes during the copy: %v", stall)
	require.Less(t, stall, bound, "the reply's writes during the copy")

	// The branch holds the chat through the prompt, finished messages only.
	require.Empty(t, mid.branch.Session.ParentSessionID)
	require.Equal(t, 1001, mid.branch.Copied)
	copied, err := ws.Messages.List(ctx, mid.branch.Session.ID)
	require.NoError(t, err)
	require.Len(t, copied, 1001)
	for _, m := range copied {
		require.True(t, m.IsFinished(), "only finished messages are copied")
		require.NotContains(t, m.Content().Text, "Working")
	}
	require.Equal(t, message.User, copied[1000].Role)
	require.Equal(t, prompt.Content().Text, copied[1000].Content().Text)

	// One send in the branch, whose last message is the user's.
	require.NoError(t, b.SendMessage(ws.ID, clientID, proto.AgentMessage{SessionID: mid.branch.Session.ID, Prompt: "And step 502?"}))
	require.Eventually(t, func() bool {
		_ = ws.Messages.FlushAll(ctx)
		msgs, err := ws.Messages.List(ctx, mid.branch.Session.ID)
		if err != nil || len(msgs) != 1003 {
			return false
		}
		answer := msgs[1002]
		return answer.Role == message.Assistant && answer.IsFinished() && answer.Content().Text == "Working Done."
	}, patience, 10*time.Millisecond, "the branch answers its first send")
	requests := llm.requests()
	roles := requests[len(requests)-1]
	t.Logf("the branch's request ended with the roles %v", roles[max(0, len(roles)-3):])
}
