# Local paintbrush patch

Source: github.com/jordanella/go-ansi-paintbrush, version v0.0.0-20240728195301-b7ad996ecf3d.

Paint collects all worker results before rendering them. The original collector ran in a separate goroutine that Paint did not wait for, allowing incomplete output and concurrent reads and writes. Worker rendering remains parallel. Progress writes during collection use the existing mutex.

The root image tests exercise this path with the Go race detector. Package sources, the embedded font, and the upstream license are retained here.
