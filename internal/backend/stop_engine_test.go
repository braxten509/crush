package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStopEngineEndsBusyEngine: the engine reports the chats and helpers it
// runs, and the desktop's "Stop engine" (owner, 2026-10-27) ends it although
// it is busy, with nothing saved for the next engine to resume: no helper,
// and no chat waiting for its usage limit to reset.
func TestStopEngineEndsBusyEngine(t *testing.T) {
	r := newLiveRig(t)
	var shutdowns atomic.Int32
	r.b.shutdownFn = func() { shutdowns.Add(1) }
	a := newClientID(t)
	p := r.project(t)
	ws := r.open(t, p, a)
	c := r.chat(t, ws, "C")
	helper := r.spawnHelper(t, ws, a, c, "s1")
	// A chat of this engine waiting for its usage limit to reset.
	pending := filepath.Join(p.dataDir, "pending-work.json")
	wait, _ := json.Marshal(map[string]any{"waits": []map[string]any{
		{"session_id": c, "At": time.Now().Add(time.Hour), "kind": "limit", "owner": os.Getpid()},
	}})
	require.NoError(t, os.WriteFile(pending, wait, 0o600))

	info := r.b.EngineInfo("")
	require.Equal(t, 1, info.BusyChats, "the helper's own chat is not counted")
	require.Equal(t, 1, info.RunningHelpers)

	r.b.StopEngine()

	require.Equal(t, int32(1), shutdowns.Load(), "the server shuts down although it was busy")
	require.True(t, r.b.Closing())
	require.Empty(t, r.b.ListWorkspaces())
	r.model.waitHeld(t, "s1", 0)
	r.model.waitHeld(t, "s1-helper", 0)
	_, saved := savedEntry(t, p, "tasks.json", helper.ID)
	require.False(t, saved, "a stopped helper is not saved for the next engine")
	var left struct {
		Waits []json.RawMessage `json:"waits"`
	}
	if data, err := os.ReadFile(pending); !os.IsNotExist(err) { // emptied, the file goes
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &left))
	}
	require.Empty(t, left.Waits, "a chat waiting for its limit does not continue in the next engine")
	time.Sleep(300 * time.Millisecond)
	require.Zero(t, r.model.noticeCount(), "the helper's end started no turn")
}
