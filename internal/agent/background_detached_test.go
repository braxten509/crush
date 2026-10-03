package agent

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetachedCompletionTracksIdentityAndFailedScans(t *testing.T) {
	started := time.Now()
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

func TestDetachedNotificationDoesNotInventExitStatus(t *testing.T) {
	start := time.Now()
	notice := detachedNotification(detachedKey{77, start}, detachedProc{command: "sleep 10"}, start.Add(10*time.Second))
	require.Contains(t, notice, "<status>ended</status>")
	require.Contains(t, notice, "can't see its exit status")
}
