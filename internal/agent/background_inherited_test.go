package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/procs"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/stretchr/testify/require"
)

// Started from another Crush's job, every process here inherits that job's
// markers: they name the other Crush's job and shell, so they mark nothing
// here, while the markers this Crush sets still count.
func TestInheritedJobMarkersMarkNothing(t *testing.T) {
	oldJob, oldShell := inheritedJob, inheritedShell
	inheritedJob, inheritedShell = "outer-job", "outer-shell"
	t.Cleanup(func() { inheritedJob, inheritedShell = oldJob, oldShell })

	env := func(vars ...string) [][]byte {
		out := make([][]byte, len(vars))
		for i, v := range vars {
			out[i] = []byte(v)
		}
		return out
	}
	cli := procFromEntry(procs.Entry{Env: env(managedJobEnv+"=outer-job", tools.NativeShellEnv+"=outer-shell")})
	require.Empty(t, cli.managedJobID, "the outer Crush's job marks nothing here")
	require.Empty(t, cli.nativeShellID, "the outer Crush's shell marks nothing here")

	own := procFromEntry(procs.Entry{Env: env(managedJobEnv+"=outer-job", tools.NativeShellEnv+"=outer-shell", managedJobEnv+"=job", tools.NativeShellEnv+"=shell")})
	require.Equal(t, "job", own.managedJobID)
	require.Equal(t, "shell", own.nativeShellID)
}
