package backend

import (
	"errors"
	"log/slog"
	"strings"
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
// everything it started. cpus is the list applied, "" when off. The save
// and the move hold one lock, so no other CPU write lands between them.
func (b *Backend) SetCPUAffinity(enabled bool) (proto.CPUAffinity, error) {
	store := b.themeConfigStore()
	if store == nil {
		return proto.CPUAffinity{}, ErrWorkspaceNotFound
	}
	st := b.cpuState()
	st.mu.Lock()
	defer st.mu.Unlock()
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

// cpuSetting is ws's effective CPU setting: "on:<cpus>", "off", or ""
// when ws has no options.
func cpuSetting(ws *Workspace) string {
	if ws == nil || ws.Cfg == nil {
		return ""
	}
	o := ws.Cfg.Config().Options
	if o == nil {
		return ""
	}
	if o.AgentCPUsEnabled {
		return "on:" + o.AgentCPUs
	}
	return "off"
}

// writeConfig runs a config write through the ordinary config routes under
// the CPU lock, and applies the CPU setting when the write changed it, so
// the engine's CPUs follow those writes too, however the key was spelled.
func (b *Backend) writeConfig(ws *Workspace, write func() error) error {
	st := b.cpuState()
	st.mu.Lock()
	defer st.mu.Unlock()
	before := cpuSetting(ws)
	if err := write(); err != nil {
		return err
	}
	if cpuSetting(ws) != before {
		b.applyCPUAffinityLocked(ws)
	}
	return nil
}

// applyCPUAffinity applies ws's saved CPU setting when it differs from
// what the engine applied last. A first call with the setting off moves
// nothing: the engine started on every CPU.
func (b *Backend) applyCPUAffinity(ws *Workspace) {
	st := b.cpuState()
	st.mu.Lock()
	defer st.mu.Unlock()
	b.applyCPUAffinityLocked(ws)
}

// applyCPUAffinityLocked is [Backend.applyCPUAffinity] with the CPU lock
// held. It reads the setting under the lock, so the last apply sees the
// last write.
func (b *Backend) applyCPUAffinityLocked(ws *Workspace) {
	if !cpuaffinity.Supported() {
		return
	}
	want := cpuSetting(ws)
	if want == "" {
		return
	}
	cpus := strings.TrimPrefix(want, "on:")
	st := b.cpuState()
	if st.applied == want || (st.applied == "" && want == "off") {
		st.applied = want
		return
	}
	if want == "off" {
		if err := cpuaffinity.Reset(); err != nil {
			slog.Warn("Failed to move Crush back onto every CPU", "error", err)
			return
		}
	} else if _, err := cpuaffinity.Apply(cpus); err != nil {
		slog.Warn("Failed to move Crush onto agent CPUs", "cpus", cpus, "error", err)
		return
	}
	st.applied = want
}
