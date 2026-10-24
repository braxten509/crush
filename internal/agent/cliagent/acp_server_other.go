//go:build !linux

package cliagent

// openCodeSteerServer is Linux-only; elsewhere a queued OpenCode prompt
// waits for the turn to end.
func openCodeSteerServer(*proc) (string, string) { return "", "" }
