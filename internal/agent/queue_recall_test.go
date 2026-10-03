package agent

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestRecallQueuedPromptPreservesOtherRequestsAndAttachments(t *testing.T) {
	sa, _, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sa.activeRequests.Set("s1", &activeCancel{cancel: cancel})
	attachment := message.Attachment{FileName: "image.png", MimeType: "image/png", Content: []byte{1, 2, 3}}
	sa.messageQueue.Set("s1", []SessionAgentCall{
		{SessionID: "s1", RunID: "first", Prompt: "first"},
		{SessionID: "s1", RunID: "last", Prompt: "last", Attachments: []message.Attachment{attachment}},
	})
	sa.messageQueue.Set("other", []SessionAgentCall{{SessionID: "other", Prompt: "other"}})

	prompt := sa.RecallQueuedPrompt("s1")
	require.Equal(t, &message.QueuedPrompt{Prompt: "last", Attachments: []message.Attachment{attachment}}, prompt)
	require.NoError(t, ctx.Err(), "recall must not stop the active turn")
	require.Equal(t, []string{"first"}, sa.QueuedPromptsList("s1"))
	require.Equal(t, []string{"other"}, sa.QueuedPromptsList("other"))
	select {
	case event := <-events:
		require.Equal(t, "last", event.Payload.RunID)
		require.True(t, event.Payload.Cancelled)
	case <-time.After(time.Second):
		t.Fatal("the recalled request's completion waiter was not released")
	}
}

func TestRecallQueuedPromptLeavesSteeredMessagesWithCLI(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	s := &cliSteps{running: toolRunningForSteer(), a: sa, ctx: t.Context(), sessionID: "s1"}
	sa.steering.Set("s1", s)
	sa.messageQueue.Set("s1", []SessionAgentCall{{SessionID: "s1", Prompt: "already handed off"}})
	require.Equal(t, "already handed off", s.steer())
	require.Nil(t, sa.RecallQueuedPrompt("s1"))
	require.Equal(t, []string{"already handed off"}, sa.QueuedPromptsList("s1"))
	sa.messageQueue.Set("s1", []SessionAgentCall{{SessionID: "s1", Prompt: "still queued"}})
	require.Equal(t, "still queued", sa.RecallQueuedPrompt("s1").Prompt)
	require.Equal(t, []string{"already handed off"}, sa.QueuedPromptsList("s1"))
	require.Len(t, s.takeSteered("already handed off"), 1)
}

func TestRecallQueuedPromptDuringInterrupt(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	sa.interrupting.Set("s1", []SessionAgentCall{{SessionID: "s1", Prompt: "first"}, {SessionID: "s1", Prompt: "last"}})
	require.Equal(t, "last", sa.RecallQueuedPrompt("s1").Prompt)
	require.Equal(t, []string{"first"}, sa.QueuedPromptsList("s1"))
	require.Equal(t, "first", sa.RecallQueuedPrompt("s1").Prompt)
	require.Nil(t, sa.RecallQueuedPrompt("s1"))
	require.False(t, sa.IsSessionBusy("s1"))
}

func TestRecallQueuedPromptLeavesInternalRequestsAlone(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	sa.messageQueue.Set("s1", []SessionAgentCall{
		{Prompt: "user"},
		{Prompt: "hidden", HiddenUserMessage: true},
		{Prompt: "continuation", CLIContinue: true},
		{Prompt: "channel", Channel: "remote"},
		{Prompt: "summary continuation", NotRecallable: true},
	})
	require.Equal(t, "user", sa.RecallQueuedPrompt("s1").Prompt)
	require.Nil(t, sa.RecallQueuedPrompt("s1"))
	require.Equal(t, []string{"hidden", "continuation", "channel", "summary continuation"}, sa.QueuedPromptsList("s1"))
}

func TestRecallQueuedPromptCompletesHeadlessCallWithoutRunID(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	run := newSessionRun(t.Context(), "s1")
	defer run.cancel()
	require.True(t, run.reserve())
	completed := make(chan notify.RunComplete, 1)
	sa.messageQueue.Set("s1", []SessionAgentCall{{
		SessionID: "s1", Prompt: "queued", sessionRun: run,
		OnComplete:              func(complete notify.RunComplete) { completed <- complete; run.record(complete) },
		queuedSessionRunRelease: run.release,
	}})
	run.release(nil)
	require.NotNil(t, sa.RecallQueuedPrompt("s1"))
	select {
	case <-run.done:
	case <-time.After(time.Second):
		t.Fatal("recall leaked the queued lifetime reservation")
	}
	select {
	case complete := <-completed:
		require.True(t, complete.Cancelled)
	default:
		t.Fatal("recall skipped the retained completion hook")
	}
}
