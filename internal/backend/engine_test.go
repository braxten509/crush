package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// engineProject makes a Git project with a .crush folder at its root and
// the folders a and b inside, and returns the root and the canonical data
// folder that both folders resolve to.
func engineProject(t *testing.T) (root, dataDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root = t.TempDir()
	out, err := exec.Command("git", "init", "-q", root).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, dir := range []string{".crush", "a", "b"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, dir), 0o700))
	}
	dataDir, err = config.CanonicalDataDir(filepath.Join(root, ".crush"))
	require.NoError(t, err)
	return root, dataDir
}

// newEngineBackend returns an engine backend for the project whose
// canonical data folder is dataDir.
func newEngineBackend(t *testing.T, dataDir string) *Backend {
	t.Helper()
	b := New(context.Background(), nil, func() {})
	b.SetCreateGrace(5 * time.Second)
	b.SetEngine(dataDir)
	t.Cleanup(func() { drainBackend(t, b) })
	return b
}

// appCreate is the create request an app in folder sends, without a data
// folder of its own.
func appCreate(folder string) proto.Workspace {
	return prototest.CreateRequest(proto.Workspace{Path: folder, ClientID: uuid.NewString()})
}

func TestEngineJoinsAppsFromDifferentFolders(t *testing.T) {
	xdgIsolated(t)
	root, dataDir := engineProject(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")

	engine := newEngineBackend(t, dataDir)
	wsA, answerA, err := engine.CreateWorkspace(appCreate(a))
	require.NoError(t, err)
	wsB, answerB, err := engine.CreateWorkspace(appCreate(b))
	require.NoError(t, err)
	require.Equal(t, wsA.ID, wsB.ID, "apps of one project share one workspace")
	require.Equal(t, answerA.ID, answerB.ID)
	require.Equal(t, a, answerB.Path, "the AI works in the first app's folder")
	require.Equal(t, dataDir, answerB.DataDir)
	require.Len(t, engine.ListWorkspaces(), 1)

	// The legacy server keeps one workspace per folder.
	legacy := New(context.Background(), nil, func() {})
	legacy.SetCreateGrace(5 * time.Second)
	t.Cleanup(func() { drainBackend(t, legacy) })
	legacyA, _, err := legacy.CreateWorkspace(appCreate(filepath.Join(root, "a")))
	require.NoError(t, err)
	legacyB, _, err := legacy.CreateWorkspace(appCreate(filepath.Join(root, "b")))
	require.NoError(t, err)
	require.NotEqual(t, legacyA.ID, legacyB.ID)
}

// queueCoordinator stands in for the agent: the first prompt of a chat
// runs until the workspace shuts down, and prompts sent meanwhile queue
// behind it, as the real scheduler does.
type queueCoordinator struct {
	agent.Coordinator
	mu       sync.Mutex
	active   map[string]int
	queued   map[string]int
	entered  chan string
	stop     chan struct{}
	stopOnce sync.Once
}

func newQueueCoordinator() *queueCoordinator {
	return &queueCoordinator{
		active:  map[string]int{},
		queued:  map[string]int{},
		entered: make(chan string, 8),
		stop:    make(chan struct{}),
	}
}

func (c *queueCoordinator) BeginAccepted(string) *agent.AcceptedRun { return nil }

func (c *queueCoordinator) RunAccepted(ctx context.Context, _ *agent.AcceptedRun, sessionID, prompt string, _ ...message.Attachment) (*fantasy.AgentResult, error) {
	c.mu.Lock()
	if c.active[sessionID] > 0 {
		c.queued[sessionID]++
		c.mu.Unlock()
		c.entered <- prompt
		return nil, nil
	}
	c.active[sessionID]++
	c.mu.Unlock()
	c.entered <- prompt
	select {
	case <-c.stop:
	case <-ctx.Done():
	}
	return nil, context.Canceled
}

func (c *queueCoordinator) CancelAll() { c.stopOnce.Do(func() { close(c.stop) }) }

func (c *queueCoordinator) counts(sessionID string) (active, queued int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active[sessionID], c.queued[sessionID]
}

func TestEngineOneQueueOneWorkerPerChat(t *testing.T) {
	xdgIsolated(t)
	root, dataDir := engineProject(t)
	engine := newEngineBackend(t, dataDir)

	createA, createB := appCreate(filepath.Join(root, "a")), appCreate(filepath.Join(root, "b"))
	ws, answerA, err := engine.CreateWorkspace(createA)
	require.NoError(t, err)
	_, answerB, err := engine.CreateWorkspace(createB)
	require.NoError(t, err)
	coordinator := newQueueCoordinator()
	ws.AgentCoordinator = coordinator

	const chat = "chat"
	require.NoError(t, engine.SendMessage(answerA.ID, createA.ClientID, proto.AgentMessage{SessionID: chat, Prompt: "from a"}))
	require.NoError(t, engine.SendMessage(answerB.ID, createB.ClientID, proto.AgentMessage{SessionID: chat, Prompt: "from b"}))
	for range 2 {
		select {
		case <-coordinator.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("a prompt never reached the agent")
		}
	}
	active, queued := coordinator.counts(chat)
	require.Equal(t, 1, active, "one run works on the chat")
	require.Equal(t, 1, queued, "the other app's prompt waits in the same queue")
}

func TestFirstFolderWinsUntilAllLeave(t *testing.T) {
	xdgIsolated(t)
	root, dataDir := engineProject(t)
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	engine := newEngineBackend(t, dataDir)

	createA, createB := appCreate(a), appCreate(b)
	ws, _, err := engine.CreateWorkspace(createA)
	require.NoError(t, err)
	_, _, err = engine.CreateWorkspace(createB)
	require.NoError(t, err)

	require.NoError(t, engine.RetireClient(createA.ClientID))
	answer, err := engine.GetWorkspaceProto(ws.ID)
	require.NoError(t, err, "the second app still holds the workspace")
	require.Equal(t, a, answer.Path, "the first folder wins even after the first app left")

	require.NoError(t, engine.RetireClient(createB.ClientID))
	_, err = engine.GetWorkspace(ws.ID)
	require.ErrorIs(t, err, ErrWorkspaceNotFound, "the last app left, so the workspace is gone")

	next, answer, err := engine.CreateWorkspace(appCreate(b))
	require.NoError(t, err)
	require.NotEqual(t, ws.ID, next.ID)
	require.Equal(t, b, answer.Path, "after teardown the next app's folder wins")
}

func TestCreateAnswerCarriesEffectiveYolo(t *testing.T) {
	xdgIsolated(t)
	root, dataDir := engineProject(t)
	engine := newEngineBackend(t, dataDir)

	first := appCreate(filepath.Join(root, "a"))
	first.YOLO = true
	_, answer, err := engine.CreateWorkspace(first)
	require.NoError(t, err)
	require.True(t, answer.YOLO)

	second := appCreate(filepath.Join(root, "b"))
	second.YOLO = false
	_, answer, err = engine.CreateWorkspace(second)
	require.NoError(t, err)
	require.True(t, answer.YOLO, "the answer carries the workspace's value, not the request's")
}

func TestEngineRefusesOtherProject(t *testing.T) {
	xdgIsolated(t)
	root, dataDir := engineProject(t)
	engine := newEngineBackend(t, dataDir)

	other := t.TempDir()
	_, _, err := engine.CreateWorkspace(appCreate(other))
	require.ErrorIs(t, err, ErrWrongProject)
	require.ErrorContains(t, err, fmt.Sprintf("this Crush engine serves the project whose data folder is %s, not %s", dataDir, filepath.Join(other, ".crush")))
	require.Empty(t, engine.ListWorkspaces())

	// An explicit data folder of another project is refused too.
	create := appCreate(filepath.Join(root, "a"))
	create.DataDir = filepath.Join(other, ".crush")
	_, _, err = engine.CreateWorkspace(create)
	require.ErrorIs(t, err, ErrWrongProject)

	// The project's own data folder, named explicitly, is accepted.
	create = appCreate(filepath.Join(root, "a"))
	create.DataDir = filepath.Join(root, ".crush")
	_, _, err = engine.CreateWorkspace(create)
	require.NoError(t, err)
}

func TestEngineExitDelayIsTenSeconds(t *testing.T) {
	t.Setenv("CRUSH_SERVER_IDLE_TIMEOUT", "")
	t.Setenv("CRUSH_ENGINE_IDLE_TIMEOUT", "")

	b := New(context.Background(), nil, func() {})
	require.Equal(t, 60*time.Second, b.lingerDelay, "the legacy server keeps its 60 s")
	require.Empty(t, b.EngineDataDir())

	b.SetEngine("/project/.crush")
	require.Equal(t, EngineIdleShutdownDelay, b.lingerDelay)
	require.Equal(t, 10*time.Second, EngineIdleShutdownDelay)
	require.Equal(t, "/project/.crush", b.EngineDataDir())

	// The idle timer that teardown arms uses it.
	b.mu.Lock()
	require.False(t, b.scheduleShutdownIfIdleLocked())
	require.NotNil(t, b.shutdownTimer)
	b.cancelShutdownLocked()
	b.mu.Unlock()

	// Tests shorten it.
	t.Setenv("CRUSH_ENGINE_IDLE_TIMEOUT", "2s")
	b.SetEngine("/project/.crush")
	require.Equal(t, 2*time.Second, b.lingerDelay)
	t.Setenv("CRUSH_ENGINE_IDLE_TIMEOUT", "3")
	b.SetEngine("/project/.crush")
	require.Equal(t, 3*time.Second, b.lingerDelay)
}

func TestEngineFirstJoinGraceIsSixtySeconds(t *testing.T) {
	t.Setenv("CRUSH_ENGINE_FIRST_JOIN_GRACE", "")

	legacy := New(context.Background(), nil, func() {})
	legacy.ArmFirstJoinGrace()
	legacy.mu.Lock()
	require.Nil(t, legacy.shutdownTimer, "the legacy server waits for no first app")
	legacy.mu.Unlock()

	b := New(context.Background(), nil, func() {})
	b.SetEngine("/project/.crush")
	require.Equal(t, 60*time.Second, EngineFirstJoinGrace)
	require.Equal(t, EngineFirstJoinGrace, b.firstJoinGrace)
	b.ArmFirstJoinGrace()
	b.mu.Lock()
	require.NotNil(t, b.shutdownTimer, "the engine arms its idle shutdown")
	b.cancelShutdownLocked()
	b.mu.Unlock()

	// Tests shorten it.
	t.Setenv("CRUSH_ENGINE_FIRST_JOIN_GRACE", "1s")
	b.SetEngine("/project/.crush")
	require.Equal(t, time.Second, b.firstJoinGrace)
}

func TestCreateRefusesProtocolMismatch(t *testing.T) {
	t.Parallel()

	b := New(context.Background(), nil, func() {})
	for _, protocol := range []int{0, proto.ProtocolVersion + 1} {
		create := prototest.CreateRequest(proto.Workspace{Path: t.TempDir(), ClientID: uuid.NewString()})
		create.Protocol = protocol
		_, _, err := b.CreateWorkspace(create)
		require.ErrorIs(t, err, ErrProtocolMismatch)
		require.ErrorContains(t, err, fmt.Sprintf("this Crush engine speaks protocol %d", proto.ProtocolVersion))
		require.ErrorContains(t, err, fmt.Sprintf("the app speaks protocol %d", protocol))
	}
	require.Empty(t, b.ListWorkspaces())

	// Checked first: even a create with no folder is refused for it.
	_, _, err := b.CreateWorkspace(proto.Workspace{})
	require.ErrorIs(t, err, ErrProtocolMismatch)
}

func TestEngineInfoAndClosing(t *testing.T) {
	t.Parallel()

	b, _ := newTestBackend(t)
	b.startedAt = time.Now()
	b.engineDataDir = "/project/.crush"
	info := b.EngineInfo("unix:///run/crush/engine-0123456789abcdef.sock")
	require.Equal(t, proto.ProtocolVersion, info.Protocol)
	require.Equal(t, os.Getpid(), info.PID)
	require.Equal(t, "/project/.crush", info.DataDir)
	require.Equal(t, "unix:///run/crush/engine-0123456789abcdef.sock", info.Address)
	require.Equal(t, b.startedAt, info.StartedAt)
	require.Empty(t, info.WorkspaceID)
	require.Zero(t, info.Apps)
	require.False(t, info.Closing)

	ws, _ := insertTestWorkspace(t, b, "/project")
	b.createGrace = time.Hour
	clientID := newClientID(t)
	require.True(t, b.registerClient(ws, clientID))
	t.Cleanup(func() { _ = b.releaseHold(ws.ID, clientID) })
	info = b.EngineInfo("")
	require.Equal(t, ws.ID, info.WorkspaceID)
	require.Equal(t, "/project", info.WorkspacePath)
	require.Equal(t, 1, info.Apps)

	require.False(t, b.Closing())
	b.mu.Lock()
	b.closing = true
	b.mu.Unlock()
	require.True(t, b.Closing())
	require.True(t, b.EngineInfo("").Closing)
}
