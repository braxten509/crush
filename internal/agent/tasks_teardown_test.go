package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/shell"
)

// blockingModelURL serves model requests that end only when their caller
// gives up, so a helper started on it runs until it is stopped.
func blockingModelURL(t *testing.T) string {
	t.Helper()
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(func() {
		close(stop)
		srv.Close()
	})
	return srv.URL + "/v1"
}

// resumeTestCoordinator is an interactive coordinator whose "fake" provider
// runs helpers until they are stopped. run answers the prompts it runs, such
// as job reports.
func resumeTestCoordinator(t *testing.T, run func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)) *coordinator {
	t.Helper()
	url := blockingModelURL(t)
	c := sessionRunTestCoordinator(t, run)
	c.interactive = true
	c.cfg.Config().Providers.Set("fake", config.ProviderConfig{
		ID: "fake", Name: "Fake", Type: catwalk.TypeOpenAICompat, BaseURL: url, APIKey: "test-key",
		Models: []catwalk.Model{{ID: "fake-model", Name: "Fake", ContextWindow: 8192, DefaultMaxTokens: 128}},
	})
	return c
}

// resumeTestHub is a fresh hub of c's project, listed in hubs and stopped
// when the test ends.
func resumeTestHub(t *testing.T, c *coordinator) *taskHub {
	t.Helper()
	events := pubsub.NewBroker[Task]()
	t.Cleanup(events.Shutdown)
	h := &taskHub{c: c, dir: t.TempDir(), events: events, tasks: map[string]*Task{}, cancels: map[string]context.CancelFunc{}, userStopped: map[string]bool{}}
	listTestHub(t, h)
	t.Cleanup(h.stopAll)
	return h
}

// listTestHub lists h in hubs until the test ends.
func listTestHub(t *testing.T, h *taskHub) {
	t.Helper()
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		hubs = slices.DeleteFunc(hubs, func(other *taskHub) bool { return other == h })
		hubsMu.Unlock()
	})
}

func hubListed(h *taskHub) bool {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	return slices.Contains(hubs, h)
}

// helperChats creates a parent chat and a helper's own chat in c's project.
func helperChats(t *testing.T, c *coordinator) (session.Session, session.Session) {
	t.Helper()
	parent, err := c.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	child, err := c.sessions.CreateTaskSession(t.Context(), uuid.NewString(), parent.ID, "worker")
	require.NoError(t, err)
	return parent, child
}

func helperTask(parent, child session.Session) Task {
	return Task{ID: "t1", SessionID: parent.ID, ChildID: child.ID, Name: "worker", CLI: "fake", Model: "fake-model", Status: TaskRunning, Started: time.Now()}
}

// releasedHelper saves the helper cut off and let go, the way a teardown or
// InterruptTask leaves it.
func releasedHelper(t *testing.T, h *taskHub, task Task) {
	t.Helper()
	require.NoError(t, h.editSaved(func([]savedTask) []savedTask {
		return []savedTask{{Task: task, Provider: "fake", Interrupted: time.Now()}}
	}))
}

// runningHelper registers the helper as running on h and saves it under
// this process, without running an agent for it.
func runningHelper(h *taskHub, task Task) {
	h.mu.Lock()
	h.tasks[task.ID] = &task
	h.cancels[task.ID] = func() {}
	h.mu.Unlock()
	h.remember(task, "fake")
}

// countStarts counts the starts of task id on h: one "running" event each.
func countStarts(t *testing.T, h *taskHub, id string) func() int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := h.events.(*pubsub.Broker[Task]).Subscribe(ctx)
	var n atomic.Int32
	go func() {
		for e := range events {
			if e.Payload.ID == id && e.Payload.Status == TaskRunning {
				n.Add(1)
			}
		}
	}()
	return func() int { return int(n.Load()) }
}

// watchedJob starts a managed job of the chat on h, the way crush bg does,
// without the bash tool's wait for early output.
func watchedJob(t *testing.T, h *taskHub, sessionID, command string) *shell.BackgroundShell {
	t.Helper()
	jobs := shell.GetBackgroundShellManager()
	job, err := jobs.Start(t.Context(), t.TempDir(), nil, command, "job", h.env(sessionID)...)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = jobs.Kill(job.ID)
		_ = jobs.Remove(job.ID)
	})
	h.watchJob(t.Context(), func() {}, sessionID, uuid.NewString(), job, false)
	return job
}

func replyOK(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	return fakeSessionReply(call, "ok"), nil
}

