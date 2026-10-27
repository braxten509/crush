package agent

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetachedCompletionTracksIdentityAndFailedScans(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	job := proc{pid: 77, ppid: 1, started: started, session: "owner", args: []string{"sleep", "10"}}
	seen := map[detachedKey]detachedProc{}
	require.Empty(t, completedDetached(seen, map[int]proc{77: job}))
	require.Len(t, seen, 1)
	require.Empty(t, completedDetached(seen, nil), "unavailable process table is not completion")
	require.Len(t, seen, 1)
	replacement := job
	replacement.started = started.Add(time.Second)
	ended := completedDetached(seen, map[int]proc{77: replacement})
	require.Equal(t, "owner", ended[detachedKey{77, started}].session)
	require.Len(t, seen, 1)
	require.Len(t, completedDetached(seen, map[int]proc{}), 1)
	require.Empty(t, completedDetached(seen, map[int]proc{}), "each exit is delivered once")
}

func TestDetachedCompletionIgnoresHelpersSubagentsAndOwnedShells(t *testing.T) {
	seen := map[detachedKey]detachedProc{}
	procs := map[int]proc{
		10: {pid: 10, ppid: os.Getpid(), session: "s"},
		11: {pid: 11, ppid: 10, session: "s"},
		12: {pid: 12, ppid: 1, session: ""},
		13: {pid: 13, ppid: 1, session: "s", busService: true},
		14: {pid: 14, ppid: 1, session: "s", nativeShellID: "managed"},
	}
	require.Empty(t, completedDetached(seen, procs))
	require.Empty(t, seen)
}

// Helpers a tool starts that outlive it for a moment (Chromium's crash
// reporter, an MCP server's npm launcher) were reported as ended background
// work (owner, 2026-10-27); only work that ran past the foreground limit is.
func TestShortLivedDetachedProcessesAreNotReported(t *testing.T) {
	seen := map[detachedKey]detachedProc{}
	quick := proc{pid: 80, ppid: 1, started: time.Now().Add(-2 * time.Second), session: "s", args: []string{"chrome_crashpad_handler"}}
	long := proc{pid: 81, ppid: 1, started: time.Now().Add(-time.Minute), session: "s", args: []string{"npm", "run", "dev"}}
	require.Empty(t, completedDetached(seen, map[int]proc{80: quick, 81: long}))
	ended := completedDetached(seen, map[int]proc{})
	require.Len(t, ended, 1)
	require.Contains(t, ended, detachedKey{81, long.started})
	require.Empty(t, seen, "the quick one is forgotten too")
}

func TestDetachedNotificationDoesNotInventExitStatus(t *testing.T) {
	start := time.Now()
	notice := detachedNotification(detachedKey{77, start}, detachedProc{command: "npm run dev"}, start.Add(10*time.Second))
	require.Contains(t, notice, "<status>ended</status>")
	require.Contains(t, notice, "can't see its exit status")
}

// A process's age is how long it was seen alive, not when its end was
// noticed: scans every 2 s would otherwise report a 9 s helper.
func TestDetachedAgeIsMeasuredWhileAlive(t *testing.T) {
	started := time.Now().Add(-20 * time.Second)
	seen := map[detachedKey]detachedProc{{82, started}: {session: "s", command: "helper", lastSeen: started.Add(8 * time.Second)}}
	require.Empty(t, completedDetached(seen, map[int]proc{}))
}
