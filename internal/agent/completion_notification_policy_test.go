package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

type completionOutcomeModel struct {
	finishStreamModel
	reason    fantasy.FinishReason
	reasoning string
	err       error
}

type completionCompactModel struct {
	finishStreamModel
	calls   atomic.Int32
	summary completionGate
}

func (m *completionCompactModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	switch m.calls.Add(1) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "work", ToolCallName: "work", ToolCallInput: "{}"}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls, Usage: fantasy.Usage{InputTokens: 190_000, OutputTokens: 1}})
		}, nil
	case 2:
		close(m.summary.entered)
		select {
		case <-m.summary.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.finishStreamModel.Stream(ctx, call)
}

func (m *completionOutcomeModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		parts := []fantasy.StreamPart{
			{Type: fantasy.StreamPartTypeReasoningStart, ID: "thought"},
			{Type: fantasy.StreamPartTypeReasoningDelta, ID: "thought", Delta: m.reasoning},
			{Type: fantasy.StreamPartTypeReasoningEnd, ID: "thought"},
			{Type: fantasy.StreamPartTypeTextStart, ID: "text"},
			{Type: fantasy.StreamPartTypeTextDelta, ID: "text", Delta: m.text},
			{Type: fantasy.StreamPartTypeTextEnd, ID: "text"},
			{Type: fantasy.StreamPartTypeFinish, FinishReason: m.reason},
		}
		for _, part := range parts {
			if !yield(part) {
				return
			}
		}
	}, nil
}

func TestCompletionNotificationRequiresUserWork(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		call       SessionAgentCall
		child      bool
		subAgent   bool
		running    bool // a sub-agent of the session is still running
		text       string
		reason     fantasy.FinishReason
		reasoning  string
		err        error
		wantNotify bool
	}{
		{name: "user reply", text: "done", wantNotify: true},
		{name: "user reply while a sub-agent runs", running: true, text: "done", wantNotify: true},
		{name: "plan handoff", call: SessionAgentCall{HiddenUserMessage: true}, text: "done", wantNotify: true},
		{name: "unprompted CLI reply", call: SessionAgentCall{CLIContinue: true, HiddenUserMessage: true}, text: "done", wantNotify: true},
		{name: "unprompted CLI reply while a sub-agent runs", call: SessionAgentCall{CLIContinue: true, HiddenUserMessage: true}, running: true, text: "done"},
		{name: "task result", call: SessionAgentCall{Prompt: taskNotification(Task{Name: "worker", Status: TaskDone}, "done")}, text: "done", wantNotify: true},
		{name: "task result while a sub-agent runs", call: SessionAgentCall{Prompt: taskNotification(Task{Name: "worker", Status: TaskDone}, "done")}, running: true, text: "done"},
		{name: "answered questions", call: SessionAgentCall{Prompt: "<crush-task-result>\n<name>Questions</name>\n<status>answered</status>\n</crush-task-result>"}, text: "done", wantNotify: true},
		{name: "answered questions while a sub-agent runs", call: SessionAgentCall{Prompt: "<crush-task-result>\n<name>Questions</name>\n<status>answered</status>\n</crush-task-result>"}, running: true, text: "done"},
		{name: "secure entry result", call: SessionAgentCall{Prompt: "<crush-task-result>\n<name>Secure entry</name>\n<status>done</status>\n</crush-task-result>"}, text: "done", wantNotify: true},
		{name: "sub-agent flag", subAgent: true, text: "done"},
		{name: "child session", child: true, text: "done"},
		{name: "noninteractive", call: SessionAgentCall{NonInteractive: true}, text: "done"},
		{name: "empty response"},
		{name: "whitespace response", text: " \n\t"},
		{name: "reasoning only", reasoning: "thinking"},
		{name: "truncated response", reason: fantasy.FinishReasonLength, text: "partial"},
		{name: "refused response", reason: fantasy.FinishReasonContentFilter, text: "partial"},
		{name: "stream error", err: errors.New("stream failed")},
		{name: "cancelled stream", err: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := testEnv(t)
			reason := test.reason
			if reason == "" {
				reason = fantasy.FinishReasonStop
			}
			model := &completionOutcomeModel{finishStreamModel: finishStreamModel{text: test.text}, reason: reason, reasoning: test.reasoning, err: test.err}
			agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			agent.isSubAgent = test.subAgent
			broker := pubsub.NewBroker[notify.Notification]()
			t.Cleanup(broker.Shutdown)
			agent.notify = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "session")
			require.NoError(t, err)
			if test.child {
				sess, err = env.sessions.CreateTaskSession(t.Context(), "child", sess.ID, "worker")
				require.NoError(t, err)
			}
			if test.running {
				agent.tasks = &taskHub{dir: t.TempDir(), tasks: map[string]*Task{"t1": {SessionID: sess.ID, Status: TaskRunning}}}
			}
			call := test.call
			call.SessionID = sess.ID
			if call.Prompt == "" {
				call.Prompt = "user request"
			}
			_, err = agent.Run(t.Context(), call)
			if test.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.err)
			}
			if test.wantNotify {
				requireCompletionNotification(t, events, sess.ID)
			} else {
				requireNoCompletionNotification(t, events)
			}
		})
	}
}

