package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// toolStepsModel calls a tool on its first two steps and answers on the
// third. The first step waits for release so a prompt can be queued.
type toolStepsModel struct {
	finishStreamModel
	mu      sync.Mutex
	calls   []fantasy.Call
	entered chan struct{}
	release chan struct{}
}

func (m *toolStepsModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	number := len(m.calls)
	m.mu.Unlock()
	if number == 1 {
		close(m.entered)
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if number <= 2 {
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "work" + string(rune('0'+number)), ToolCallName: "work", ToolCallInput: "{}"}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
		}, nil
	}
	return m.finishStreamModel.Stream(ctx, call)
}

// A prompt queued during a native turn joins it at the next tool boundary
// and stays in the conversation for the rest of the turn.
func TestNativeQueueJoinsAtToolBoundary(t *testing.T) {
	model := &toolStepsModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan struct{}), release: make(chan struct{})}
	env := testEnv(t)
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "native boundary")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "START"})
		done <- err
	}()
	select {
	case <-model.entered:
	case <-ctx.Done():
		t.Fatal("first step never started")
	}
	_, err = sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "QUEUED", SubmissionID: "queued"})
	require.NoError(t, err)
	require.Equal(t, []string{"QUEUED"}, sa.QueuedPromptsList(sess.ID))
	close(model.release)
	require.NoError(t, <-done)

	model.mu.Lock()
	calls := append([]fantasy.Call(nil), model.calls...)
	model.mu.Unlock()
	require.Len(t, calls, 3, "the queued prompt joins the turn instead of starting another")
	require.NotContains(t, batchUserTexts(calls[0]), "QUEUED")
	for _, call := range calls[1:] {
		require.Equal(t, 1, countText(batchUserTexts(call), "QUEUED"), "the queued prompt stays in every later step, once")
	}
	require.Zero(t, sa.QueuedPrompts(sess.ID))
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var submissions []string
	for _, msg := range msgs {
		if msg.Role == message.User && msg.Content().SubmissionID != "" {
			submissions = append(submissions, msg.Content().SubmissionID)
		}
	}
	require.Equal(t, []string{"queued"}, submissions)
}

func countText(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if text == want {
			n++
		}
	}
	return n
}
