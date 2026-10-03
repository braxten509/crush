package uv

import (
	"bytes"
	"testing"
)

// Konsole marks lines a program scrolls, so its renderer never hard
// scrolls; other terminals keep the optimization.
func TestNoScrollOptimInKonsole(t *testing.T) {
	konsole := NewTerminalRenderer(new(bytes.Buffer), []string{"TERM=xterm-256color", "KONSOLE_VERSION=250800"})
	konsole.SetScrollOptim(true)
	if konsole.flags.Contains(tScrollOptim) {
		t.Fatal("Konsole must not hard scroll")
	}
	other := NewTerminalRenderer(new(bytes.Buffer), []string{"TERM=xterm-256color"})
	other.SetScrollOptim(true)
	if !other.flags.Contains(tScrollOptim) {
		t.Fatal("other terminals keep hard scrolling")
	}
}