func TestStopAllUnregistersHub(t *testing.T) {
	h, _ := savingHub(t, true)
	h.dir = t.TempDir()
	listTestHub(t, h)
	read, write, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = read.Close() })
	h.reaper = write
	id := "t-" + uuid.NewString()
	chat := uuid.NewString()
	h.tasks[id] = &Task{ID: id, SessionID: chat, ChildID: uuid.NewString(), Status: TaskRunning}
	h.cancels[id] = func() {}
	require.Len(t, SessionTasks(chat), 1)

	h.stopAll()
	require.False(t, hubListed(h))
	require.Empty(t, SessionTasks(chat), "a removed hub is not scanned")
	require.Error(t, StopTask(id), "a removed hub's tasks can't be stopped by ID")
	require.Nil(t, h.reaper)

	got := make(chan error, 1)
	go func() {
		_, err := read.Read(make([]byte, 1))
		got <- err
	}()
	select {
	case err := <-got:
		require.ErrorIs(t, err, io.EOF, "closing the pipe starts the reaper")
	case <-time.After(time.Second):
		t.Fatal("the reaper's pipe was left open")
	}
}

func TestStopAllTwiceIsHarmless(t *testing.T) {
	h, path := savingHub(t, true)
	h.dir = t.TempDir()
	listTestHub(t, h)
	read, write, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = read.Close() })
	h.reaper = write
	running := Task{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: TaskRunning, Started: time.Now()}
	h.remember(running, "codex")
	h.tasks["t1"] = &running
	cancels := 0
	h.cancels["t1"] = func() { cancels++ }

	h.stopAll()
	require.Nil(t, h.reaper, "the reaper is closed once, then forgotten")
	h.stopAll()
	require.Equal(t, 1, cancels)
	require.False(t, hubListed(h))
	_, err = read.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
	saved := readSaved(t, path)
	require.Len(t, saved, 1)
	require.Zero(t, saved[0].Owner)
	require.False(t, saved[0].Interrupted.IsZero())
}

func TestClaimReleasedOwnerZero(t *testing.T) {
	t.Parallel()
	h, path := savingHub(t, true)
	require.True(t, taskProcessAlive(0), "the case this pins: owner 0 looks alive")
	now := time.Now()
	require.NoError(t, h.editSaved(func([]savedTask) []savedTask {
		return []savedTask{
			{Task: Task{ID: "t1", ChildID: "released", Started: now}, Interrupted: now},
			{Task: Task{ID: "t2", ChildID: "old", Started: now.Add(-3 * resumeWindow)}, Interrupted: now.Add(-2 * resumeWindow)},
		}
	}))
	claimed, err := h.claimInterrupted()
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "released", claimed[0].ChildID)
	require.Equal(t, os.Getpid(), claimed[0].Owner)
	saved := readSaved(t, path)
	require.Len(t, saved, 1, "a released task too old to resume is dropped")
	require.Equal(t, os.Getpid(), saved[0].Owner, "a claimed task stays saved under this Crush")

	require.NoError(t, h.editSavedJobs(func([]savedJob) []savedJob {
		return []savedJob{
			{ID: "001", SessionID: "s", Started: now, Interrupted: now},
			{ID: "002", SessionID: "s", Started: now.Add(-3 * resumeWindow), Interrupted: now.Add(-2 * resumeWindow)},
		}
	}))
	jobs, err := h.claimInterruptedJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, "001", jobs[0].ID)
	require.Empty(t, readSavedJobs(t, h.savedDataPath(savedJobsFile)))
}

func TestResumeTwiceIsIdempotent(t *testing.T) {
	c := resumeTestCoordinator(t, replyOK)
	h := resumeTestHub(t, c)
	parent, child := helperChats(t, c)
	releasedHelper(t, h, helperTask(parent, child))
	starts := countStarts(t, h, "t1")

	h.resumeInterrupted(t.Context())
	h.resumeInterrupted(t.Context())
	require.Eventually(t, func() bool { return starts() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return starts() > 1 }, 300*time.Millisecond, 20*time.Millisecond)
	tasks := SessionTasks(parent.ID)
	require.Len(t, tasks, 1)
	require.Equal(t, "t1", tasks[0].ID)
	require.Equal(t, TaskRunning, tasks[0].Status)
	saved := readSaved(t, h.savedTasksPath())
	require.Len(t, saved, 1)
	require.Equal(t, os.Getpid(), saved[0].Owner)
}

