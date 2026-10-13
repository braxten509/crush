# Crush's patches to vt

Source: github.com/charmbracelet/x/vt, the version go.mod pins (see
`replace` there). Go can only patch a dependency through a full copy, so
this folder holds upstream's sources with the changes below, made for
Crush's terminal sessions (internal/ui/sessionhost). Run
`scripts/third-party-diff.sh vt` to see them exactly; that list must match
this file.

1. **Input queue instead of a pipe** (`input.go`, new; `emulator.go`, and
   every `e.pw` write in `csi.go`, `csi_mode.go`, `focus.go`, `handlers.go`,
   `key.go`, `osc.go`). Replies and key input go into an unbounded buffer, so
   a process that reads slowly can no longer block the emulator or drop
   bytes. Closing drains accepted input before EOF.
2. **Race-free close** (`emulator.go`). The closed flag is an atomic, so a
   session closing its input while another goroutine reads it is not a data
   race. SafeEmulator deliberately doesn't hold its drawing lock during a
   blocking read.
3. **Scrollback viewport** (`emulator.go` `DrawViewport`, `scrollback.go`
   running total, `terminal.go` interface additions `DrawViewport` and
   `ScrollbackState`). Sessions can draw the screen scrolled back by some
   lines and know how many rows have ever scrolled off.
4. **Mouse reporting query** (`mouse.go` `MouseReporting`, also in
   `terminal.go`). Tells whether the program asked for mouse events, as
   opposed to only choosing an encoding.
5. **Safer SafeEmulator** (`safe_emulator.go`). `WriteString`, copies of
   cells from `CellAt` instead of shared pointers, and locked setters for the
   default colors.
6. **Cursor restore like xterm** (`screen.go`). Restoring a saved cursor
   keeps the current cursor style, blinking and visibility, which saving
   doesn't include.

When updating the pinned version, copy the new upstream sources and apply
these changes again.
