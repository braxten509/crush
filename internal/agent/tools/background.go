package tools

import (
	"context"
	"sync"

	"github.com/charmbracelet/crush/internal/shell"
)

const NativeShellEnv = "CRUSH_NATIVE_SHELL"

// JobStarted is told about every job a bash call leaves running in the
// background, with the call's shell environment and the NativeShellEnv
// value its processes carry, so Crush can report on the job until it ends.
// It returns whether it took the job.
type JobStarted func(env []string, sessionID, marker string, job *shell.BackgroundShell) bool

type watchedJobKey struct{}

// WithWatchedJob marks a bash call whose caller reports on its background
// job itself, so JobStarted isn't told.
func WithWatchedJob(ctx context.Context) context.Context {
	return context.WithValue(ctx, watchedJobKey{}, true)
}

// Tell tells f about a job the bash call in ctx left running, unless the
// call's caller watches it itself. It reports whether f took the job.
func (f JobStarted) Tell(ctx context.Context, sessionID, marker string, job *shell.BackgroundShell) bool {
	if f == nil || GetContextValue(ctx, watchedJobKey{}, false) {
		return false
	}
	return f(GetContextValue(ctx, ShellEnvContextKey, []string(nil)), sessionID, marker, job)
}

// agentStops holds the jobs the agent's own job_kill call ended, so the
// agent isn't told about a stop it made.
var agentStops sync.Map

// MarkStoppedByAgent records that job_kill is ending the job.
func MarkStoppedByAgent(id string) {
	agentStops.Store(id, struct{}{})
}

// UnmarkStoppedByAgent forgets a stop job_kill failed to make.
func UnmarkStoppedByAgent(id string) {
	agentStops.Delete(id)
}

// StoppedByAgent reports, once, whether job_kill ended the job.
func StoppedByAgent(id string) bool {
	_, ok := agentStops.LoadAndDelete(id)
	return ok
}

var shellBackgrounds = struct {
	sync.Mutex
	sessions   map[string]map[chan struct{}]struct{}
	foreground map[string]struct{}
}{sessions: make(map[string]map[chan struct{}]struct{}), foreground: make(map[string]struct{})}

func RegisterShellBackground(sessionID, marker string, signal chan struct{}) func() {
	shellBackgrounds.Lock()
	if shellBackgrounds.sessions[sessionID] == nil {
		shellBackgrounds.sessions[sessionID] = make(map[chan struct{}]struct{})
	}
	shellBackgrounds.sessions[sessionID][signal] = struct{}{}
	shellBackgrounds.foreground[marker] = struct{}{}
	shellBackgrounds.Unlock()
	return func() {
		shellBackgrounds.Lock()
		defer shellBackgrounds.Unlock()
		delete(shellBackgrounds.sessions[sessionID], signal)
		delete(shellBackgrounds.foreground, marker)
		if len(shellBackgrounds.sessions[sessionID]) == 0 {
			delete(shellBackgrounds.sessions, sessionID)
		}
	}
}

func IsForegroundShell(id string) bool {
	shellBackgrounds.Lock()
	defer shellBackgrounds.Unlock()
	_, active := shellBackgrounds.foreground[id]
	return active
}

func BackgroundShell(sessionID string) bool {
	shellBackgrounds.Lock()
	defer shellBackgrounds.Unlock()
	active := false
	for signal := range shellBackgrounds.sessions[sessionID] {
		active = true
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	return active
}
