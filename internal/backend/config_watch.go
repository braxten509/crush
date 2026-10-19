package backend

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/ui/styles"
)

// Config watch (desktop design 5-6, section 4 M2; G-1). An engine reloads
// its config after its own writes only. So that a change made by another
// engine (another project's app) or by hand reaches this engine's apps,
// each engine checks once a second whether its config files or the themes
// folder changed; on a change it reloads and sends config_changed.

// configWatchEvery is how often an engine checks its config files.
var configWatchEvery = time.Second

// desktopEngine is the G56 engine-wide state.
type desktopEngine struct {
	watchOnce sync.Once
	// cpu is what the engine last applied of the agent CPU setting (G-16).
	cpu cpuState
}

// desktopEngines maps each Backend to its desktopEngine.
var desktopEngines sync.Map // *Backend → *desktopEngine

// desktopEngine returns b's G56 engine-wide state, made on first use and
// forgotten when b's context ends.
func (b *Backend) desktopEngine() *desktopEngine {
	if e, ok := desktopEngines.Load(b); ok {
		return e.(*desktopEngine)
	}
	e, loaded := desktopEngines.LoadOrStore(b, &desktopEngine{})
	if !loaded {
		if done := b.engineContext().Done(); done != nil {
			go func() {
				<-done
				desktopEngines.Delete(b)
			}()
		}
	}
	return e.(*desktopEngine)
}

// desktopHooks are the G56 client hooks.
func (b *Backend) desktopHooks() clientHooks {
	return clientHooks{joined: b.desktopJoined}
}

// desktopJoined starts the engine's config watch at its first workspace.
func (b *Backend) desktopJoined(ws *Workspace, _ string) {
	b.desktopEngine().watchOnce.Do(func() { go b.watchConfig(b.engineContext()) })
	b.applyCPUAffinity(ws)
}

// watchConfig checks the config files and the themes folder every
// configWatchEvery until ctx ends.
func (b *Backend) watchConfig(ctx context.Context) {
	ticker := time.NewTicker(configWatchEvery)
	defer ticker.Stop()
	themes := themesFolderState()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := themesFolderState()
		themesChanged := now != themes
		themes = now
		for _, ws := range b.workspaces.Seq2() {
			changed := themesChanged
			if ws.Cfg != nil && ws.Cfg.ChangedOnDisk() {
				if err := ws.Cfg.ReloadFromDisk(ctx); err != nil {
					slog.Debug("Config changed on disk but did not reload", "workspace", ws.ID, "error", err)
				} else {
					changed = true
				}
			}
			if changed {
				sendConfigChanged(ws)
				b.applyCPUAffinity(ws)
			}
		}
	}
}

// themesFolderState is the themes folder's entries (name, size and
// modification time), one per line, sorted; "" when it is missing.
func themesFolderState() string {
	dir := styles.ThemeDirs()[0]
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %d %d", e.Name(), info.Size(), info.ModTime().UnixNano()))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
