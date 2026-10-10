package vt

import "testing"

// Prompts like Powerlevel10k save the cursor, hide it, restore it and show
// it again. Restoring must not bring back the old visibility unannounced,
// or the last reported visibility stays hidden.
func TestRestoreCursorKeepsVisibility(t *testing.T) {
	e := NewEmulator(20, 5)
	visible := true
	e.SetCallbacks(Callbacks{CursorVisibility: func(v bool) { visible = v }})

	_, _ = e.Write([]byte("\x1b7\x1b[?25l\x1b8\x1b[?25h"))

	if !visible {
		t.Fatal("reported cursor hidden after it was shown again")
	}
	if e.scr.cur.Hidden {
		t.Fatal("cursor hidden after it was shown again")
	}
}
