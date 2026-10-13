# Crush's patches to Ultraviolet

Source: github.com/charmbracelet/ultraviolet, the version go.mod pins
(see `replace` there). Go can only patch a dependency through a full copy,
so this folder holds upstream's sources with the changes below. Run
`scripts/third-party-diff.sh ultraviolet` to see them exactly; that list
must match this file.

1. **Held alt+backspace** (`terminal_reader.go`, two places). An escape byte
   followed by one more byte waits for more input in case it starts a longer
   sequence. When further bytes already follow in the same read, the sequence
   is complete, so it no longer waits. Holding alt+backspace otherwise loses
   or garbles key presses. Marked `// crush:` in the code.
2. **Konsole scrolled-line bars** (`terminal_renderer.go`). Konsole marks
   every line a program scrolls with a bar in its left margin ("highlight
   scrolled lines"), which then sits beside redrawn rows. The renderer reads
   `KONSOLE_VERSION` and never turns on hard scroll optimizations there.

When updating the pinned version, copy the new upstream sources and apply
these changes again, or drop the copy if upstream has fixed both.
