package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonInteractiveInitKeepsInteractiveCoordinator: a non-interactive
// InitAgent on a workspace whose interactive coordinator runs keeps that
// coordinator. A desktop turn with a helper, and a prompt queued behind it,
// stay on it: the turn runs on, the queued prompt runs next, and a new
// helper lands in the same helper hub.
func TestNonInteractiveInitKeepsInteractiveCoordinator(t *testing.T) {
	r := newLiveRig(t)
	a := newClientID(t)
	ws := r.open(t, r.project(t), a)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	helper := r.spawnHelper(t, ws, a, c, "a1")
	r.send(t, ws, a, c, "a2", "live:hold:a2")
	requireQueued(t, ws, c, "a2")
	before := ws.coordinator()
	turnCalls, helperCalls := r.model.callsOf("a1"), r.model.callsOf(helper.Name)

	require.NoError(t, r.b.InitAgent(context.Background(), ws.ID, "", false))

	require.True(t, ws.coordinator() == before, "the interactive coordinator stays")
	require.True(t, ws.coordinator().IsSessionBusy(c), "the turn runs on it")
	require.Equal(t, 1, r.model.heldNow("a1"))
	require.Equal(t, turnCalls, r.model.callsOf("a1"), "and never restarts")
	requireQueued(t, ws, c, "a2")
	task, ok := sessionTask(c, helper.Name)
	require.True(t, ok)
	require.Equal(t, agent.TaskRunning, task.Status)
	require.Equal(t, 1, r.model.heldNow(helper.Name))
	require.Equal(t, helperCalls, r.model.callsOf(helper.Name))

	// The same helper hub: a new helper takes the hub's next task ID, not a
	// second hub's first one.
	next := r.spawnHelper(t, ws, a, d, "d1")
	require.NotEqual(t, helper.ID, next.ID)

	// And the queue drains on it.
	require.NoError(t, r.b.Interrupt(ws.ID, c))
	r.model.waitHeld(t, "a2", 1)
}

// TestNonInteractiveStartHasNoQuestionTool: a prompt from an app that
// started its agent non-interactively, as `crush run` does on a server
// (cmd/run.go runOnServer), gets no interactive-only question tool, which
// only an interactive coordinator offers (agent/toolset.go builtinTools);
// nothing would answer it. An interactive app's prompt in the same
// workspace keeps the tool, and its turn runs on.
func TestNonInteractiveStartHasNoQuestionTool(t *testing.T) {
	r := newLiveRig(t)
	desktop, run := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, desktop)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	r.hold(t, ws, desktop, c, "i1")
	require.Contains(t, r.model.toolsOf("i1"), tools.QuestionToolName, "an interactive app's turn offers it")

	r.joinNonInteractive(t, ws, p, run)
	r.hold(t, ws, run, d, "n1")
	require.NotContains(t, r.model.toolsOf("n1"), tools.QuestionToolName, "a non-interactive start's turn does not")
	require.Equal(t, 1, r.model.heldNow("i1"), "the interactive turn runs on")
}

// TestInteractivePromptKeepsQuestionTool: only a non-interactive start's
// own prompts lose the question tool. An interactive app's prompt sent
// while one of them runs, in another chat on the same coordinator, keeps
// it.
func TestInteractivePromptKeepsQuestionTool(t *testing.T) {
	r := newLiveRig(t)
	desktop, run := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, desktop)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	r.joinNonInteractive(t, ws, p, run)
	r.hold(t, ws, run, d, "n1")

	r.hold(t, ws, desktop, c, "i1")
	require.Contains(t, r.model.toolsOf("i1"), tools.QuestionToolName)
}

