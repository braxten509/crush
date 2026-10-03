package cmd

import (
	"io"

	uv "github.com/charmbracelet/ultraviolet"
)

// Konsole marks the lines that last scrolled with a bar in the left margin
// ("Highlight the lines coming into view", on by default), and only the next
// scroll moves it. Crush draws on the alternate screen, which never scrolls,
// so the mark left by the shell starting Crush stayed beside the bottom row
// for the whole session. Konsole's OSC 50 profile sequence switches it off
// for this tab only, without touching the saved profile.
const (
	konsoleHideScrollMarker = "\x1b]50;HighlightScrolledLines=false\a"
	konsoleShowScrollMarker = "\x1b]50;HighlightScrolledLines=true\a"
)

// hideKonsoleScrollMarker switches Konsole's scroll mark off while Crush
// runs and returns the function that switches it back on. out must be the
// terminal (tty reports whether it is).
func hideKonsoleScrollMarker(env uv.Environ, out io.Writer, tty bool) (restore func()) {
	if !tty || env.Getenv("KONSOLE_VERSION") == "" {
		return func() {}
	}
	_, _ = io.WriteString(out, konsoleHideScrollMarker)
	return func() { _, _ = io.WriteString(out, konsoleShowScrollMarker) }
}
