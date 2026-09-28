package uv

import "testing"

// Held alt+backspace streams "\x1b\x7f" repeatedly with no pause; every
// complete sequence must be emitted without waiting for the input to go idle.
func TestHeldAltBackspaceNotBuffered(t *testing.T) {
	d := newEventScanner()
	buf := []byte("\x1b\x7f\x1b\x7f\x1b\x7f")
	n, events := d.scanEvents(buf, false)
	if len(events) != 2 || n != 4 {
		t.Fatalf("want 2 events and 4 bytes consumed (last one waits), got %d events, %d bytes", len(events), n)
	}
}
