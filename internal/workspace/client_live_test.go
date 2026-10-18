package workspace_test

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestClientWorkspaceTranslatesHelperEvent: in client mode, a task event in
// the engine reaches the app's UI loop as the agent's task event again, with
// every field, so the terminal's task row works (engine-design.md 5.3).
func TestClientWorkspaceTranslatesHelperEvent(t *testing.T) {
	xdgIsolate(t)
	rt := newRuntimeServer(t)
	cwd := t.TempDir()
	c := rt.newClient(t, cwd)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	wsProto, err := c.CreateWorkspace(ctx, prototest.CreateRequest(proto.Workspace{Path: cwd, DataDir: t.TempDir()}))
	require.NoError(t, err)
	w := workspace.NewClientWorkspace(c, *wsProto)
	evc, err := c.SubscribeEvents(ctx, wsProto.ID)
	require.NoError(t, err)
	tasks := make(chan pubsub.Event[agent.Task], 8)
	go w.ConsumeEventsForTest(evc, func(msg tea.Msg) {
		if ev, ok := msg.(pubsub.Event[agent.Task]); ok {
			tasks <- ev
		}
	})
	engine, err := rt.srv.Backend().GetWorkspace(wsProto.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return backend.WorkspaceLiveStreamCountForTest(engine) >= 1 },
		5*time.Second, 5*time.Millisecond, "the app's stream is attached")

	started := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	task := agent.Task{
		ID: "t7", SessionID: "chat-1", ChildID: "child-1", Name: "worker", CLI: "codex", Model: "gpt-5",
		Effort: "high", Fast: true, ReadOnly: true, Status: agent.TaskStopped,
		Started: started, Ended: started.Add(time.Minute), Delivered: true, StartedBy: c.ClientID(),
	}
	engine.SendEvent(pubsub.Event[agent.Task]{Type: pubsub.UpdatedEvent, Payload: task})

	select {
	case got := <-tasks:
		require.Equal(t, pubsub.UpdatedEvent, got.Type)
		require.True(t, got.Payload.Started.Equal(task.Started))
		require.True(t, got.Payload.Ended.Equal(task.Ended))
		got.Payload.Started, got.Payload.Ended = task.Started, task.Ended
		require.Equal(t, task, got.Payload)
	case <-time.After(5 * time.Second):
		t.Fatal("the helper event did not reach the UI loop as a task event")
	}
}

// TestClientWorkspaceAgentBackgroundUsesRoute: in client mode, Ctrl+B goes
// through the engine's route and signals only the chat's shell; a failed
// call reports false (engine-design.md 5.4).
func TestClientWorkspaceAgentBackgroundUsesRoute(t *testing.T) {
	xdgIsolate(t)
	rt := newRuntimeServer(t)
	cwd := t.TempDir()
	c := rt.newClient(t, cwd)
	wsProto, err := c.CreateWorkspace(t.Context(), prototest.CreateRequest(proto.Workspace{Path: cwd, DataDir: t.TempDir()}))
	require.NoError(t, err)
	w := workspace.NewClientWorkspace(c, *wsProto)

	chatA, chatB := "chat-a-"+uuid.NewString(), "chat-b-"+uuid.NewString()
	signal := make(chan struct{}, 1)
	t.Cleanup(tools.RegisterShellBackground(chatA, "marker-"+uuid.NewString(), signal))

	require.False(t, w.AgentBackground(chatB), "chat B has no running command")
	select {
	case <-signal:
		t.Fatal("chat A's shell was signalled for chat B")
	case <-time.After(50 * time.Millisecond):
	}

	require.True(t, w.AgentBackground(chatA), "Ctrl+B moves chat A's command in client mode")
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("chat A's shell was not signalled")
	}

	gone := workspace.NewClientWorkspace(c, proto.Workspace{ID: uuid.NewString()})
	require.False(t, gone.AgentBackground(chatA), "a failed call reports false")
	select {
	case <-signal:
		t.Fatal("a failed call signalled the shell")
	case <-time.After(50 * time.Millisecond):
	}
}
