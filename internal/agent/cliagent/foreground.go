package cliagent

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// foregroundCalls exposes each live CLI's pending shell calls to the
// background process list. Tool results remove calls once the command
// finishes or the CLI hands it back running in the background.
var foregroundCalls sync.Map // CLI process ID -> *openCalls

// IsForegroundCommand reports whether args belongs to a shell call cliPID
// is still waiting on. The start time distinguishes older background jobs
// that ran the same command from a new foreground call.
func IsForegroundCommand(cliPID int, args []string, started time.Time) bool {
	v, ok := foregroundCalls.Load(cliPID)
	if !ok {
		return false
	}
	for _, c := range v.(*openCalls).list() {
		if !started.Before(c.at.Add(-500*time.Millisecond)) && c.runBy(args) {
			return true
		}
	}
	return false
}

// runBy reports whether a process with these arguments runs the call: as a
// shell's script, or run directly with the words the shell would give it.
func (c openCall) runBy(args []string) bool {
	if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, c.command) }) {
		return true
	}
	if len(c.words) == 0 || len(args) < len(c.words) {
		return false
	}
	return filepath.Base(args[0]) == filepath.Base(c.words[0]) && slices.Equal(args[1:len(c.words)], c.words[1:])
}
