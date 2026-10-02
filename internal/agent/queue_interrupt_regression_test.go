package agent

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestInterruptPreservesEachQueuedRequest(t *testing.T) {
	for _, headless := range []bool{false, true} {
		name := "broker"
		if headless {
			name = "headless"
		}
		t.Run(name, func(t *testing.T) {
			env := testEnv(t)
			sa := testSessionAgent(env, &finishStreamModel{text: "done"}, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			broker := pubsub.NewBroker[notify.RunComplete]()
			t.Cleanup(broker.Shutdown)
			sa.runComplete = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "interrupt")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancel})
			var runs []*sessionRun
			for _, id := range []string{"first", "second"} {
				call := SessionAgentCall{SessionID: sess.ID, RunID: id, Prompt: id + " prompt", Attachments: []message.Attachment{{MimeType: "text/plain", FileName: id + ".txt", Content: []byte(id + " attachment")}}}
				if headless {
					run := newSessionRun(t.Context(), sess.ID)
					defer run.cancel()
					runs = append(runs, run)
					call.sessionRun = run
					call.OnComplete = func(complete notify.RunComplete) {
						run.record(complete)
						broker.Publish(pubsub.UpdatedEvent, complete)
					}
				}
				_, err := sa.Run(t.Context(), call)
				require.NoError(t, err)
			}
			// The queued reservation must keep each headless owner alive
			// after its initial submission returns.
			for _, run := range runs {
				run.release(nil)
			}
			sa.Interrupt(sess.ID)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			sa.activeRequests.Del(sess.ID)
			require.Eventually(t, func() bool {
				msgs, _ := env.messages.List(t.Context(), sess.ID)
				return len(msgs) == 4 && !sa.IsSessionBusy(sess.ID)
			}, 5*time.Second, time.Millisecond)
			require.Zero(t, sa.QueuedPrompts(sess.ID))
			got := map[string]notify.RunComplete{}
			for range 2 {
				select {
				case event := <-events:
					require.NotContains(t, got, event.Payload.RunID, "each request completes exactly once")
					got[event.Payload.RunID] = event.Payload
				case <-time.After(time.Second):
					t.Fatalf("queued request never completed: %v", got)
				}
			}
			for _, id := range []string{"first", "second"} {
				require.Equal(t, "done", got[id].Text)
				require.False(t, got[id].Cancelled)
				require.Empty(t, got[id].Error)
			}
			for _, run := range runs {
				select {
				case <-run.done:
				case <-time.After(time.Second):
					t.Fatal("queued headless reservation was never released")
				}
			}
			select {
			case event := <-events:
				t.Fatalf("unexpected extra completion: %+v", event.Payload)
			default:
			}
			msgs, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			require.Len(t, msgs, 4)
			for i, id := range []string{"first", "second"} {
				require.Equal(t, id+" prompt", msgs[i*2].Content().String())
				require.Len(t, msgs[i*2].BinaryContent(), 1)
				require.Equal(t, []byte(id+" attachment"), msgs[i*2].BinaryContent()[0].Data)
			}
		})
	}
}

func TestCLIInterruptWaitsForSteeringTransfer(t *testing.T) {
	env := testEnv(t)
	sa := testSessionAgent(env, &finishStreamModel{text: "done"}, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "steering transfer")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &cliSteps{a: sa, ctx: ctx, sessionID: sess.ID}
	sa.steering.Set(sess.ID, s)
	sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancel})
	sa.messageQueue.Set(sess.ID, []SessionAgentCall{{SessionID: sess.ID, Prompt: "preserve this prompt"}})
	// Pause the real transfer after it drains the queue but before it can
	// record the steered call. Interrupt must wait for the whole transfer.
	s.steerMu.Lock()
	steerLocked := true
	defer func() {
		if steerLocked {
			s.steerMu.Unlock()
		}
	}()
	steered := make(chan string, 1)
	go func() { steered <- s.steer() }()
	require.Eventually(t, func() bool {
		calls, _ := sa.messageQueue.Get(sess.ID)
		return len(calls) == 0
	}, time.Second, time.Millisecond)
	mu := sa.sessionMu(sess.ID)
	transferLocked := !mu.TryLock()
	if !transferLocked {
		mu.Unlock()
	}
	interrupted := make(chan struct{})
	go func() { sa.Interrupt(sess.ID); close(interrupted) }()
	s.steerMu.Unlock()
	steerLocked = false
	require.Equal(t, "preserve this prompt", <-steered)
	select {
	case <-interrupted:
	case <-time.After(time.Second):
		t.Fatal("interrupt deadlocked with steering")
	}
	s.returnUnsteered()
	sa.steering.Del(sess.ID)
	sa.activeRequests.Del(sess.ID)
	require.Eventually(t, func() bool {
		msgs, _ := env.messages.List(t.Context(), sess.ID)
		return len(msgs) == 2 && !sa.IsSessionBusy(sess.ID)
	}, 5*time.Second, time.Millisecond)
	require.True(t, transferLocked, "queue-to-steering handoff must hold the dispatch mutex")
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "preserve this prompt", msgs[0].Content().String())
}

func TestCLIInterruptClaimsSteeredCallsBeforeLateAcknowledgement(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &cliSteps{a: sa, ctx: ctx, sessionID: "late-ack"}
	sa.steering.Set(s.sessionID, s)
	sa.activeRequests.Set(s.sessionID, &activeCancel{cancel: cancel})
	sa.messageQueue.Set(s.sessionID, []SessionAgentCall{{SessionID: s.sessionID, Prompt: "once"}})
	require.Equal(t, "once", s.steer())
	sa.Interrupt(s.sessionID)
	acknowledged := s.takeSteered("once")
	pending, _ := sa.interrupting.Get(s.sessionID)
	sa.ClearQueue(s.sessionID)
	sa.activeRequests.Del(s.sessionID)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(s.sessionID) }, time.Second, time.Millisecond)
	require.Empty(t, acknowledged, "a late acknowledgement must not consume an interrupted prompt again")
	require.Len(t, pending, 1)
	require.Equal(t, "once", pending[0].Prompt)
}
