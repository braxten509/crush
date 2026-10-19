package backend

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/proto"
)

// CPU affinity (desktop design 5-6, 2.13; G-16). The terminal pinned its
// own process tree; agents now run in the engine, so the engine pins
// itself: through the route, at its first workspace and after each config
// change, from options.agent_cpus_enabled and options.agent_cpus.

// ErrCPUAffinity wraps a failed move between CPUs; its text is the
// terminal's.
var ErrCPUAffinity = errors.New("cpu affinity")

// cpuState remembers what this engine last applied, so a config change
// that leaves the setting alone moves nothing.
type cpuState struct {
	mu      sync.Mutex
	applied string // "on:<cpus>", "off", or "" before the first apply
}

func (b *Backend) cpuState() *cpuState { return &b.desktopEngine().cpu }

// SetCPUAffinity turns the agent CPUs on or off as the terminal's toggle
// does: it saves options.agent_cpus_enabled, then moves the engine and
// everything it started. cpus is the list applied, "" when off.
func (b *Backend) SetCPUAffinity(enabled bool) (proto.CPUAffinity, error) {
	store := b.themeConfigStore()
	if store == nil {
		return proto.CPUAffinity{}, ErrWorkspaceNotFound
	}
	if err := store.SetConfigField(config.ScopeGlobal, "options.agent_cpus_enabled", enabled); err != nil {
		return proto.CPUAffinity{}, err
	}
	for _, ws := range b.workspaces.Seq2() {
		sendConfigChanged(ws)
	}
	cpus := ""
	if o := store.Config().Options; o != nil {
		cpus = o.AgentCPUs
	}
	st := b.cpuState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !enabled {
		if err := cpuaffinity.Reset(); err != nil {
			return proto.CPUAffinity{}, kindErr(ErrCPUAffinity, err)
		}
		st.applied = "off"
		return proto.CPUAffinity{}, nil
	}
	list, err := cpuaffinity.Apply(cpus)
	if err != nil {
		return proto.CPUAffinity{}, kindErr(ErrCPUAffinity, err)
	}
	st.applied = "on:" + cpus
	return proto.CPUAffinity{CPUs: list}, nil
}

// applyCPUAffinity applies ws's saved CPU setting when it differs from
// what the engine applied last. A first call with the setting off moves
// nothing: the engine started on every CPU.
func (b *Backend) applyCPUAffinity(ws *Workspace) {
	if ws == nil || ws.Cfg == nil || !cpuaffinity.Supported() {
		return
	}
	o := ws.Cfg.Config().Options
	if o == nil {
		return
	}
	want := "off"
	if o.AgentCPUsEnabled {
		want = "on:" + o.AgentCPUs
	}
	st := b.cpuState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.applied == want || (st.applied == "" && want == "off") {
		st.applied = want
		return
	}
	if want == "off" {
		if err := cpuaffinity.Reset(); err != nil {
			slog.Warn("Failed to move Crush back onto every CPU", "error", err)
			return
		}
	} else if _, err := cpuaffinity.Apply(o.AgentCPUs); err != nil {
		slog.Warn("Failed to move Crush onto agent CPUs", "cpus", o.AgentCPUs, "error", err)
		return
	}
	st.applied = want
}
