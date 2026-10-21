package backend

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Usage limits (engine-design.md 5.9 e). Limits belong to the account, so
// every app gets each change.

// limitsHooked lists the backends whose apps get limits events. The
// limits hook is process-wide, so it is installed while the list is not
// empty.
var limitsHooked struct {
	sync.Mutex
	backends map[*Backend]struct{}
}

// startLimits sends limits events from now on, and refreshes every AI
// tool's limits once, at the engine's first workspace. Tests never ask the
// real tools' accounts: they replace the fetchers first.
func (b *Backend) startLimits() {
	limitsHooked.Lock()
	if limitsHooked.backends == nil {
		limitsHooked.backends = make(map[*Backend]struct{})
	}
	limitsHooked.backends[b] = struct{}{}
	if len(limitsHooked.backends) == 1 {
		cliagent.SetLimitsHook(sendLimits)
	}
	limitsHooked.Unlock()
	if testing.Testing() && !cliagent.LimitFetchersReplaced() {
		return
	}
	ctx := b.engineContext()
	for _, k := range cliagent.Snapshot() {
		go func() { _ = cliagent.RefreshLimitsNow(ctx, k.Kind) }()
	}
	for _, a := range b.extraAccounts() {
		go func() { _, _ = cliagent.RefreshAccountLimits(ctx, a.Kind, a.ID, true) }()
	}
}

// extraAccounts lists every options.accounts sign-in of b's workspaces.
func (b *Backend) extraAccounts() []config.Account {
	var out []config.Account
	for _, ws := range b.workspaces.Seq2() {
		if ws.Cfg == nil {
			continue
		}
		cfg := ws.Cfg.Config()
		if cfg == nil || cfg.Options == nil {
			continue
		}
		for _, a := range cfg.Options.Accounts {
			if !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// removeLimitsHook stops sending b's apps limits events.
func removeLimitsHook(b *Backend) {
	limitsHooked.Lock()
	defer limitsHooked.Unlock()
	if _, ok := limitsHooked.backends[b]; !ok {
		return
	}
	delete(limitsHooked.backends, b)
	if len(limitsHooked.backends) == 0 {
		cliagent.SetLimitsHook(nil)
	}
}

// sendLimits is the limits hook: every app gets the account's new state.
func sendLimits(kind catwalk.Type, account string) {
	snap := cliagent.Snapshot()
	i := slices.IndexFunc(snap, func(k cliagent.KindLimits) bool { return k.Kind == kind && k.Account == account })
	if i < 0 {
		return
	}
	ev := EngineEvent{Type: pubsub.PayloadTypeLimits, Kind: pubsub.UpdatedEvent, Payload: cliLimits(snap[i])}
	limitsHooked.Lock()
	backends := slices.Collect(maps.Keys(limitsHooked.backends))
	limitsHooked.Unlock()
	for _, b := range backends {
		for _, ws := range b.workspaces.Seq2() {
			b.SendAll(ws.ID, ev)
		}
	}
}

// cliLimits is the wire form of one AI tool's limits: ready once a reading
// arrived, error while there is none and the last refresh failed, checking
// otherwise.
func cliLimits(k cliagent.KindLimits) proto.CLILimits {
	out := proto.CLILimits{Kind: string(k.Kind), Account: k.Account, Refills: k.Refills, Error: k.Err, Windows: make([]proto.LimitWindow, 0, len(k.Limits))}
	switch {
	case !k.ReadAt.IsZero():
		out.State, out.ReadAt = "ready", k.ReadAt.UTC()
	case k.Err != "":
		out.State = "error"
	default:
		out.State = "checking"
	}
	for _, l := range k.Limits {
		out.Windows = append(out.Windows, proto.LimitWindow{Name: l.Name, Used: l.Used, ResetsAt: l.ResetsAt, Model: l.Model})
	}
	return out
}

// Limits returns every AI tool's limits as this engine last read them.
func (b *Backend) Limits() proto.LimitsSnapshot {
	return CurrentLimits()
}

// CurrentLimits returns every AI tool's limits as this process last read
// them, in their wire form.
func CurrentLimits() proto.LimitsSnapshot {
	snap := cliagent.Snapshot()
	out := proto.LimitsSnapshot{CLIs: make([]proto.CLILimits, 0, len(snap))}
	for _, k := range snap {
		out.CLIs = append(out.CLIs, cliLimits(k))
	}
	return out
}

// RefreshLimits starts a refresh of kind's limits, for every account of
// it, and returns at once; the results arrive as limits events. It reports
// false for a kind that reports no limits.
func (b *Backend) RefreshLimits(kind string) bool {
	if !slices.ContainsFunc(cliagent.Snapshot(), func(k cliagent.KindLimits) bool { return string(k.Kind) == kind }) {
		return false
	}
	ctx := b.engineContext()
	go func() { _ = cliagent.RefreshLimitsNow(ctx, catwalk.Type(kind)) }()
	for _, a := range b.extraAccounts() {
		if string(a.Kind) == kind {
			go func() { _, _ = cliagent.RefreshAccountLimits(ctx, a.Kind, a.ID, true) }()
		}
	}
	return true
}