func TestStartRefusedOnClosingHub(t *testing.T) {
	c := resumeTestCoordinator(t, replyOK)
	parent, child := helperChats(t, c)

	h := resumeTestHub(t, c)
	h.mu.Lock()
	h.closing = true
	h.mu.Unlock()
	var released atomic.Int32
	_, err := h.start(t.Context(), func() { released.Add(1) }, &Task{SessionID: parent.ID, ChildID: child.ID, Status: TaskRunning}, nil, Model{}, config.ProviderConfig{ID: "fake"}, "work")
	require.ErrorIs(t, err, errHubClosing)
	require.Equal(t, int32(1), released.Load())
	h.mu.Lock()
	require.Empty(t, h.tasks)
	require.Empty(t, h.cancels)
	h.mu.Unlock()

	// A claimed entry whose hub starts closing before its start is saved
	// released again, not forgotten.
	h2 := resumeTestHub(t, c)
	releasedHelper(t, h2, helperTask(parent, child))
	h2.mu.Lock()
	claimed, err := h2.claimInterrupted()
	h2.closing = true
	h2.mu.Unlock()
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	h2.resumeClaimed(t.Context(), claimed, nil)
	saved := readSaved(t, h2.savedTasksPath())
	require.Len(t, saved, 1)
	require.Zero(t, saved[0].Owner)
	require.False(t, saved[0].Interrupted.IsZero())
	h2.mu.Lock()
	require.Empty(t, h2.tasks)
	h2.mu.Unlock()
}

func TestClosingHubNeverClaims(t *testing.T) {
	reports := make(chan SessionAgentCall, 8)
	c := resumeTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		reports <- call
		return fakeSessionReply(call, "ok"), nil
	})
	parent, child := helperChats(t, c)
	h1 := resumeTestHub(t, c)
	runningHelper(h1, helperTask(parent, child))
	now := time.Now()
	require.NoError(t, h1.editSavedJobs(func([]savedJob) []savedJob {
		return []savedJob{{ID: "J1", SessionID: parent.ID, Name: "build", Command: "sleep 30", Started: now, Interrupted: now}}
	}))

	h1.stopAll()
	// As a copy of hubs taken before the teardown would make it.
	h1.resumeInterrupted(t.Context())
	select {
	case call := <-reports:
		t.Fatalf("a closing hub must run nothing: %q", call.Prompt)
	case <-time.After(300 * time.Millisecond):
	}
	require.Len(t, readSavedJobs(t, h1.savedDataPath(savedJobsFile)), 1, "the job entry stays for the next hub")
	saved := readSaved(t, h1.savedTasksPath())
	require.Len(t, saved, 1)
	require.Zero(t, saved[0].Owner)

	// The project's next hub resumes the helper once and reports the job once.
	h2 := resumeTestHub(t, c)
	starts := countStarts(t, h2, "t1")
	h2.resumeInterrupted(t.Context())
	h2.resumeInterrupted(t.Context())
	select {
	case call := <-reports:
		require.Equal(t, parent.ID, call.SessionID)
		require.Contains(t, call.Prompt, "Job J1 (build)")
	case <-time.After(5 * time.Second):
		t.Fatal("the next hub did not report the job")
	}
	require.Eventually(t, func() bool { return starts() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return starts() > 1 || len(reports) > 0 }, 300*time.Millisecond, 20*time.Millisecond)
	require.Empty(t, readSavedJobs(t, h2.savedDataPath(savedJobsFile)))
	saved = readSaved(t, h2.savedTasksPath())
	require.Len(t, saved, 1)
	require.Equal(t, os.Getpid(), saved[0].Owner)
}

func TestTeardownRacingResumeKeepsWork(t *testing.T) {
	var reported atomic.Int32
	c := resumeTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if strings.Contains(call.Prompt, "crush bg jobs were still running") {
			reported.Add(1)
		}
		return fakeSessionReply(call, "ok"), nil
	})
	parent, child := helperChats(t, c)
	for round := range 200 {
		h := resumeTestHub(t, c)
		runningHelper(h, helperTask(parent, child))
		job := watchedJob(t, h, parent.ID, "sleep 30")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			h.stopAll()
		}()
		go func() {
			defer wg.Done()
			h.resumeInterrupted(t.Context())
		}()
		wg.Wait()
		require.NoError(t, shell.GetBackgroundShellManager().Kill(job.ID))
		require.Eventually(t, func() bool {
			saved := readSavedJobs(t, h.savedDataPath(savedJobsFile))
			return len(saved) == 1 && saved[0].Owner == 0 && !saved[0].Interrupted.IsZero()
		}, 5*time.Second, 5*time.Millisecond, "round %d: the job is saved released", round)
		saved := readSaved(t, h.savedTasksPath())
		require.Len(t, saved, 1, "round %d", round)
		require.Zero(t, saved[0].Owner, "round %d: the helper is saved released", round)
		h.mu.Lock()
		require.Len(t, h.tasks, 1, "round %d: the closed hub started nothing", round)
		h.mu.Unlock()
		require.Zero(t, reported.Load(), "round %d: no job report on a closed hub", round)
		require.NoError(t, h.editSaved(func([]savedTask) []savedTask { return nil }))
		require.NoError(t, h.editSavedJobs(func([]savedJob) []savedJob { return nil }))
	}
	require.Never(t, func() bool { return reported.Load() > 0 }, 300*time.Millisecond, 20*time.Millisecond)
}
