package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
)

func TestTaskIDsOutliveCrushForFollowUps(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	done := Task{ID: "t7", SessionID: "parent", ChildID: "child-7", Name: "Author", CLI: "claude", Model: "opus", Status: TaskRunning, Started: time.Now()}
	h.remember(done, "claude")
	h.forget(done.ChildID)

	// A later Crush in the same project starts with no tasks in memory.
	later := &taskHub{c: h.c, tasks: map[string]*Task{}, cancels: map[string]context.CancelFunc{}, userStopped: map[string]bool{}}
	found, ok := later.findTask("parent", "t7")
	require.True(t, ok)
	require.Equal(t, "child-7", found.ChildID)
	_, ok = later.findTask("other", "t7")
	require.False(t, ok, "tasks belong to their own session")

	later.skipUsedIDs("parent")
	require.Equal(t, 7, later.seq, "new tasks must not reuse an earlier task's ID")
}

func TestContinueRefusesRunningAndUnknownTasks(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	h.tasks["t1"] = &Task{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: TaskRunning}
	_, err := h.continueTask(TaskRequest{Session: "parent", Continue: "t1", Prompt: "more"})
	require.ErrorContains(t, err, "still running")
	_, err = h.continueTask(TaskRequest{Session: "parent", Continue: "t9", Prompt: "more"})
	require.ErrorContains(t, err, "no task")
	_, err = h.continueTask(TaskRequest{Session: "parent", Continue: "t1", Prompt: " "})
	require.ErrorContains(t, err, "empty")
}

// A stopped or finished helper can carry on with another model or tool:
// the follow-up runs there, on the same chat, and the chat and task list
// show the new model (owner, 2026-10-27).
func TestContinueCanSwitchModel(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	c.tasks = h
	claude := config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus", ReasoningLevels: []string{"high", "max"}}, {ID: "sonnet"}}}
	codex := config.ProviderConfig{ID: "codex", Type: config.TypeCodexCLI, Models: []catwalk.Model{{ID: "gpt-6.1-sol"}}}
	c.cfg.Config().Providers.Set(claude.ID, claude)
	c.cfg.Config().Providers.Set(codex.ID, codex)
	parent, _ := helperChats(t, c)
	ran := make(chan string, 4)
	h.subAgentHook = func(_ config.ProviderConfig, m catwalk.Model) SessionAgent {
		return newMockAgent("claude", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			ran <- m.ID
			return agentResultWithText("done"), nil
		})
	}
	ranOn := func(want string) {
		t.Helper()
		select {
		case got := <-ran:
			require.Equal(t, want, got)
		case <-time.After(5 * time.Second):
			t.Fatalf("nothing ran on %s", want)
		}
	}
	idle := func(id string) {
		t.Helper()
		require.Eventually(t, func() bool { return !h.childRunning(id) }, 5*time.Second, 10*time.Millisecond)
	}

	task, err := h.spawn(TaskRequest{Session: parent.ID, CLI: "claude", Model: "opus", Effort: "max", Prompt: "work"})
	require.NoError(t, err)
	ranOn("opus")
	idle(task.ChildID)

	next, err := h.continueTask(TaskRequest{Session: parent.ID, Continue: task.ID, Model: "sonnet", Prompt: "more"})
	require.NoError(t, err)
	ranOn("sonnet")
	require.Equal(t, task.ChildID, next.ChildID, "it keeps its own chat")
	require.Equal(t, "sonnet", next.Model)
	require.Empty(t, next.Effort, "the old model's effort is not carried to a new model")
	require.Equal(t, "sonnet", chatModelOf(t, c, task.ChildID).Model)
	idle(task.ChildID)

	next, err = h.continueTask(TaskRequest{Session: parent.ID, Continue: task.ID, CLI: "codex", Prompt: "more"})
	require.NoError(t, err)
	ranOn("gpt-6.1-sol")
	require.Equal(t, "codex", next.CLI)
	require.Equal(t, "codex", chatModelOf(t, c, task.ChildID).Provider)
	idle(task.ChildID)
	found, ok := h.findTask(parent.ID, task.ID)
	require.True(t, ok)
	require.Equal(t, "gpt-6.1-sol", found.Model, "the task list shows the new model")

	_, err = h.continueTask(TaskRequest{Session: parent.ID, Continue: task.ID, Model: "nope", Prompt: "more"})
	require.ErrorContains(t, err, "unknown codex model")
}

func TestSwitchingModelRefusesRunningTask(t *testing.T) {
	t.Parallel()
	h, _ := savingHub(t, true)
	h.tasks["t1"] = &Task{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: TaskRunning}
	_, _, err := h.messageRunning(TaskRequest{Session: "parent", Continue: "t1", Model: "sonnet", Prompt: "more"})
	require.ErrorContains(t, err, "stop it first")
}