// TestNonInteractivePromptWaitsForHelper: a non-interactive start's prompt
// ends only after its helper, as on a non-interactive coordinator: its
// RunComplete, which `crush run` waits for, comes after the helper's result
// turn, with that turn's answer. And, as for `crush run` without a server,
// the helper is not saved for a later Crush to resume.
func TestNonInteractivePromptWaitsForHelper(t *testing.T) {
	r := newLiveRig(t)
	desktop, run := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, desktop)
	d := r.chat(t, ws, "D")
	r.joinNonInteractive(t, ws, p, run)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completions := ws.RunCompletions().Subscribe(ctx)

	require.NoError(t, r.b.SendMessage(ws.ID, run, proto.AgentMessage{
		SessionID: d, SubmissionID: "n1", RunID: "run-n1", Prompt: "live:start:n1",
	}))
	r.model.waitHeld(t, "n1-helper", 1)
	helper, ok := sessionTask(d, "n1-helper")
	require.True(t, ok)
	require.Eventually(t, func() bool { return !ws.coordinator().IsSessionBusy(d) }, 20*time.Second, 10*time.Millisecond,
		"the turn ends while its helper runs")
	saved, _ := os.ReadFile(filepath.Join(ws.Cfg.Config().Options.DataDirectory, "tasks.json"))
	assert.NotContains(t, string(saved), helper.ChildID, "the helper is not saved for a resume")

	require.NoError(t, agent.StopSessionTask(d, helper.ID))
	var got notify.RunComplete
	timeout := time.After(20 * time.Second)
	for got.RunID != "run-n1" {
		select {
		case ev := <-completions:
			got = ev.Payload
		case <-timeout:
			t.Fatal("no RunComplete for the prompt")
		}
	}
	require.Equal(t, "Noted.", got.Text, "the run ends with its helper's result turn, not before it")
}

// TestDesktopPromptDuringRunRunsOnItsOwn: a desktop prompt sent to a chat
// whose non-interactive prompt's run is still open (its turn ended, its
// helper runs) does not join that run. It runs as its own interactive turn,
// with the question tool; when the `crush run` app leaves, its run ends
// with its own answer while the desktop's turn runs on; and the desktop's
// turn ends with its own RunComplete.
func TestDesktopPromptDuringRunRunsOnItsOwn(t *testing.T) {
	r := newLiveRig(t)
	desktop, run := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, desktop)
	d := r.chat(t, ws, "D")
	r.joinNonInteractive(t, ws, p, run)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completions := ws.RunCompletions().Subscribe(ctx)

	require.NoError(t, r.b.SendMessage(ws.ID, run, proto.AgentMessage{
		SessionID: d, SubmissionID: "n1", RunID: "run-n1", Prompt: "live:start:n1",
	}))
	r.model.waitHeld(t, "n1-helper", 1)
	require.Eventually(t, func() bool { return !ws.coordinator().IsSessionBusy(d) }, 20*time.Second, 10*time.Millisecond,
		"the run's turn ends while its helper runs")

	require.NoError(t, r.b.SendMessage(ws.ID, desktop, proto.AgentMessage{
		SessionID: d, SubmissionID: "i1", RunID: "run-i1", Prompt: "live:hold:i1",
	}))
	r.model.waitHeld(t, "i1", 1)
	assert.Contains(t, r.model.toolsOf("i1"), tools.QuestionToolName, "the desktop's turn is interactive")

	// Q2 stops the leaving app's helper, which ends its run.
	require.NoError(t, r.b.RetireClient(run))
	got := waitRunComplete(t, completions, "run-n1")
	require.Equal(t, "Started.", got.Text, "the run's answer is its own")
	require.Equal(t, 1, r.model.heldNow("i1"), "the desktop's turn runs on")

	require.NoError(t, r.b.CancelSession(ws.ID, d))
	got = waitRunComplete(t, completions, "run-i1")
	require.Equal(t, "i1", got.SubmissionID, "the desktop's turn ends with its own RunComplete")
}

// waitRunComplete waits for the RunComplete of runID.
func waitRunComplete(t *testing.T, completions <-chan pubsub.Event[notify.RunComplete], runID string) notify.RunComplete {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case ev := <-completions:
			if ev.Payload.RunID == runID {
				return ev.Payload
			}
		case <-timeout:
			t.Fatalf("no RunComplete for %s", runID)
			return notify.RunComplete{}
		}
	}
}

// joinNonInteractive joins p's workspace as app and starts the agent
// non-interactively, as `crush run` does on a server (cmd/run.go
// runOnServer).
func (r *liveRig) joinNonInteractive(t *testing.T, ws *Workspace, p liveProject, app string) {
	t.Helper()
	joined, _, err := r.b.CreateWorkspace(prototest.CreateRequest(proto.Workspace{
		Path: p.dir, DataDir: p.dataDir, ClientID: app, YOLO: true,
	}))
	require.NoError(t, err)
	require.Same(t, ws, joined)
	require.NoError(t, r.b.InitAgent(context.Background(), ws.ID, app, false))
}
