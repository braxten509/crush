package sessionhost

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Run shows the host until its last session ends. Sessions still running
// when it stops anyway are asked to exit, and killed if they don't.
func Run(ctx context.Context, opts Options) error {
	h := New(opts)
	p := tea.NewProgram(h, tea.WithContext(ctx), tea.WithEnvironment(os.Environ()))
	h.SetSend(p.Send)
	_, err := p.Run()
	h.stopAll()
	if h.startErr != nil {
		return fmt.Errorf("could not start a Crush session: %w", h.startErr)
	}
	return err
}

func (h *Host) stopAll() {
	for _, s := range h.sessions {
		s.stop()
	}
	deadline := time.Now().Add(stopGrace + time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, s := range h.sessions {
			alive = alive || !s.exited.Load()
		}
		if !alive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// terminalEnv names variables that describe the real terminal. Sessions
// draw into the host's emulator instead, so they must not act on them.
var terminalEnv = []string{
	"TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
	"LC_TERMINAL", "LC_TERMINAL_VERSION", "VTE_VERSION", "WT_SESSION",
}

// terminalEnvPrefixes are the same for terminal-specific families.
var terminalEnvPrefixes = []string{"KONSOLE_", "KITTY_", "GHOSTTY_", "WEZTERM_", "ITERM_", "TMUX"}

// ChildEnviron returns base without the real terminal's variables and the
// names in drop, plus what a session inside the host needs.
func ChildEnviron(base []string, drop ...string) []string {
	env := make([]string, 0, len(base)+3)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if dropped(name, drop) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor", ChildEnv+"=1")
}

func dropped(name string, drop []string) bool {
	for _, d := range append(drop, terminalEnv...) {
		if name == d {
			return true
		}
	}
	for _, prefix := range terminalEnvPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
