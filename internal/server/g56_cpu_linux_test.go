//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestCPUAffinityRoute: turning the agent CPUs on saves the setting and
// moves the engine onto options.agent_cpus; turning them off moves it back
// onto every CPU it may use (G-16).
func TestCPUAffinityRoute(t *testing.T) {
	var start unix.CPUSet
	require.NoError(t, unix.SchedGetaffinity(0, &start))
	var allowed []int
	for cpu := range 1024 {
		if start.IsSet(cpu) {
			allowed = append(allowed, cpu)
		}
	}
	require.NotEmpty(t, allowed)
	t.Cleanup(func() { _, _ = cpuaffinity.Apply(cpuaffinity.FormatList(allowed)) })

	g := newG56Workspace(t)
	u := newUnixCaller(t, g)
	store := mustWorkspace(t, g).Cfg
	agentCPUs := cpuaffinity.FormatList(allowed[:1])
	require.NoError(t, store.SetConfigField(config.ScopeGlobal, "options.agent_cpus", agentCPUs))

	liveError(t, g.h, http.MethodPost, "/v1/cpu-affinity", proto.CPUAffinityRequest{Enabled: true}, http.StatusForbidden, "local_only", "")
	require.False(t, store.Config().Options.AgentCPUsEnabled, "a TCP request changes nothing")

	on := unixJSON[proto.CPUAffinity](t, u, http.MethodPost, "/v1/cpu-affinity", proto.CPUAffinityRequest{Enabled: true}, http.StatusOK)
	require.Equal(t, agentCPUs, on.CPUs)
	require.True(t, store.Config().Options.AgentCPUsEnabled, "the setting is saved")
	var now unix.CPUSet
	require.NoError(t, unix.SchedGetaffinity(0, &now))
	require.Equal(t, 1, now.Count(), "the engine runs on the agent CPUs only")
	require.True(t, now.IsSet(allowed[0]))

	off := unixJSON[proto.CPUAffinity](t, u, http.MethodPost, "/v1/cpu-affinity", proto.CPUAffinityRequest{Enabled: false}, http.StatusOK)
	require.Empty(t, off.CPUs)
	require.False(t, store.Config().Options.AgentCPUsEnabled)
	require.NoError(t, unix.SchedGetaffinity(0, &now))
	for _, cpu := range allowed {
		require.True(t, now.IsSet(cpu), "CPU %d is usable again", cpu)
	}

	require.NoError(t, store.SetConfigField(config.ScopeGlobal, "options.agent_cpus", "100000"))
	code, out := u.call(t, http.MethodPost, "/v1/cpu-affinity", proto.CPUAffinityRequest{Enabled: true})
	require.Equal(t, http.StatusBadRequest, code, "%s", out)
	require.Contains(t, string(out), `"code":"bad_request"`)
}
