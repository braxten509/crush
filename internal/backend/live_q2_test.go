package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Ownership (Q2, engine-design.md 5.5), on real turns, helpers and jobs of
// the fake model.

// completions collects a workspace's RunComplete events by submission ID.
type completions struct {
	mu   sync.Mutex
	byID map[string][]notify.RunComplete
}

func watchRunCompletions(t *testing.T, ws *Workspace) *completions {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &completions{byID: map[string][]notify.RunComplete{}}
	events := ws.RunCompletions().Subscribe(ctx)
	go func() {
		for ev := range events {
			c.mu.Lock()
			c.byID[ev.Payload.SubmissionID] = append(c.byID[ev.Payload.SubmissionID], ev.Payload)
			c.mu.Unlock()
		}
	}()
	return c
}

func (c *completions) of(id string) []notify.RunComplete {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.byID[id])
}

// wait waits for the RunComplete of submission id.
func (c *completions) wait(t *testing.T, id string) notify.RunComplete {
	t.Helper()
	require.Eventually(t, func() bool { return len(c.of(id)) > 0 }, 20*time.Second, 10*time.Millisecond,
		"no RunComplete for %s", id)
	return c.of(id)[0]
}

// requireQueued waits until submission id waits in chat's queue.
func requireQueued(t *testing.T, ws *Workspace, chat, id string) {
	t.Helper()
	require.Eventually(t, func() bool { return slices.Contains(queuedIDs(ws.coordinator(), chat), id) },
		10*time.Second, 10*time.Millisecond, "%s must wait in the queue", id)
}

// savedWork is an entry of tasks.json or background-jobs.json.
type savedWork struct {
	ID          string    `json:"id"`
	ChildID     string    `json:"child_id"`
	SessionID   string    `json:"session_id"`
	Owner       int       `json:"owner"`
	Interrupted time.Time `json:"interrupted"`
}

// savedEntry reads the entry id of a project's saved helpers or jobs.
func savedEntry(t *testing.T, p liveProject, file, id string) (savedWork, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(p.dataDir, file))
	if errors.Is(err, os.ErrNotExist) {
		return savedWork{}, false
	}
	require.NoError(t, err)
	var saved []savedWork
	if json.Unmarshal(data, &saved) != nil {
		return savedWork{}, false // caught mid-write; the caller polls
	}
	for _, s := range saved {
		if s.ID == id {
			return s, true
		}
	}
	return savedWork{}, false
}

// requireReleased waits until the saved entry id is interrupted and owned
// by no Crush, so the next resume takes it.
func requireReleased(t *testing.T, p liveProject, file, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		s, ok := savedEntry(t, p, file, id)
		return ok && s.Owner == 0 && !s.Interrupted.IsZero()
	}, 20*time.Second, 20*time.Millisecond, "%s must be saved interrupted with owner 0 in %s", id, file)
}

