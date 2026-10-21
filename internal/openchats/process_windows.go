//go:build windows

package openchats

// Windows has no shared desktop app yet: no lock, and every signed-in app
// counts as running.
func lock(string) (func(), error) { return func() {}, nil }

func processStart(int) string { return "" }

func alive(int, string) bool { return true }
