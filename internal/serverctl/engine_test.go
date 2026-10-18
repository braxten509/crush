//go:build !windows

package serverctl_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/stretchr/testify/require"
)

// TestEnsureRestartsIdleMismatchedEngine: an idle engine from before the
// protocol field (it reports protocol 0) agrees to stand down, and ensure
// starts a new engine in its place.
func TestEnsureRestartsIdleMismatchedEngine(t *testing.T) {
	p := newEngineProject(t, "")
	fake := p.startFakeEngine(t, fakeEngineOpts{
		vi: proto.VersionInfo{Version: version.Version, BuildID: version.BuildID},
	})

	h, err := p.ensure(t.Context())
	require.NoError(t, err)
	require.True(t, h.Started, "a new engine replaces the idle old one")
	require.NotEqual(t, fakeEnginePID, h.PID)
	require.Equal(t, proto.ProtocolVersion, h.Protocol)
	require.Equal(t, p.dataDir, h.DataDir)
	require.Equal(t, p.socket, h.Socket)
	require.Equal(t, "unix://"+p.socket, h.Address)
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, fake.commandsSeen(),
		"only the conditional shutdown is asked")
}

// TestEnsureRefusesBusyMismatchedEngine: a busy engine of another protocol
// is never reused, and is left running.
func TestEnsureRefusesBusyMismatchedEngine(t *testing.T) {
	p := newEngineProject(t, "")
	fake := p.startFakeEngine(t, fakeEngineOpts{
		vi:   proto.VersionInfo{Version: "v9.9.9", Protocol: proto.ProtocolVersion + 1},
		busy: true,
	})

	_, err := p.ensure(t.Context())
	require.ErrorIs(t, err, backend.ErrProtocolMismatch)
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, fake.commandsSeen())

	// Untouched: it still answers, and no engine was started next to it.
	c, err := client.NewClient(p.cwd, "unix", p.socket)
	require.NoError(t, err)
	vi, err := c.VersionInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, proto.ProtocolVersion+1, vi.Protocol)
	stateDir, err := serverctl.EngineStateDir(p.socket)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(stateDir, "engine.log"))
}

// TestEnsureReusesBusySameProtocolOtherBuild keeps today's build rule: a
// busy engine of the same protocol but another build is reused.
func TestEnsureReusesBusySameProtocolOtherBuild(t *testing.T) {
	p := newEngineProject(t, "")
	fake := p.startFakeEngine(t, fakeEngineOpts{
		vi: proto.VersionInfo{
			Version: "not-" + version.Version, BuildID: "other-build", Protocol: proto.ProtocolVersion,
		},
		busy: true,
	})

	h, err := p.ensure(t.Context())
	require.NoError(t, err)
	require.False(t, h.Started)
	require.Equal(t, fakeEnginePID, h.PID)
	require.Equal(t, "not-"+version.Version, h.Version)
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, fake.commandsSeen())
}

// TestReopenBeforeAndAfterIdleExit scales the plan's reopen at 5 s and at
// 11 s for a 10 s exit to a 2 s exit: the last app closes cleanly, then an
// app reopens at 1 s and reaches the same engine; after it closes again,
// an app reopening at 3 s gets a new engine.
func TestReopenBeforeAndAfterIdleExit(t *testing.T) {
	p := newEngineProject(t, "2s")
	ctx := t.Context()

	h1, err := p.ensure(ctx)
	require.NoError(t, err)
	require.True(t, h1.Started)
	c1, _ := p.create(t, h1)
	require.NoError(t, c1.RetireClient(ctx))

	time.Sleep(time.Second)
	h2, err := p.ensure(ctx)
	require.NoError(t, err)
	require.False(t, h2.Started, "the lingering engine is reused")
	require.Equal(t, h1.PID, h2.PID)
	c2, _ := p.create(t, h2)
	require.NoError(t, c2.RetireClient(ctx))

	time.Sleep(3 * time.Second)
	h3, err := p.ensure(ctx)
	require.NoError(t, err)
	require.True(t, h3.Started, "the idle engine exited, so a new one starts")
	require.NotEqual(t, h1.PID, h3.PID)
	require.Eventually(t, func() bool { return processGone(h1.PID) }, 2*time.Second, 20*time.Millisecond,
		"the old engine exited by itself")
	c3, _ := p.create(t, h3)
	require.NoError(t, c3.RetireClient(ctx))
}