// promptsOf lists the submission IDs liveState holds for app.
func promptsOf(ws *Workspace, app string) []string {
	ws.live.mu.Lock()
	defer ws.live.mu.Unlock()
	var out []string
	for id, p := range ws.live.prompts {
		if p.client == app {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (r *liveRig) view(t *testing.T, ws *Workspace, app, chat string) {
	t.Helper()
	require.NoError(t, r.b.SetCurrentSession(context.Background(), ws.ID, app, chat))
}

// TestQ2ReconnectWithinGraceKeepsRun: a stream that drops and comes back
// within the grace stops nothing. (The grace is 500 ms rather than the
// design's 200 ms, so a slow machine still reattaches inside it.)
func TestQ2ReconnectWithinGraceKeepsRun(t *testing.T) {
	r := newLiveRig(t)
	r.b.SetDetachGrace(500 * time.Millisecond)
	a, other := newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, other)
	c := r.chat(t, ws, "C")
	r.hold(t, ws, a, c, "a1")

	r.b.DetachClient(ws.ID, a)
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, r.b.AttachClient(ws.ID, a))
	time.Sleep(800 * time.Millisecond)

	require.Equal(t, 1, r.model.heldNow("a1"), "the run never stops")
	require.Equal(t, 1, r.model.callsOf("a1"), "and never restarts")
	require.True(t, ws.coordinator().IsSessionBusy(c))
	require.Equal(t, []string{"a1"}, promptsOf(ws, a))
}

// TestQ2DropPastGraceStopsUnviewedWork: past the grace, all of the leaving
// app's work in a chat nobody views stops, and another app's work does not.
func TestQ2DropPastGraceStopsUnviewedWork(t *testing.T) {
	r := newLiveRig(t)
	r.b.SetDetachGrace(500 * time.Millisecond)
	a, bApp := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, a, bApp)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	r.view(t, ws, bApp, d)
	done := watchRunCompletions(t, ws)

	helperA, jobA := r.spawnWork(t, ws, a, c, "a1")
	r.send(t, ws, a, c, "a2", "live:hold:a2")
	requireQueued(t, ws, c, "a2")
	helperB, jobB := r.spawnWork(t, ws, bApp, d, "b1")

	r.b.DetachClient(ws.ID, a)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 1, r.model.heldNow("a1"), "nothing stops inside the grace")

	r.model.waitHeld(t, "a1", 0)
	r.model.waitHeld(t, "a1-helper", 0)
	require.True(t, done.wait(t, "a2").Cancelled, "A's queued prompt is dropped with a cancelled RunComplete")
	require.Eventually(t, func() bool { return len(agent.SessionBackgroundProcesses(c)) == 0 },
		20*time.Second, 20*time.Millisecond, "A's job is killed")
	requireReleased(t, p, "tasks.json", helperA.ID)
	requireReleased(t, p, "background-jobs.json", jobA.JobID)
	require.Eventually(t, func() bool { return !ws.coordinator().IsSessionBusy(c) }, 10*time.Second, 10*time.Millisecond)

	time.Sleep(time.Second)
	require.Zero(t, r.model.noticeCount(), "neither the helper's end nor the job's started a turn")
	require.Zero(t, r.model.callsOf("a2"), "A's queued prompt never ran")
	require.Empty(t, promptsOf(ws, a))

	require.Equal(t, 1, r.model.heldNow("b1"), "B's turn runs on")
	require.Equal(t, 1, r.model.heldNow("b1-helper"), "B's helper runs on")
	task, ok := sessionTask(d, helperB.Name)
	require.True(t, ok)
	require.Equal(t, agent.TaskRunning, task.Status)
	jobs := agent.SessionBackgroundProcesses(d)
	require.Len(t, jobs, 1)
	require.Equal(t, jobB.JobID, jobs[0].JobID, "B's job runs on")
}

// TestQ2QueuedPromptsOfOtherOpenAppSurvive: the leaving app's turn and
// queued prompt stop; another open app's prompt queued between them runs
// next.
func TestQ2QueuedPromptsOfOtherOpenAppSurvive(t *testing.T) {
	r := newLiveRig(t)
	a, bApp := newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, bApp)
	c, d := r.chat(t, ws, "C"), r.chat(t, ws, "D")
	r.view(t, ws, bApp, d)
	done := watchRunCompletions(t, ws)

	r.hold(t, ws, a, c, "a1")
	r.send(t, ws, bApp, c, "b1", "live:hold:b1")
	requireQueued(t, ws, c, "b1")
	r.send(t, ws, a, c, "a2", "live:hold:a2")
	requireQueued(t, ws, c, "a2")

	require.NoError(t, r.b.RetireClient(a))

	require.True(t, done.wait(t, "a2").Cancelled, "A's queued prompt is dropped")
	r.model.waitHeld(t, "a1", 0)
	r.model.waitHeld(t, "b1", 1)
	require.Zero(t, r.model.callsOf("a2"))
	require.Empty(t, done.of("b1"), "B's prompt is not cancelled")
}

// TestQ2TurnOfOtherOpenAppKeepsRunning: a turn another open app started
// keeps running when the app that queued behind it leaves, even with nobody
// viewing the chat.
func TestQ2TurnOfOtherOpenAppKeepsRunning(t *testing.T) {
	r := newLiveRig(t)
	a, bApp := newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, bApp)
	c := r.chat(t, ws, "C")
	done := watchRunCompletions(t, ws)

	r.hold(t, ws, bApp, c, "b1")
	r.send(t, ws, a, c, "a1", "live:hold:a1")
	requireQueued(t, ws, c, "a1")

	require.NoError(t, r.b.RetireClient(a))

	require.True(t, done.wait(t, "a1").Cancelled, "only A's prompt is dropped")
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, r.model.heldNow("b1"), "B's turn keeps running")
	require.Equal(t, 1, r.model.callsOf("b1"))
	require.True(t, ws.coordinator().IsSessionBusy(c))
	require.Empty(t, done.of("b1"))
	require.Zero(t, r.model.callsOf("a1"))
}

