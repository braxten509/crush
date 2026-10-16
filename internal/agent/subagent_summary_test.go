package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

// summarizedWorkerModel grows the context past the compaction limit on its
// first step, writes the summary on the second and the worker's real report
// in the continuation that follows.
type summarizedWorkerModel struct {
	finishStreamModel
	calls atomic.Int32
}

func (m *summarizedWorkerModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	switch m.calls.Add(1) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "work", ToolCallName: "work", ToolCallInput: "{}"}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls, Usage: fantasy.Usage{InputTokens: 190_000, OutputTokens: 1}})
		}, nil
	case 2:
		return (&finishStreamModel{text: "summary of the work so far"}).Stream(ctx, call)
	}
	return m.finishStreamModel.Stream(ctx, call)
}

func newSummarizedWorker(t *testing.T) (*sessionAgent, *summarizedWorkerModel, string) {
	t.Helper()
	env := testEnv(t)
	model := &summarizedWorkerModel{finishStreamModel: finishStreamModel{text: "final report"}}
	tool := fantasy.NewAgentTool("work", "Do work", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("work done"), nil
	})
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system", tool).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "worker")
	require.NoError(t, err)
	return agent, model, sess.ID
}

func TestSubAgentReportWaitsForSummaryContinuation(t *testing.T) {
	t.Parallel()
	agent, model, sessionID := newSummarizedWorker(t)

	result, err := runSubAgentReport(t.Context(), agent, SessionAgentCall{SessionID: sessionID, Prompt: "do the task", NonInteractive: true})

	require.NoError(t, err)
	require.Equal(t, "final report", subAgentOutput(result))
	require.EqualValues(t, 3, model.calls.Load(), "work, summary and continuation must each run")
}

// queuedBehindBusySession starts a held turn on a fresh session and a worker
// call queued behind it, returning the worker's outcome channel.
func queuedBehindBusySession(t *testing.T, ctx context.Context) (*sessionAgent, string, *gatedStreamModel, chan error) {
	t.Helper()
	env := testEnv(t)
	model := &gatedStreamModel{text: "done", gate: make(chan struct{}), entered: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-model.gate:
		default:
			close(model.gate)
		}
	})
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "worker")
	require.NoError(t, err)
	go func() { _, _ = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "held"}) }()
	<-model.entered
	outcome := make(chan error, 1)
	go func() {
		_, err := runToReport(ctx, agent, SessionAgentCall{SessionID: sess.ID, Prompt: "worker task", NonInteractive: true})
		outcome <- err
	}()
	require.Eventually(t, func() bool { return len(agent.QueuedPromptsList(sess.ID)) == 1 }, 5*time.Second, 5*time.Millisecond)
	return agent, sess.ID, model, outcome
}

func TestSubAgentReportDroppedContinuationDoesNotHang(t *testing.T) {
	t.Parallel()
	agent, sessionID, _, outcome := queuedBehindBusySession(t, t.Context())

	agent.ClearQueue(sessionID)

	select {
	case err := <-outcome:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("worker kept waiting for a turn that was thrown away")
	}
}

func TestSubAgentReportWaitStopsWithTheWorker(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	_, _, _, outcome := queuedBehindBusySession(t, ctx)

	cancel()

	select {
	case err := <-outcome:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the worker did not stop its wait")
	}
}

func TestSubAgentReportWaitsForQueuedTurn(t *testing.T) {
	t.Parallel()
	_, _, model, outcome := queuedBehindBusySession(t, t.Context())

	close(model.gate)

	select {
	case err := <-outcome:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker never got the queued turn's answer")
	}
}