// TestCreateDuringExitRetriesThroughEnsure: the engine's exit latch closes
// between ensure and create, so the create answers 503 shutting_down. The
// create's retry runs ensure again, which treats the closing engine as
// gone, waits for it to let go of the project, and starts a new engine,
// where the second create lands.
func TestCreateDuringExitRetriesThroughEnsure(t *testing.T) {
	p := newEngineProject(t, "")
	ctx := t.Context()

	// The old engine holds the project until it has exited.
	require.NoError(t, os.MkdirAll(p.dataDir, 0o700))
	releaseLock, err := db.LockProject(ctx, p.dataDir, db.ProjectLockEngine, 0)
	require.NoError(t, err)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { _ = releaseLock() }) }
	t.Cleanup(release)

	fake := p.startFakeEngine(t, fakeEngineOpts{
		vi: proto.VersionInfo{Version: version.Version, BuildID: version.BuildID, Protocol: proto.ProtocolVersion},
		onCreate: func(f *fakeEngine, w http.ResponseWriter) {
			// The exit timer fired after ensure: the engine refuses the
			// create, reports closing, then stops listening and exits.
			f.setClosing()
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			go func() {
				time.Sleep(300 * time.Millisecond)
				f.stopListening()
				release()
			}()
		},
	})

	h1, err := p.ensure(ctx)
	require.NoError(t, err)
	require.False(t, h1.Started)
	require.Equal(t, fakeEnginePID, h1.PID, "ensure reaches the engine before its exit latch closes")

	c, err := client.NewClient(p.cwd, "unix", h1.Socket)
	require.NoError(t, err)
	var replacements []serverctl.EngineHandle
	ws, err := serverctl.CreateWorkspace(ctx, c, prototest.CreateRequest(proto.Workspace{Path: p.cwd}), func() error {
		h, err := p.ensure(ctx)
		replacements = append(replacements, h)
		return err
	})
	require.NoError(t, err, "the 503 must not reach the caller")
	t.Cleanup(func() { _ = c.RetireClient(context.Background()) })
	require.Equal(t, 1, fake.createCount())
	require.Len(t, replacements, 1, "ensure runs again once")
	require.True(t, replacements[0].Started, "a closing engine is never reused")
	require.NotEqual(t, fakeEnginePID, replacements[0].PID)

	info, err := c.EngineInfo(ctx)
	require.NoError(t, err)
	require.Equal(t, replacements[0].PID, info.PID)
	require.Equal(t, ws.ID, info.WorkspaceID, "the second create landed on the new engine")
}

// TestEnsureWaitsForSlowOldEngine: an old engine's socket is gone but it
// still holds the project lock for 3 s. Ensure waits for the lock instead
// of starting an engine at once, so the new engine never reports
// project_locked.
func TestEnsureWaitsForSlowOldEngine(t *testing.T) {
	p := newEngineProject(t, "")
	ctx := t.Context()
	require.NoError(t, os.MkdirAll(p.dataDir, 0o700))
	releaseLock, err := db.LockProject(ctx, p.dataDir, db.ProjectLockEngine, 0)
	require.NoError(t, err)
	timer := time.AfterFunc(3*time.Second, func() { _ = releaseLock() })
	t.Cleanup(func() {
		if timer.Stop() {
			_ = releaseLock()
		}
	})

	start := time.Now()
	h, err := p.ensure(ctx)
	require.NoError(t, err)
	require.True(t, h.Started)
	require.GreaterOrEqual(t, time.Since(start), 2900*time.Millisecond, "ensure waited for the old engine")

	stateDir, err := serverctl.EngineStateDir(p.socket)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(stateDir, "start-error.json"), "the new engine never met the lock")
}

// TestEnsureGivesUpOnStuckOldEngine: the lock stays held past the ensure
// timeout, so ensure fails with the lock's message and the holder's PID,
// and starts nothing.
func TestEnsureGivesUpOnStuckOldEngine(t *testing.T) {
	p := newEngineProject(t, "")
	ctx := t.Context()
	require.NoError(t, os.MkdirAll(p.dataDir, 0o700))
	releaseLock, err := db.LockProject(ctx, p.dataDir, db.ProjectLockEngine, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = releaseLock() })

	opts := p.opts()
	opts.Timeout = time.Second
	start := time.Now()
	_, err = p.ensureWith(ctx, opts)
	require.ErrorIs(t, err, serverctl.ErrProjectLocked)
	require.EqualError(t, err, fmt.Sprintf(db.MsgOtherEngineHoldsProject, os.Getpid()))
	require.Less(t, time.Since(start), 5*time.Second, "ensure gives up at its timeout")

	stateDir, err := serverctl.EngineStateDir(p.socket)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(stateDir, "engine.log"), "no engine was started")
}

// TestEnsureConcurrentStartsOneEngine: five ensures at once take turns on
// start.lock, so exactly one starts the engine and all get its PID.
func TestEnsureConcurrentStartsOneEngine(t *testing.T) {
	p := newEngineProject(t, "")
	ctx := t.Context()

	const n = 5
	handles := make([]serverctl.EngineHandle, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { handles[i], errs[i] = p.ensure(ctx) })
	}
	wg.Wait()

	started := 0
	for i := range n {
		require.NoError(t, errs[i])
		require.Equal(t, handles[0].PID, handles[i].PID, "one engine")
		if handles[i].Started {
			started++
		}
	}
	require.Equal(t, 1, started, "exactly one ensure started it")
}

// TestEngineRefusedWhileLoneWindowOpen: the test holds the shared project
// lock, as a lone terminal window will from phase 7. `crush engine ensure`
// starts an engine, which tells the shared holder from another engine:
// ensure exits 4 with project_locked and the lone-window message.
func TestEngineRefusedWhileLoneWindowOpen(t *testing.T) {
	p := newEngineProject(t, "")
	ctx := t.Context()
	require.NoError(t, os.MkdirAll(p.dataDir, 0o700))
	releaseLock, err := db.LockProject(ctx, p.dataDir, db.ProjectLockAlone, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = releaseLock() })

	exe, err := os.Executable()
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	ensure := exec.CommandContext(ctx, exe, "engine", "ensure", "--cwd", p.cwd)
	ensure.Stdout, ensure.Stderr = &stdout, &stderr
	err = ensure.Run()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "ensure must fail: %v; stderr: %s", err, stderr.String())
	require.Equal(t, 4, exitErr.ExitCode(), "stderr: %s", stderr.String())

	var out struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &out), "stdout: %s", stdout.String())
	require.Equal(t, "project_locked", out.Error.Code)
	require.Equal(t, db.MsgLoneWindowHoldsProject, out.Error.Message)
}