// TestEveryPromptGetsSubmissionID: a prompt sent without a submission ID is
// stored under a new UUID, and its RunComplete removes it.
func TestEveryPromptGetsSubmissionID(t *testing.T) {
	r := newLiveRig(t)
	a := newClientID(t)
	ws := r.open(t, r.project(t), a)
	c := r.chat(t, ws, "C")
	done := watchRunCompletions(t, ws)

	r.send(t, ws, a, c, "", "live:hold:x1")
	r.model.waitHeld(t, "x1", 1)
	ids := promptsOf(ws, a)
	require.Len(t, ids, 1)
	_, err := uuid.Parse(ids[0])
	require.NoError(t, err, "the prompt is stored under a new UUID")
	ws.live.mu.Lock()
	stored := ws.live.prompts[ids[0]]
	ws.live.mu.Unlock()
	require.Equal(t, c, stored.sessionID)

	require.NoError(t, r.b.CancelSession(ws.ID, c))
	rc := done.wait(t, ids[0])
	require.Equal(t, c, rc.SessionID, "its RunComplete carries the new ID")
	require.Eventually(t, func() bool { return len(promptsOf(ws, a)) == 0 }, 10*time.Second, 10*time.Millisecond,
		"its RunComplete removes it")

	// A prompt with its own ID keeps it, and goes at its end too.
	r.send(t, ws, a, c, "mine-1", "Say hello.")
	require.Empty(t, done.wait(t, "mine-1").Error)
	require.Eventually(t, func() bool { return len(promptsOf(ws, a)) == 0 }, 10*time.Second, 10*time.Millisecond)
}

// TestQ2KeepsWorkAnotherAppViews: when another app views the chat, the
// leaving app's work keeps going under that app; when that app leaves last,
// teardown stops everything as before.
func TestQ2KeepsWorkAnotherAppViews(t *testing.T) {
	r := newLiveRig(t)
	a, bApp := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, a, bApp)
	c := r.chat(t, ws, "C")
	r.view(t, ws, bApp, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var taskMu sync.Mutex
	var taskEvents []agent.Task
	go func() {
		for ev := range ws.Events(ctx) {
			if te, ok := ev.Payload.(pubsub.Event[agent.Task]); ok {
				taskMu.Lock()
				taskEvents = append(taskEvents, te.Payload)
				taskMu.Unlock()
			}
		}
	}()

	helper, job := r.spawnWork(t, ws, a, c, "a1")
	r.send(t, ws, a, c, "a2", "live:hold:a2")
	requireQueued(t, ws, c, "a2")

	require.NoError(t, r.b.RetireClient(a))

	require.Eventually(t, func() bool {
		task, ok := sessionTask(c, helper.Name)
		return ok && task.StartedBy == bApp
	}, 10*time.Second, 10*time.Millisecond, "the helper's owner becomes B")
	require.Eventually(t, func() bool {
		taskMu.Lock()
		defer taskMu.Unlock()
		return slices.ContainsFunc(taskEvents, func(task agent.Task) bool {
			return task.ID == helper.ID && task.StartedBy == bApp
		})
	}, 10*time.Second, 10*time.Millisecond, "a helper event shows started_by = B")
	require.Eventually(t, func() bool { return slices.Equal(promptsOf(ws, bApp), []string{"a1", "a2"}) },
		10*time.Second, 10*time.Millisecond, "A's prompts become B's")
	ws.live.mu.Lock()
	require.Equal(t, bApp, ws.live.owners[workKey{kind: agent.WorkHelper, id: helper.ID}])
	require.Equal(t, bApp, ws.live.owners[jobKey(job)])
	ws.live.mu.Unlock()
	jobs, err := r.b.ListJobs(ws.ID, c)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, bApp, jobs[0].StartedBy)

	require.Equal(t, 1, r.model.heldNow("a1"), "the turn keeps going")
	require.Equal(t, 1, r.model.heldNow("a1-helper"), "the helper keeps going")
	requireQueued(t, ws, c, "a2")

	// B leaves last: teardown stops it all, as before.
	require.NoError(t, r.b.RetireClient(bApp))
	r.model.waitHeld(t, "a1", 0)
	r.model.waitHeld(t, "a1-helper", 0)
	requireReleased(t, p, "tasks.json", helper.ID)
	requireReleased(t, p, "background-jobs.json", job.JobID)
}

