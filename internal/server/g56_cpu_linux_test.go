//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
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

	// Nor can TCP reach the CPU setting through the ordinary config routes,
	// by any spelling, while other keys still work there.
	cfgPath := "/v1/workspaces/" + g.ws.ID + "/config/"
	for _, key := range []string{"options.agent_cpus_enabled", ":options.agent_cpus_enabled", "options"} {
		value := any(true)
		if key == "options" {
			value = map[string]any{"agent_cpus_enabled": true}
		}
		liveError(t, g.h, http.MethodPost, cfgPath+"set",
			proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: key, Value: value}, http.StatusForbidden, "local_only", "")
	}
	liveError(t, g.h, http.MethodPost, cfgPath+"remove",
		proto.ConfigRemoveRequest{Scope: config.ScopeGlobal, Key: "options.agent_cpus"}, http.StatusForbidden, "local_only", "")
	require.False(t, store.Config().Options.AgentCPUsEnabled, "a TCP config set changes nothing")
	require.Equal(t, agentCPUs, store.Config().Options.AgentCPUs, "a TCP config remove changes nothing")
	liveJSON[rpc.Empty](t, g.h, http.MethodPost, cfgPath+"set",
		proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.debug", Value: true}, http.StatusOK)
	liveJSON[rpc.Empty](t, g.h, http.MethodPost, cfgPath+"remove",
		proto.ConfigRemoveRequest{Scope: config.ScopeGlobal, Key: "options.debug"}, http.StatusOK)

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

	// The ordinary config routes move the engine too, whatever the key's
	// spelling: an equivalent path turns the CPUs on, a removal turns them
	// off.
	unixJSON[rpc.Empty](t, u, http.MethodPost, cfgPath+"set",
		proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: ":options.agent_cpus_enabled", Value: true}, http.StatusOK)
	require.True(t, store.Config().Options.AgentCPUsEnabled)
	require.NoError(t, unix.SchedGetaffinity(0, &now))
	require.Equal(t, 1, now.Count(), "an equivalent path moves the engine onto the agent CPUs")
	require.True(t, now.IsSet(allowed[0]))
	unixJSON[rpc.Empty](t, u, http.MethodPost, cfgPath+"remove",
		proto.ConfigRemoveRequest{Scope: config.ScopeGlobal, Key: "options.agent_cpus_enabled"}, http.StatusOK)
	require.False(t, store.Config().Options.AgentCPUsEnabled)
	require.NoError(t, unix.SchedGetaffinity(0, &now))
	for _, cpu := range allowed {
		require.True(t, now.IsSet(cpu), "after the removal, CPU %d is usable again", cpu)
	}

	require.NoError(t, store.SetConfigField(config.ScopeGlobal, "options.agent_cpus", "100000"))
	code, out := u.call(t, http.MethodPost, "/v1/cpu-affinity", proto.CPUAffinityRequest{Enabled: true})
	require.Equal(t, http.StatusBadRequest, code, "%s", out)
	require.Contains(t, string(out), `"code":"bad_request"`)
}
