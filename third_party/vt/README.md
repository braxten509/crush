# Local terminal emulator patch

Source: github.com/charmbracelet/x/vt, v0.0.0-20261004011457-ad85c59fdf4e.

The closed flag uses atomic reads and a swap on Close. A session can close the input pipe while another goroutine reads it; SafeEmulator intentionally does not hold its drawing mutex during a blocking read. The upstream plain boolean made that normal shutdown a data race.

Upstream sources, tests, and license are preserved. Crush's sessionhost race tests cover concurrent input and shutdown.
