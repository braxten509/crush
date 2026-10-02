package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

type sessionRunFakeAgent struct {
	SessionAgent
	run func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)
}

func (a *sessionRunFakeAgent) Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	return a.run(ctx, call)
}

func sessionRunTestCoordinator(t *testing.T, run func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)) *coordinator {
	t.Helper()
	c := newGateTestCoordinator(t, false)
	require.NoError(t, c.readyWg.Wait())
	c.mainAgent = &sessionRunFakeAgent{SessionAgent: c.mainAgent, run: run}
	c.tasks = &taskHub{c: c, tasks: make(map[string]*Task)}
	broker := pubsub.NewBroker[notify.RunComplete]()
	c.runComplete = broker
	t.Cleanup(broker.Shutdown)
	return c
}

func fakeSessionReply(call SessionAgentCall, text string) *fantasy.AgentResult {
	call.OnComplete(notify.RunComplete{SessionID: call.SessionID, RunID: call.RunID, MessageID: text, Text: text})
	return &fantasy.AgentResult{Response: fantasy.Response{Content: fantasy.ResponseContent{fantasy.TextContent{Text: text}}}}
}

func TestSessionRunWaitsThroughTaskResultAndFollowUp(t *testing.T) {
	taskFinished := make(chan struct{})
	deliver := make(chan struct{})
	followUpEntered := make(chan struct{})
	finishFollowUp := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "main" {
			ctx, release := c.tasks.reserveFollowUp(call.SessionID)
			go func() {
				defer release()
				// The task is finished, but its result has not yet been
				// dispatched. Status-based waiting used to exit here.
				close(taskFinished)
				select {
				case <-deliver:
				case <-ctx.Done():
					return
				}
				_, _ = c.Run(ctx, call.SessionID, "result")
			}()
			return fakeSessionReply(call, "before task"), nil
		}
		close(followUpEntered)
		select {
		case <-finishFollowUp:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return fakeSessionReply(call, "reviewed task result"), nil
	})
	events := c.runComplete.(*pubsub.Broker[notify.RunComplete]).Subscribe(ctx)
	done := make(chan *fantasy.AgentResult, 1)
	go func() {
		result, err := c.Run(WithRunID(ctx, "run-main"), "session", "main")
		if err != nil {
			t.Errorf("run: %v", err)
		}
		done <- result
	}()
	select {
	case <-taskFinished:
	case <-ctx.Done():
		t.Fatal("task never finished")
	}
	select {
	case <-done:
		t.Fatal("run exited before result delivery")
	case <-events:
		t.Fatal("terminal event published before result delivery")
	default:
	}
	close(deliver)
	select {
	case <-followUpEntered:
	case <-ctx.Done():
		t.Fatal("follow-up never started")
	}
	select {
	case <-done:
		t.Fatal("run exited during follow-up")
	case <-events:
		t.Fatal("terminal event published during follow-up")
	default:
	}
	close(finishFollowUp)
	select {
	case result := <-done:
		require.Equal(t, "reviewed task result", result.Response.Content.Text())
	case <-ctx.Done():
		t.Fatal("run did not complete")
	}
	select {
	case event := <-events:
		require.Equal(t, "run-main", event.Payload.RunID)
		require.Equal(t, "reviewed task result", event.Payload.Text)
	case <-ctx.Done():
		t.Fatal("missing terminal event")
	}
	select {
	case event := <-events:
		t.Fatalf("duplicate completion: %+v", event)
	default:
	}
}

func TestSessionRunWithoutTasksReturnsImmediately(t *testing.T) {
	want := &fantasy.AgentResult{Response: fantasy.Response{Content: fantasy.ResponseContent{fantasy.TextContent{Text: "plain"}}}}
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		fakeSessionReply(call, "plain")
		return want, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := c.Run(ctx, "session", "main")
	require.NoError(t, err)
	require.Same(t, want, result)
	require.Empty(t, c.tasks.sessionRuns)
}

