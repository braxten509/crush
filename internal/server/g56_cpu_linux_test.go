//go:build linux

package server

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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

// allowedCPUs lists the CPUs this test process may use and puts it back on
// all of them when the test ends.
func allowedCPUs(t *testing.T) []int {
	t.Helper()
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
	return allowed
}

// TestCPUConfigWritesMatchLoaderSpelling: over TCP, the config routes
// refuse a CPU write however the loader would read it (any case, in the
// global or the project file, as a key or inside a whole object); on the
// unix socket the same writes go through and move the engine (G-16).
func TestCPUConfigWritesMatchLoaderSpelling(t *testing.T) {
	allowed := allowedCPUs(t)
	g := newG56Workspace(t)
	u := newUnixCaller(t, g)
	store := mustWorkspace(t, g).Cfg
	require.NoError(t, store.SetConfigField(config.ScopeGlobal, "options.agent_cpus", cpuaffinity.FormatList(allowed[:1])))

	cfgPath := "/v1/workspaces/" + g.ws.ID + "/config/"
	writes := []proto.ConfigSetRequest{
		{Scope: config.ScopeGlobal, Key: "options.AGENT_CPUS_ENABLED", Value: true},
		{Scope: config.ScopeWorkspace, Key: "options.Agent_Cpus_Enabled", Value: true},
		{Scope: config.ScopeGlobal, Key: "OPTIONS", Value: map[string]any{"aGeNt_CpUs_EnAbLeD": true}},
	}
	for _, w := range writes {
		liveError(t, g.h, http.MethodPost, cfgPath+"set", w, http.StatusForbidden, "local_only", "")
		require.False(t, store.HasConfigField(w.Scope, w.Key), "%s: a refused write leaves the file alone", w.Key)
		require.False(t, store.Config().Options.AgentCPUsEnabled, "%s: a refused write changes nothing", w.Key)
	}

	for _, w := range writes {
		unixJSON[rpc.Empty](t, u, http.MethodPost, cfgPath+"set", w, http.StatusOK)
		require.True(t, store.Config().Options.AgentCPUsEnabled, "%s: the loader reads the setting", w.Key)
		var now unix.CPUSet
		require.NoError(t, unix.SchedGetaffinity(0, &now))
		require.Equal(t, 1, now.Count(), "%s: on the socket the write moves the engine", w.Key)

		liveError(t, g.h, http.MethodPost, cfgPath+"remove", proto.ConfigRemoveRequest{Scope: w.Scope, Key: w.Key},
			http.StatusForbidden, "local_only", "")
		unixJSON[rpc.Empty](t, u, http.MethodPost, cfgPath+"remove", proto.ConfigRemoveRequest{Scope: w.Scope, Key: w.Key}, http.StatusOK)
		require.False(t, store.Config().Options.AgentCPUsEnabled, "%s: removed on the socket", w.Key)
	}
}

// TestCPUConfigCheckHoldsTheLock: the CPU check is part of the locked
// write. A writer holding the config lock (as another process would)
// turns the CPUs on while a TCP request that would have changed nothing
// waits; the request then sees that change and is refused instead of
// undoing it (G-16).
func TestCPUConfigCheckHoldsTheLock(t *testing.T) {
	allowedCPUs(t)
	g := newG56Workspace(t)
	store := mustWorkspace(t, g).Cfg
	require.NoError(t, store.SetConfigField(config.ScopeGlobal, "options.agent_cpus_enabled", false))
	path := store.GlobalDataPath()

	release, err := lock.File(t.Context(), path+".lock")
	require.NoError(t, err)
	type answer struct {
		code int
		body []byte
	}
	done := make(chan answer, 1)
	go func() {
		code, body := liveCall(t, g.h, http.MethodPost, "/v1/workspaces/"+g.ws.ID+"/config/set",
			proto.ConfigSetRequest{Scope: config.ScopeGlobal, Key: "options.agent_cpus_enabled", Value: false})
		done <- answer{code, body}
	}()
	time.Sleep(300 * time.Millisecond) // the request is now waiting for the lock

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	changed, err := sjson.SetBytes(data, "options.agent_cpus_enabled", true)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, changed, 0o600))
	release()

	got := <-done
	require.Equal(t, http.StatusForbidden, got.code, "%s", got.body)
	require.Contains(t, string(got.body), `"code":"local_only"`)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(data, "options.agent_cpus_enabled").Bool(), "the other writer's change stands")
}
