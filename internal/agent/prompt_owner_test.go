package agent

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// ownedBy stands in for the backend's "sent by the leaving app": the
// leaving app's submission IDs start with prefix.
func ownedBy(prefix string) func(string) bool {
	return func(id string) bool { return strings.HasPrefix(id, prefix) }
}

func ownsAll([]string) bool { return true }

// recordingAgent is a model-backed agent whose run completions the test
// reads.
func recordingAgent(t *testing.T) (*sessionAgent, fakeEnv, *batchRecordingModel, <-chan pubsub.Event[notify.RunComplete]) {
	t.Helper()
	env := testEnv(t)
	model := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	return sa, env, model, broker.Subscribe(t.Context())
}

// waitCompletions collects RunComplete events by submission ID until every
// one of ids arrived.
func waitCompletions(t *testing.T, events <-chan pubsub.Event[notify.RunComplete], ids ...string) map[string]notify.RunComplete {
	t.Helper()
	got := map[string]notify.RunComplete{}
	deadline := time.After(5 * time.Second)
	for {
		missing := slices.ContainsFunc(ids, func(id string) bool { _, ok := got[id]; return !ok })
		if !missing {
			return got
		}
		select {
		case e := <-events:
			got[e.Payload.SubmissionID] = e.Payload
		case <-deadline:
			t.Fatalf("completions missing; have %v, want %v", got, ids)
		}
	}
}

// steeringTurn makes a running agent CLI turn in the chat, with the queued
// calls handed to the CLI as a mid-turn message it has not taken in yet.
func steeringTurn(t *testing.T, sa *sessionAgent, sessionID string, handed ...SessionAgentCall) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	s := &cliSteps{running: toolRunningForSteer(), a: sa, ctx: ctx, sessionID: sessionID}
	sa.steering.Set(sessionID, s)
	sa.activeRequests.Set(sessionID, &activeCancel{cancel: cancel})
	sa.messageQueue.Set(sessionID, handed)
	require.NotEmpty(t, s.steer())
	return ctx
}

// endTurn ends the simulated turn, as its run's cleanup would.
func endTurn(sa *sessionAgent, sessionID string) {
	sa.steering.Del(sessionID)
	sa.activeRequests.Del(sessionID)
}

func TestStopOwnedNeverCancels(t *testing.T) {
	t.Run("handed prompt alone", func(t *testing.T) {
		sa, env, broker := newCancelTestAgentWithRunComplete(t)
		events := broker.Subscribe(t.Context())
		sess, err := env.sessions.Create(t.Context(), "leaving app")
		require.NoError(t, err)
		turn := steeringTurn(t, sa, sess.ID, SessionAgentCall{SessionID: sess.ID, Prompt: "handed", SubmissionID: "l-handed"})

		var queued []string
		dropped := sa.StopOwned(sess.ID, ownedBy("l-"), func(ids []string) bool { queued = ids; return true })
		require.Equal(t, 1, dropped)
		require.Equal(t, []string{"l-handed"}, queued, "a handed prompt counts as waiting")
		require.Error(t, turn.Err(), "the leaving app's turn is interrupted")
		require.False(t, sa.hasPendingCancel(sess.ID), "never the cancel path")
		endTurn(sa, sess.ID)
		done := waitCompletions(t, events, "l-handed")
		require.True(t, done["l-handed"].Cancelled)
		require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond, "the chat ends idle")
		require.Empty(t, sa.QueuedPromptSummaries(sess.ID))
	})
	t.Run("another app's prompt runs next", func(t *testing.T) {
		sa, env, model, events := recordingAgent(t)
		sess, err := env.sessions.Create(t.Context(), "two apps")
		require.NoError(t, err)
		turn := steeringTurn(t, sa, sess.ID, SessionAgentCall{SessionID: sess.ID, Prompt: "handed", SubmissionID: "l-handed"})
		_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "from B", SubmissionID: "b-queued"})
		require.NoError(t, err)

		dropped := sa.StopOwned(sess.ID, ownedBy("l-"), ownsAll)
		require.Equal(t, 1, dropped)
		require.Error(t, turn.Err())
		endTurn(sa, sess.ID)
		batchWaitTurn(t, model, 1)
		require.Equal(t, []string{"from B"}, batchUserTexts(model.snapshot()[0]), "B's prompt runs next, without L's")
		done := waitCompletions(t, events, "l-handed", "b-queued")
		require.True(t, done["l-handed"].Cancelled)
		require.False(t, done["b-queued"].Cancelled)
		require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
		require.Len(t, model.snapshot(), 1)
	})
}

