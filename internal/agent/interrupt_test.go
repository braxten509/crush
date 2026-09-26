package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// TestInterrupt_CancelsActiveAndRunsQueued checks that Interrupt stops the
// active turn but keeps the queue, so the queued prompt runs next.
func TestInterrupt_CancelsActiveAndRunsQueued(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)

	large := &gatedStreamModel{text: "done", gate: make(chan struct{}), entered: make(chan struct{})}
	small := &finishStreamModel{text: "title"}
	sa := NewSessionAgent(SessionAgentOptions{
		LargeModel:  Model{Model: large, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		SmallModel:  Model{Model: small, CatwalkCfg: catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}},
		IsYolo:      true,
		Sessions:    env.sessions,
		Messages:    env.messages,
		RunComplete: broker,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	subCtx, subCancel := context.WithCancel(t.Context())
	defer subCancel()
	ch := broker.Subscribe(subCtx)

	go func() {
		_, _ = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, RunID: "run-main", Prompt: "main"})
	}()
	select {
	case <-large.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("main run never entered Stream")
	}

	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, RunID: "run-follow", Prompt: "follow"})
	require.NoError(t, err)
	require.Equal(t, 1, sa.QueuedPrompts(sess.ID))

	sa.Interrupt(sess.ID)

	got := map[string]notify.RunComplete{}
	deadline := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case ev := <-ch:
			got[ev.Payload.RunID] = ev.Payload
		case <-deadline:
			t.Fatalf("timed out waiting for both RunCompletes; got %v", got)
		}
	}
	require.True(t, got["run-main"].Cancelled, "the interrupted turn is reported canceled")
	require.False(t, got["run-follow"].Cancelled)
	require.Equal(t, "done", got["run-follow"].Text, "the queued prompt ran after the interrupt")
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, 10*time.Millisecond)
	require.Zero(t, sa.QueuedPrompts(sess.ID))
}
