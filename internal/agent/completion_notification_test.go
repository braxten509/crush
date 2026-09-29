package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

type completionGate struct {
	entered chan struct{}
	release chan struct{}
}

type completionGatedModel struct {
	finishStreamModel
	gates []completionGate
	calls atomic.Int32
}

func (m *completionGatedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	index := int(m.calls.Add(1)) - 1
	if index < len(m.gates) {
		gate := m.gates[index]
		close(gate.entered)
		select {
		case <-gate.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.finishStreamModel.Stream(ctx, call)
}

type completionFlushMessages struct {
	message.Service
	gate completionGate
}

func (m *completionFlushMessages) FlushAll(ctx context.Context) error {
	close(m.gate.entered)
	select {
	case <-m.gate.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return m.Service.FlushAll(ctx)
}

func waitCompletionGate(t *testing.T, gate completionGate) {
	t.Helper()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent never reached completion gate")
	}
}

func requireNoCompletionNotification(t *testing.T, events <-chan pubsub.Event[notify.Notification]) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("completion notification arrived while work remained: %+v", event.Payload)
	default:
	}
}

func requireCompletionNotification(t *testing.T, events <-chan pubsub.Event[notify.Notification], sessionID string) {
	t.Helper()
	select {
	case event := <-events:
		require.Equal(t, notify.TypeAgentFinished, event.Payload.Type)
		require.Equal(t, sessionID, event.Payload.SessionID)
	case <-time.After(5 * time.Second):
		t.Fatal("no completion notification after the agent finished")
	}
	requireNoCompletionNotification(t, events)
}

func TestCompletionNotificationWaitsForQueuedTurn(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	model := &completionGatedModel{finishStreamModel: finishStreamModel{text: "done"}}
	for range 2 {
		model.gates = append(model.gates, completionGate{make(chan struct{}), make(chan struct{})})
	}
	// Always release the gates, including when an assertion fails.
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
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "follow-up", RunID: "follow-up"})
	require.NoError(t, err)
	close(model.gates[0].release)
	waitCompletionGate(t, model.gates[1])
	requireNoCompletionNotification(t, events)
	close(model.gates[1].release)
	require.NoError(t, <-done)
	requireCompletionNotification(t, events, sess.ID)
}

func TestCompletionNotificationWaitsForFinalFlush(t *testing.T) {
	t.Parallel()
	agent, env := newStreamTestAgent(t)
	gate := completionGate{make(chan struct{}), make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-gate.release:
		default:
			close(gate.release)
		}
	})
	agent.messages = &completionFlushMessages{Service: agent.messages, gate: gate}
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
	waitCompletionGate(t, gate)
	requireNoCompletionNotification(t, events)
	close(gate.release)
	require.NoError(t, <-done)
	requireCompletionNotification(t, events, sess.ID)
}

func TestCompletionNotificationWaitsForAcceptedTurn(t *testing.T) {
	t.Parallel()
	agent, env := newStreamTestAgent(t)
	broker := pubsub.NewBroker[notify.Notification]()
	t.Cleanup(broker.Shutdown)
	agent.notify = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	var accepted *AcceptedRun
	_, err = agent.Run(t.Context(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "main",
		OnComplete: func(notify.RunComplete) {
			accepted = agent.BeginAccepted(sess.ID)
		},
	})
	require.NoError(t, err)
	require.NotNil(t, accepted)
	defer accepted.Close()
	requireNoCompletionNotification(t, events)
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "follow-up", Accepted: accepted})
	require.NoError(t, err)
	requireCompletionNotification(t, events, sess.ID)
}
