package agent

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestQueueHandoffRetainsAcceptedFollowUpBeforeDispatch(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		name := "natural"
		if interrupt {
			name = "interrupt"
		}
		t.Run(name, func(t *testing.T) {
			env := testEnv(t)
			model := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
			sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			broker := pubsub.NewBroker[notify.RunComplete]()
			t.Cleanup(broker.Shutdown)
			sa.runComplete = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "delayed acceptance")
			require.NoError(t, err)
			old := &activeCancel{cancel: func() {}}
			sa.activeRequests.Set(sess.ID, old)
			accepted := sa.BeginAccepted(sess.ID)
			defer accepted.Close()
			owner := newSessionRun(t.Context(), sess.ID)
			defer owner.cancel()
			attachment := message.Attachment{MimeType: "text/plain", FileName: "first.txt", Content: []byte("accepted attachment")}
			call := SessionAgentCall{SessionID: sess.ID, Prompt: "accepted first", SubmissionID: "accepted-id", RunID: "accepted-run", Accepted: accepted, Attachments: []message.Attachment{attachment}, sessionRun: owner,
				OnComplete: func(complete notify.RunComplete) {
					owner.record(complete)
					broker.Publish(pubsub.UpdatedEvent, complete)
				}}
			_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "queued second", SubmissionID: "queued-id", RunID: "queued-run"})
			require.NoError(t, err)
			if interrupt {
				sa.Interrupt(sess.ID)
				sa.Interrupt(sess.ID) // pending accepts and queue belong to one transition
			}
			sa.finishDispatch(t.Context(), sess.ID, old)
			require.True(t, sa.IsSessionBusy(sess.ID), "the undelivered acceptance keeps the snapshot busy")
			require.Empty(t, model.snapshot(), "the batch must wait for its accepted sibling")
			_, err = sa.Run(t.Context(), call)
			require.NoError(t, err)
			owner.release(nil)
			batchWaitTurn(t, model, 1)
			require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
			require.Len(t, model.snapshot(), 1)
			require.Equal(t, []string{message.PromptWithTextAttachments(call.Prompt, call.Attachments), "queued second"}, batchUserTexts(model.snapshot()[0]))
			msgs, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			require.Len(t, msgs, 3)
			require.Equal(t, "accepted-id", msgs[0].Content().SubmissionID)
			require.Equal(t, attachment.Content, msgs[0].BinaryContent()[0].Data)
			require.Equal(t, "queued-id", msgs[1].Content().SubmissionID)
			for _, id := range []string{"accepted", "queued"} {
				select {
				case event := <-events:
					require.Equal(t, id+"-id", event.Payload.SubmissionID)
					require.Equal(t, id+"-run", event.Payload.RunID)
					require.False(t, event.Payload.Cancelled)
					require.Equal(t, "done", event.Payload.Text)
				case <-time.After(time.Second):
					t.Fatal("accepted follow-up lost its terminal event")
				}
			}
			select {
			case <-owner.done:
			case <-time.After(time.Second):
				t.Fatal("accepted headless follow-up leaked a reservation")
			}
			select {
			case event := <-events:
				t.Fatalf("duplicate completion: %+v", event.Payload)
			default:
			}
			require.Zero(t, sa.acceptedCount(sess.ID))
		})
	}
}

func TestQueueHandoffInterruptRepeatsCannotCancelReservedReplacement(t *testing.T) {
	env := testEnv(t)
	model := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "repeat during reservation")
	require.NoError(t, err)
	var cancellations atomic.Int32
	old := &activeCancel{cancel: func() { cancellations.Add(1) }}
	sa.activeRequests.Set(sess.ID, old)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "replacement", SubmissionID: "replacement-id", RunID: "replacement-run"})
	require.NoError(t, err)
	// Hold the dispatcher between reserving the replacement and entering Run.
	// A canceled headless sibling's terminal callback is a real blocking point.
	owner := newSessionRun(t.Context(), sess.ID)
	defer owner.cancel()
	owner.cancel()
	publishing := make(chan struct{})
	resume := make(chan struct{})
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	queued, _ := sa.messageQueue.Get(sess.ID)
	sa.messageQueue.Set(sess.ID, append(queued, SessionAgentCall{
		SessionID: sess.ID, Prompt: "canceled owner", sessionRun: owner, RunID: "canceled",
		OnComplete: func(complete notify.RunComplete) {
			require.True(t, complete.Cancelled)
			close(publishing)
			<-resume
		},
	}))
	sa.Interrupt(sess.ID)
	for range 3 {
		sa.Interrupt(sess.ID)
	}
	require.EqualValues(t, 1, cancellations.Load())
	sa.finishDispatch(t.Context(), sess.ID, old)
	select {
	case <-publishing:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not reserve replacement")
	}
	require.Empty(t, model.snapshot())
	for range 3 {
		sa.Interrupt(sess.ID)
	}
	require.False(t, sa.hasPendingCancel(sess.ID), "repeat must not mark replacement's lease canceled")
	close(resume)
	batchWaitTurn(t, model, 1)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
	require.Len(t, model.snapshot(), 1)
	require.Equal(t, []string{"replacement"}, batchUserTexts(model.snapshot()[0]))
	select {
	case event := <-events:
		require.Equal(t, "replacement-id", event.Payload.SubmissionID)
		require.Equal(t, "replacement-run", event.Payload.RunID)
		require.False(t, event.Payload.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("replacement lost its completion")
	}
	select {
	case event := <-events:
		t.Fatalf("duplicate completion: %+v", event.Payload)
	default:
	}
}