func TestCompletionNotificationFollowUpsWaitForSubAgents(t *testing.T) {
	t.Parallel()
	agent, env := newStreamTestAgent(t)
	broker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(broker.Shutdown)
	agent.notify = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "user request"})
	require.NoError(t, err)
	requireCompletionNotification(t, events, sess.ID)
	worker := &Task{SessionID: sess.ID, Status: TaskRunning}
	agent.tasks = &taskHub{dir: t.TempDir(), tasks: map[string]*Task{"t1": worker}}
	followUps := []SessionAgentCall{
		{SessionID: sess.ID, Prompt: "continue", HiddenUserMessage: true, CLIContinue: true},
		{SessionID: sess.ID, Prompt: taskNotification(Task{Name: "worker", Status: TaskDone}, "done")},
	}
	for _, call := range followUps {
		_, err = agent.Run(t.Context(), call)
		require.NoError(t, err)
		requireNoCompletionNotification(t, events)
	}
	worker.Status = TaskDone
	for _, call := range followUps {
		_, err = agent.Run(t.Context(), call)
		require.NoError(t, err)
		requireCompletionNotification(t, events, sess.ID)
	}
}

func TestCompletionNotificationRequiresFinishedAssistant(t *testing.T) {
	t.Parallel()
	for _, reason := range []message.FinishReason{
		"", message.FinishReasonUnknown, message.FinishReasonToolUse,
		message.FinishReasonCanceled, message.FinishReasonError,
	} {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()
			agent, _ := newStreamTestAgent(t)
			broker := pubsub.NewBroker[notify.Notification]()
			t.Cleanup(broker.Shutdown)
			agent.notify = broker
			events := broker.Subscribe(t.Context())
			assistant := &message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "partial"}}}
			if reason != "" {
				assistant.AddFinish(reason, "", "")
			}
			agent.notifySessionFinished(SessionAgentCall{SessionID: "session", Prompt: "user request"}, session.Session{ID: "session", Title: "session"}, assistant)
			requireNoCompletionNotification(t, events)
		})
	}
}

func TestCompletionNotificationPreservesContinuationOrigin(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		original   string
		wantNotify bool
	}{
		{name: "user auto-compact continuation", original: "user request", wantNotify: true},
		{name: "task auto-compact continuation", original: taskNotification(Task{Name: "worker", Status: TaskDone}, "done"), wantNotify: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := testEnv(t)
			model := &completionCompactModel{finishStreamModel: finishStreamModel{text: "done"}, summary: completionGate{make(chan struct{}), make(chan struct{})}}
			t.Cleanup(func() {
				select {
				case <-model.summary.release:
				default:
					close(model.summary.release)
				}
			})
			tool := fantasy.NewAgentTool("work", "Do work", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return fantasy.NewTextResponse("work done"), nil
			})
			agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system", tool).(*sessionAgent)
			broker := pubsub.NewBroker[notify.Notification]()
			t.Cleanup(broker.Shutdown)
			agent.notify = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "session")
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: test.original})
				done <- err
			}()
			waitCompletionGate(t, model.summary)
			requireNoCompletionNotification(t, events)
			close(model.summary.release)
			require.NoError(t, <-done)
			require.EqualValues(t, 3, model.calls.Load(), "work, summary, and continuation must each run")
			sess, err = env.sessions.Get(t.Context(), sess.ID)
			require.NoError(t, err)
			require.NotEmpty(t, sess.SummaryMessageID)
			if test.wantNotify {
				requireCompletionNotification(t, events, sess.ID)
			} else {
				requireNoCompletionNotification(t, events)
			}
		})
	}
}