// TestQ2RetireActsAtOnce: a retired app's work stops without waiting for
// the detach grace.
func TestQ2RetireActsAtOnce(t *testing.T) {
	r := newLiveRig(t)
	r.b.SetDetachGrace(time.Minute)
	a, other := newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, other)
	c := r.chat(t, ws, "C")
	r.hold(t, ws, a, c, "a1")

	r.b.DetachClient(ws.ID, a)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, r.model.heldNow("a1"), "inside the grace nothing stops")

	start := time.Now()
	require.NoError(t, r.b.RetireClient(a))
	r.model.waitHeld(t, "a1", 0)
	require.Less(t, time.Since(start), 20*time.Second, "the retire acts at once, not after the minute of grace")
}

// TestQ2DetachWithZeroGrace: with no grace, the last stream's drop ends the
// claim at once (expired), or released after an explicit release, and Q2
// stops the app's unviewed work.
func TestQ2DetachWithZeroGrace(t *testing.T) {
	logs := captureDebugLogs(t)
	r := newLiveRig(t)
	r.b.SetDetachGrace(0)
	a, a2, other := newClientID(t), newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, a2, other)
	c1, c2 := r.chat(t, ws, "C1"), r.chat(t, ws, "C2")
	r.hold(t, ws, a, c1, "a1")
	r.hold(t, ws, a2, c2, "a2")
	left := func(app string, reason GoneReason) bool {
		for line := range strings.Lines(logs.String()) {
			if strings.Contains(line, "An app left a workspace") && strings.Contains(line, "client="+app) &&
				strings.Contains(line, "reason="+string(reason)) {
				return true
			}
		}
		return false
	}

	r.b.DetachClient(ws.ID, a)
	r.model.waitHeld(t, "a1", 0)
	require.True(t, left(a, GoneExpired), "the drop fires gone(expired)")

	require.NoError(t, r.b.DeleteWorkspace(ws.ID, a2))
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, r.model.heldNow("a2"), "a release while a stream is open stops nothing yet")
	r.b.DetachClient(ws.ID, a2)
	r.model.waitHeld(t, "a2", 0)
	require.True(t, left(a2, GoneReleased), "after a release the drop fires gone(released)")
}

// TestQ2InterruptedHelperResumesOnNextJoin: a helper Q2 interrupted starts
// again when the next app joins and starts its agent, on its child chat,
// with its task ID, owned by that app.
func TestQ2InterruptedHelperResumesOnNextJoin(t *testing.T) {
	r := newLiveRig(t)
	a, other, e := newClientID(t), newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, a, other)
	c := r.chat(t, ws, "C")
	helper := r.spawnHelper(t, ws, a, c, "a1")

	require.NoError(t, r.b.RetireClient(a))
	r.model.waitHeld(t, "a1-helper", 0)
	requireReleased(t, p, "tasks.json", helper.ID)

	require.Same(t, ws, r.open(t, p, e))
	r.model.waitHeld(t, "a1-helper", 1)
	task, ok := sessionTask(c, helper.Name)
	require.True(t, ok)
	require.Equal(t, helper.ID, task.ID, "the same task ID")
	require.Equal(t, helper.ChildID, task.ChildID, "on its child chat")
	require.Equal(t, agent.TaskRunning, task.Status)
	require.Equal(t, e, task.StartedBy, "owned by the app that joined")
	require.Equal(t, 2, r.model.callsOf("a1-helper"))
}