func TestSessionRunCancellationStopsTaskContext(t *testing.T) {
	taskContext := make(chan context.Context, 1)
	c := sessionRunTestCoordinator(t, nil)
	// The fake reserves the same context that a spawned task receives.
	c.mainAgent.(*sessionRunFakeAgent).run = func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		ctx, release := c.tasks.reserveFollowUp(call.SessionID)
		go func() { <-ctx.Done(); release() }()
		taskContext <- ctx
		return fakeSessionReply(call, "waiting"), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Run(ctx, "session", "main"); done <- err }()
	var childCtx context.Context
	select {
	case childCtx = <-taskContext:
	case <-time.After(5 * time.Second):
		t.Fatal("task never started")
	}
	c.Cancel("session")
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop the waiting run promptly")
	}
	require.ErrorIs(t, childCtx.Err(), context.Canceled)
}

func TestSessionRunCountsEveryProducerAndPropagatesErrors(t *testing.T) {
	r := newSessionRun(t.Context(), "main")
	defer r.cancel()
	require.True(t, r.reserve())
	require.True(t, r.reserve())
	r.release(nil) // main
	r.record(notify.RunComplete{Text: "first"})
	r.release(nil) // first task
	select {
	case <-r.done:
		t.Fatal("second task is still running")
	default:
	}
	require.True(t, r.reserve()) // a follow-up spawns more work
	r.release(nil)               // second task
	r.record(notify.RunComplete{Text: "last"})
	wantErr := errors.New("follow-up failed")
	r.release(wantErr)
	complete, ok, err := r.wait()
	require.True(t, ok)
	require.Equal(t, "last", complete.Text)
	require.ErrorIs(t, err, wantErr)
	require.False(t, r.reserve(), "finished runs cannot accept more work")
}

func TestSessionRunDeadlineStopsProducers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	r := newSessionRun(ctx, "session")
	defer r.cancel()
	require.True(t, r.reserve())
	r.release(nil)
	_, _, err := r.wait()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, r.ctx.Err(), context.DeadlineExceeded)
	r.release(nil)
}

func TestSessionRunKeepsJoinedRequestRunIDs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	producer := make(chan func(), 1)
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "main" {
			_, release := c.tasks.reserveFollowUp(call.SessionID)
			result := fakeSessionReply(call, "before follow-up")
			producer <- release
			return result, nil
		}
		if call.RunID != "joined" {
			return nil, errors.New("joined request lost its RunID")
		}
		if !call.HiddenUserMessage || !call.CLIContinue {
			return nil, errors.New("follow-up lost its provenance")
		}
		return fakeSessionReply(call, "after follow-up"), nil
	})
	events := c.runComplete.(*pubsub.Broker[notify.RunComplete]).Subscribe(ctx)
	done := make(chan error, 1)
	go func() { _, err := c.Run(WithRunID(ctx, "main"), "session", "main"); done <- err }()
	var release func()
	select {
	case release = <-producer:
	case <-ctx.Done():
		t.Fatal("main did not start")
	}
	joinedCtx := withCLIContinue(message.WithHiddenUserMessage(WithRunCompleteMarker(WithRunID(ctx, "joined"))))
	_, err := c.Run(joinedCtx, "session", "follow-up")
	require.NoError(t, err)
	require.True(t, RunCompletePublished(joinedCtx), "the owner will publish the joined request's terminal event")
	release()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("main did not finish")
	}
	got := map[string]string{}
	for range 2 {
		select {
		case event := <-events:
			got[event.Payload.RunID] = event.Payload.Text
		case <-ctx.Done():
			t.Fatal("missing correlated completion")
		}
	}
	require.Equal(t, map[string]string{"main": "after follow-up", "joined": "after follow-up"}, got)
}