func TestCompletionNotificationWaitsForQueuedChain(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		last       SessionAgentCall
		wantNotify bool
	}{
		{name: "user chain", last: SessionAgentCall{Prompt: "last", RunID: "last"}, wantNotify: true},
		{name: "hidden final turn", last: SessionAgentCall{Prompt: "continue", HiddenUserMessage: true, CLIContinue: true, RunID: "last"}, wantNotify: true},
		{name: "internal final turn", last: SessionAgentCall{Prompt: taskNotification(Task{Name: "worker", Status: TaskDone}, "done"), RunID: "last"}, wantNotify: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := testEnv(t)
			model := &completionGatedModel{finishStreamModel: finishStreamModel{text: "done"}}
			for range 3 {
				model.gates = append(model.gates, completionGate{make(chan struct{}), make(chan struct{})})
			}
			t.Cleanup(func() {
				for _, gate := range model.gates {
					select {
					case <-gate.release:
					default:
						close(gate.release)
					}
				}
			})
			agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			broker := pubsub.NewBroker[notify.Notification]()
			t.Cleanup(broker.Shutdown)
			agent.notify = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "session")
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "main", RunID: "main"})
				done <- err
			}()
			waitCompletionGate(t, model.gates[0])
			_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "follow-up", RunID: "middle"})
			require.NoError(t, err)
			last := test.last
			last.SessionID = sess.ID
			_, err = agent.Run(t.Context(), last)
			require.NoError(t, err)
			turns := 2 // main, then the complete queue batch
			if test.last.CLIContinue {
				turns = 3 // native continuations have separate dispatch semantics
			}
			for i := range turns - 1 {
				close(model.gates[i].release)
				waitCompletionGate(t, model.gates[i+1])
				requireNoCompletionNotification(t, events)
			}
			close(model.gates[turns-1].release)
			require.NoError(t, <-done)
			if test.wantNotify {
				requireCompletionNotification(t, events, sess.ID)
			} else {
				requireNoCompletionNotification(t, events)
			}
		})
	}
}

func TestCompletionNotificationInterrupt(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &completionGatedModel{finishStreamModel: finishStreamModel{text: "done"}}
	for range 2 {
		model.gates = append(model.gates, completionGate{make(chan struct{}), make(chan struct{})})
	}
	t.Cleanup(func() {
		for _, gate := range model.gates {
			select {
			case <-gate.release:
			default:
				close(gate.release)
			}
		}
	})
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	broker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(broker.Shutdown)
	agent.notify = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "main"})
		done <- err
	}()
	waitCompletionGate(t, model.gates[0])
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "follow-up"})
	require.NoError(t, err)
	agent.Interrupt(sess.ID)
	require.ErrorIs(t, <-done, context.Canceled)
	waitCompletionGate(t, model.gates[1])
	requireNoCompletionNotification(t, events)
	close(model.gates[1].release)
	requireCompletionNotification(t, events, sess.ID)
	// Interrupt dispatches its replacement on a goroutine; let its deferred
	// cleanup finish before the test's database is closed.
	require.Eventually(t, func() bool { return !agent.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
}

func TestCompletionNotificationCancelActive(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &completionGatedModel{
		finishStreamModel: finishStreamModel{text: "done"},
		gates:             []completionGate{{make(chan struct{}), make(chan struct{})}},
	}
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	broker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(broker.Shutdown)
	agent.notify = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "main"})
		done <- err
	}()
	waitCompletionGate(t, model.gates[0])
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "queued"})
	require.NoError(t, err)
	agent.Cancel(sess.ID)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, agent.QueuedPrompts(sess.ID))
	requireNoCompletionNotification(t, events)
}

func TestCompletionNotificationTitleAndSummary(t *testing.T) {
	t.Parallel()
	agent, env := newStreamTestAgent(t)
	broker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(broker.Shutdown)
	agent.notify = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	agent.GenerateTitle(t.Context(), sess.ID, "user request")
	requireNoCompletionNotification(t, events)
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{
		Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "summarize this"}},
	})
	require.NoError(t, err)
	require.NoError(t, agent.Summarize(t.Context(), sess.ID, nil, nil))
	sess, err = env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, sess.SummaryMessageID)
	requireNoCompletionNotification(t, events)
}
