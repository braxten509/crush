//go:build !windows

package sessionhost

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// A session that asks the terminal questions without reading the answers,
// as a busy Crush does while it draws a form and the mouse moves, fills its
// input. Keys and clicks for it must still never hold up the host.
func TestBusySessionNeverBlocksTheHost(t *testing.T) {
	t.Parallel()
	l := launch{
		exe:  "/bin/sh",
		args: []string{"-c", `while :; do printf '\033[c\033[c\033[c\033[c\033[c\033[c\033[c\033[c'; done`},
		dir:  t.TempDir(),
		env:  os.Environ(),
	}
	s, err := startSession(1, l, 40, 10, nil, nil, func(tea.Msg) {}, func(*session) {})
	require.NoError(t, err)
	t.Cleanup(func() {
		kill(s.cmd.Process)
	})
	time.Sleep(500 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for range 200 {
			s.emu.SendMouse(uv.MouseMotionEvent{X: 3, Y: 3})
			s.emu.SendKey(uv.KeyPressEvent{Code: 'a'})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sending input to a busy session blocked")
	}
}