// TestQ2NoOwnerWorkUnaffected: a turn no app started keeps running when an
// app leaves, even the app that last sent in that chat.
func TestQ2NoOwnerWorkUnaffected(t *testing.T) {
	r := newLiveRig(t)
	a, other := newClientID(t), newClientID(t)
	ws := r.open(t, r.project(t), a, other)
	c, c2 := r.chat(t, ws, "C"), r.chat(t, ws, "C2")
	done := watchRunCompletions(t, ws)

	r.send(t, ws, a, c, "a0", "Say hello.")
	done.wait(t, "a0")
	require.Eventually(t, func() bool { return len(promptsOf(ws, a)) == 0 }, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, r.b.SendMessage(ws.ID, "", proto.AgentMessage{SessionID: c, Prompt: "live:hold:n1"}))
	r.model.waitHeld(t, "n1", 1)
	r.hold(t, ws, a, c2, "a1")

	require.NoError(t, r.b.RetireClient(a))
	r.model.waitHeld(t, "a1", 0)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, r.model.heldNow("n1"), "the turn with no owner keeps running")
	require.True(t, ws.coordinator().IsSessionBusy(c))
}

// TestTeardownRecreateResumesOnce: a helper cut off because the last app
// left starts exactly once when the project opens again in the same
// process, on the new hub, with its task ID; it stays saved until it ends.
func TestTeardownRecreateResumesOnce(t *testing.T) {
	r := newLiveRig(t)
	a, n := newClientID(t), newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, a)
	c := r.chat(t, ws, "C")
	helper := r.spawnHelper(t, ws, a, c, "h1")

	require.NoError(t, r.b.RetireClient(a))
	r.model.waitHeld(t, "h1-helper", 0)
	require.Empty(t, agent.SessionTasks(c), "the old hub is no longer listed")
	requireReleased(t, p, "tasks.json", helper.ID)

	ws2 := r.open(t, p, n)
	require.NotEqual(t, ws.ID, ws2.ID)
	require.NoError(t, r.b.InitAgent(context.Background(), ws2.ID, n, true))
	requireResumedOnce(t, r, p, c, helper)

	require.NoError(t, agent.StopSessionTask(c, helper.ID))
	require.Eventually(t, func() bool {
		_, saved := savedEntry(t, p, "tasks.json", helper.ID)
		return !saved
	}, 20*time.Second, 20*time.Millisecond, "an ended helper leaves tasks.json")
}

// requireResumedOnce checks that helper runs again exactly once, on one
// hub, saved as this process's.
func requireResumedOnce(t *testing.T, r *liveRig, p liveProject, chat string, helper agent.Task) {
	t.Helper()
	name := helper.Name
	r.model.waitHeld(t, name, 1)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 2, r.model.callsOf(name), "the helper starts exactly once more")
	require.Equal(t, 1, r.model.heldNow(name))
	var running []agent.Task
	for _, task := range agent.SessionTasks(chat) {
		if task.ID == helper.ID {
			running = append(running, task)
		}
	}
	require.Len(t, running, 1, "one hub lists it")
	require.Equal(t, agent.TaskRunning, running[0].Status)
	require.Equal(t, helper.ChildID, running[0].ChildID)
	s, ok := savedEntry(t, p, "tasks.json", helper.ID)
	require.True(t, ok, "it stays saved while it runs")
	require.Equal(t, os.Getpid(), s.Owner)
}

// TestRecreateWhileTeardownRuns: the project opens again while the old
// workspace is still shutting down; the helper the old one cut off starts
// exactly once, on the new hub.
func TestRecreateWhileTeardownRuns(t *testing.T) {
	r := newLiveRig(t)
	for round := range 20 {
		a, n := newClientID(t), newClientID(t)
		p := r.project(t)
		ws := r.open(t, p, a)
		c := r.chat(t, ws, "C")
		helper := r.spawnHelper(t, ws, a, c, fmt.Sprintf("r%d", round))

		entered, gate := make(chan struct{}), make(chan struct{})
		SetWorkspaceShutdownFnForTest(ws, func() {
			close(entered)
			<-gate
			ws.Shutdown()
		})
		retired := make(chan error, 1)
		go func() { retired <- r.b.RetireClient(a) }()
		select {
		case <-entered:
		case <-time.After(20 * time.Second):
			t.Fatalf("round %d: teardown did not start", round)
		}

		ws2 := r.open(t, p, n)
		require.NotEqual(t, ws.ID, ws2.ID)
		close(gate)
		require.NoError(t, <-retired)
		require.NoError(t, r.b.InitAgent(context.Background(), ws2.ID, n, true))
		requireResumedOnce(t, r, p, c, helper)

		require.NoError(t, r.b.RetireClient(n))
		r.model.waitHeld(t, helper.Name, 0)
	}
}