func TestStopOwnedKeepsAcceptedPromptOfOtherApp(t *testing.T) {
	sa, env, model, events := recordingAgent(t)
	sess, err := env.sessions.Create(t.Context(), "accepted")
	require.NoError(t, err)
	turn, cancelTurn := context.WithCancel(t.Context())
	defer cancelTurn()
	old := &activeCancel{cancel: cancelTurn}
	sa.activeRequests.Set(sess.ID, old)
	// B's prompt is accepted while L's turn runs; its goroutine has not
	// reached Run yet.
	accepted := sa.BeginAccepted(sess.ID)
	defer accepted.Close()

	dropped := sa.StopOwned(sess.ID, ownedBy("l-"), ownsAll)
	require.Zero(t, dropped)
	require.Error(t, turn.Err(), "L's turn is interrupted")
	require.False(t, accepted.canceled.Load())
	require.False(t, sa.hasPendingCancel(sess.ID))
	sa.finishDispatch(t.Context(), sess.ID, old)

	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "from B", SubmissionID: "b-accepted", Accepted: accepted})
	require.NoError(t, err)
	batchWaitTurn(t, model, 1)
	require.Equal(t, []string{"from B"}, batchUserTexts(model.snapshot()[0]))
	done := waitCompletions(t, events, "b-accepted")
	require.False(t, done["b-accepted"].Cancelled, "B's prompt is not cancelled on entry")
}

func TestStopOwnedLeavesUnscheduledPrimaryAlone(t *testing.T) {
	sa, env, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "primary")
	require.NoError(t, err)
	// As in TestQueueHandoffNewPrimaryAfterCancelIsNotAFollowUp: a cancelled
	// acceptance, then a fresh primary that has not reached the scheduler.
	old := sa.BeginAccepted(sess.ID)
	defer old.Close()
	sa.Cancel(sess.ID)
	fresh := sa.BeginAccepted(sess.ID)
	defer fresh.Close()
	require.False(t, fresh.followUp)
	turn, cancelTurn := context.WithCancel(t.Context())
	defer cancelTurn()
	sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancelTurn})
	defer sa.activeRequests.Del(sess.ID)
	sa.messageQueue.Set(sess.ID, []SessionAgentCall{
		{SessionID: sess.ID, Prompt: "leaving", SubmissionID: "l-1", queueOrder: 1},
		{SessionID: sess.ID, Prompt: "staying", SubmissionID: "b-1", queueOrder: 2},
	})

	dropped := sa.StopOwned(sess.ID, ownedBy("l-"), ownsAll)
	require.Equal(t, 1, dropped)
	require.False(t, fresh.canceled.Load(), "the unscheduled primary is not cancelled")
	require.NoError(t, turn.Err(), "the active turn keeps running")
	queued, _ := sa.messageQueue.Get(sess.ID)
	require.Len(t, queued, 1)
	require.Equal(t, "b-1", queued[0].SubmissionID)
	done := waitCompletions(t, events, "l-1")
	require.True(t, done["l-1"].Cancelled)
}

func TestStopOwnedDecidesUnderDispatchLock(t *testing.T) {
	sa, env, _ := newCancelTestAgentWithRunComplete(t)
	sess, err := env.sessions.Create(t.Context(), "lock")
	require.NoError(t, err)
	turn, cancelTurn := context.WithCancel(t.Context())
	defer cancelTurn()
	sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancelTurn})
	defer sa.activeRequests.Del(sess.ID)
	sa.messageQueue.Set(sess.ID, []SessionAgentCall{
		{SessionID: sess.ID, Prompt: "leaving", SubmissionID: "l-1", queueOrder: 1},
		{SessionID: sess.ID, Prompt: "staying", SubmissionID: "b-1", queueOrder: 2},
	})

	var locked bool
	var got []string
	dropped := sa.StopOwned(sess.ID, ownedBy("l-"), func(queued []string) bool {
		mu := sa.sessionMu(sess.ID)
		if mu.TryLock() {
			mu.Unlock()
		} else {
			locked = true
		}
		got = slices.Clone(queued)
		return false
	})
	require.True(t, locked, "ownsTurn runs under the chat's dispatch mutex")
	require.Equal(t, []string{"l-1", "b-1"}, got, "with the prompts queued at that moment")
	require.Equal(t, 1, dropped)
	require.NoError(t, turn.Err(), "a turn that is not the leaving app's keeps running")
	queued, _ := sa.messageQueue.Get(sess.ID)
	require.Len(t, queued, 1)
	require.Equal(t, "b-1", queued[0].SubmissionID)
}