func TestQueueHandoffAcceptanceCloseAndValidationFailureReleaseBarrier(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "validation failure"}[invalid], func(t *testing.T) {
			env := testEnv(t)
			model := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
			sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			sess, err := env.sessions.Create(t.Context(), "released acceptance")
			require.NoError(t, err)
			old := &activeCancel{cancel: func() {}}
			sa.activeRequests.Set(sess.ID, old)
			accepted := sa.BeginAccepted(sess.ID)
			defer accepted.Close()
			_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "survivor"})
			require.NoError(t, err)
			sa.Interrupt(sess.ID)
			sa.finishDispatch(t.Context(), sess.ID, old)
			if invalid {
				_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Accepted: accepted})
				require.ErrorIs(t, err, ErrEmptyPrompt)
			} else {
				accepted.Close()
			}
			batchWaitTurn(t, model, 1)
			require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
			require.Equal(t, []string{"survivor"}, batchUserTexts(model.snapshot()[0]))
			require.Zero(t, sa.acceptedCount(sess.ID))
		})
	}
}

func TestQueueHandoffInterruptStillCancelsUnstartedPrimary(t *testing.T) {
	sa, env, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "primary")
	require.NoError(t, err)
	accepted := sa.BeginAccepted(sess.ID)
	defer accepted.Close()
	sa.Interrupt(sess.ID)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "primary", Accepted: accepted, SubmissionID: "primary-id"})
	require.NoError(t, err)
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, message.FinishReasonCanceled, msgs[1].FinishReason())
	select {
	case event := <-events:
		require.Equal(t, "primary-id", event.Payload.SubmissionID)
		require.True(t, event.Payload.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("unstarted primary lost cancellation")
	}
}

func TestQueueHandoffCancelAlsoCancelsUndispatchedFollowUp(t *testing.T) {
	sa, env, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "explicit cancel")
	require.NoError(t, err)
	old := &activeCancel{cancel: func() {}}
	sa.activeRequests.Set(sess.ID, old)
	accepted := sa.BeginAccepted(sess.ID)
	defer accepted.Close()
	sa.Interrupt(sess.ID) // no queued call yet: the lease itself owns the snapshot
	sa.Cancel(sess.ID)
	sa.finishDispatch(t.Context(), sess.ID, old)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, time.Second, time.Millisecond,
		"explicit cancel must not wait for a canceled lease to dispatch")
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "late dispatch", Accepted: accepted, SubmissionID: "late-id"})
	require.NoError(t, err)
	select {
	case event := <-events:
		require.Equal(t, "late-id", event.Payload.SubmissionID)
		require.True(t, event.Payload.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("explicit cancel lost the accepted follow-up's terminal")
	}
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, message.FinishReasonCanceled, msgs[1].FinishReason())
}

func TestQueueHandoffNewPrimaryAfterCancelIsNotAFollowUp(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	old := sa.BeginAccepted("session")
	defer old.Close()
	sa.Cancel("session")
	fresh := sa.BeginAccepted("session")
	defer fresh.Close()
	require.False(t, fresh.followUp, "a canceled predecessor does not own a new primary")
	sa.Interrupt("session")
	require.True(t, fresh.canceled.Load(), "an unstarted primary is still canceled by Interrupt")
}

func TestQueueHandoffRecallUsesAcceptanceOrderAndLeavesLaterArrivals(t *testing.T) {
	env := testEnv(t)
	model := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{})}}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "recall acceptance")
	require.NoError(t, err)
	old := &activeCancel{cancel: func() {}}
	sa.activeRequests.Set(sess.ID, old)
	first := sa.BeginAccepted(sess.ID)
	defer first.Close()
	second := sa.BeginAccepted(sess.ID)
	defer second.Close()
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "newest in snapshot", Accepted: second, SubmissionID: "recall-id"})
	require.NoError(t, err)
	sa.Interrupt(sess.ID)
	later := sa.BeginAccepted(sess.ID)
	defer later.Close()
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "later arrival", Accepted: later, SubmissionID: "later-id"})
	require.NoError(t, err)
	// The later queue is recalled first, even while the older snapshot waits.
	require.Equal(t, "later-id", sa.RecallQueuedPrompt(sess.ID).SubmissionID)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "first accepted", Accepted: first, SubmissionID: "first-id"})
	require.NoError(t, err)
	require.Equal(t, []string{"first accepted", "newest in snapshot"}, sa.QueuedPromptsList(sess.ID))
	require.Equal(t, "recall-id", sa.RecallQueuedPrompt(sess.ID).SubmissionID,
		"an older acceptance dispatching late must not become the newest recallable prompt")
	// A new post-snapshot input is kept for its own drain.
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "next batch", SubmissionID: "next-id"})
	require.NoError(t, err)
	sa.finishDispatch(t.Context(), sess.ID, old)
	batchWaitTurn(t, model, 1)
	require.Equal(t, []string{"first accepted"}, batchUserTexts(model.snapshot()[0]))
	require.Equal(t, []string{"next batch"}, sa.QueuedPromptsList(sess.ID))
	close(model.gates[1])
	batchWaitTurn(t, model, 2)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
	require.Len(t, model.snapshot(), 2)
	seen := map[string]bool{}
	for range 4 {
		select {
		case event := <-events:
			id := event.Payload.SubmissionID
			require.NotContains(t, seen, id)
			seen[id] = true
			require.Equal(t, id == "later-id" || id == "recall-id", event.Payload.Cancelled)
		case <-time.After(time.Second):
			t.Fatal("recall or queued dispatch lost a completion")
		}
	}
	require.Equal(t, map[string]bool{"later-id": true, "recall-id": true, "first-id": true, "next-id": true}, seen)
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 4, "recalled prompts must never persist")
}