func TestSessionRunWaitsForPromptQueuedBehindExistingTurn(t *testing.T) {
	sa, env := newStreamTestAgent(t)
	model := &completionGatedModel{finishStreamModel: finishStreamModel{text: "queued reply"}}
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
	large := sa.Model()
	large.Model = model
	sa.SetModels(large, sa.smallModel.Get())
	c := sessionRunTestCoordinator(t, sa.Run)
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	olderDone := make(chan error, 1)
	go func() {
		_, err := sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, RunID: "older", Prompt: "older", NonInteractive: true})
		olderDone <- err
	}()
	waitCompletionGate(t, model.gates[0])
	done := make(chan error, 1)
	go func() { _, err := c.Run(WithRunID(ctx, "queued"), sess.ID, "queued"); done <- err }()
	require.Eventually(t, func() bool { return sa.QueuedPrompts(sess.ID) == 1 }, time.Second, time.Millisecond)
	select {
	case <-done:
		t.Fatal("queued headless run returned before its turn began")
	default:
	}
	close(model.gates[0].release)
	waitCompletionGate(t, model.gates[1])
	select {
	case <-done:
		t.Fatal("headless run returned during its queued turn")
	default:
	}
	close(model.gates[1].release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("headless run did not finish after the queued turn")
	}
	require.NoError(t, <-olderDone)
}

func TestNonInteractiveAskReturnsCancelledFollowUp(t *testing.T) {
	var c *coordinator
	initialReply := make(chan struct{})
	c = sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "main" {
			err := c.tasks.ask(TaskRequest{Session: call.SessionID, Ask: []byte(`{"questions":[{"type":"free_text","question":"Choose?","description":"Need input"}]}`)})
			if err != nil {
				return nil, err
			}
			result := fakeSessionReply(call, "questions open")
			close(initialReply)
			return result, nil
		}
		select {
		case <-initialReply:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		name, status, ok := ParseTaskNotification(call.Prompt)
		if !ok || name != AskName || status != AskCancelled {
			return nil, errors.New("question result was not cancelled")
		}
		return fakeSessionReply(call, "questions cancelled"), nil
	})
	c.questions = question.NewService()
	sess, err := c.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := c.Run(ctx, sess.ID, "main")
	require.NoError(t, err)
	require.Equal(t, "questions cancelled", result.Response.Content.Text())
	require.False(t, c.questions.Cancel(), "no question form should be left waiting")
}

func TestSessionRunReportsWaitingOnSubAgents(t *testing.T) {
	delay := waitReportDelay
	waitReportDelay = 20 * time.Millisecond
	t.Cleanup(func() { waitReportDelay = delay })

	deliver := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "main" {
			ctx, release := c.tasks.reserveFollowUp(call.SessionID)
			go func() {
				defer release()
				select {
				case <-deliver:
				case <-ctx.Done():
					return
				}
				_, _ = c.Run(ctx, call.SessionID, "result")
			}()
			return fakeSessionReply(call, "before task"), nil
		}
		return fakeSessionReply(call, "reviewed task result"), nil
	})

	statuses := make(chan bool, 8)
	runCtx := WithRunStatus(ctx, func(waiting bool) { statuses <- waiting })
	done := make(chan error, 1)
	go func() {
		_, err := c.Run(runCtx, "session", "main")
		done <- err
	}()

	select {
	case waiting := <-statuses:
		require.True(t, waiting)
	case <-ctx.Done():
		t.Fatal("waiting was never reported")
	}
	close(deliver)
	select {
	case waiting := <-statuses:
		require.False(t, waiting)
	case <-ctx.Done():
		t.Fatal("the end of waiting was never reported")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("run did not complete")
	}
	// The moment between the follow-up turn ending and the task letting go
	// is not a wait.
	time.Sleep(3 * waitReportDelay)
	require.Empty(t, statuses)
}

func TestSessionRunWithoutSubAgentsReportsNothing(t *testing.T) {
	delay := waitReportDelay
	waitReportDelay = 20 * time.Millisecond
	t.Cleanup(func() { waitReportDelay = delay })

	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "done"), nil
	})
	statuses := make(chan bool, 8)
	_, err := c.Run(WithRunStatus(t.Context(), func(waiting bool) { statuses <- waiting }), "session", "main")
	require.NoError(t, err)
	time.Sleep(3 * waitReportDelay)
	require.Empty(t, statuses)
}
