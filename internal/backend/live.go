package backend

import (
	"context"
	"errors"
	"sync"

	"github.com/charmbracelet/crush/internal/agent"
)

// Errors of piece B's routes (engine-design.md, section 0.1).
var (
	// ErrHelperNotFound: the helper is not in the chat.
	ErrHelperNotFound = errors.New("helper not found in this chat")
	// ErrJobNotFound: the job is not in the chat's list.
	ErrJobNotFound = errors.New("job not found in this chat")
	// ErrBadEnvName: a name to check is not a bare environment variable
	// name, or there are too many.
	ErrBadEnvName = errors.New("environment variable names must match ^[A-Za-z_][A-Za-z0-9_]{0,127}$, at most 64 of them")
	// ErrMemoryLimitInvalid wraps the memory-limit dialog's validation text.
	ErrMemoryLimitInvalid = errors.New("invalid memory limit")
)

// liveState is piece B's state per workspace (live work, ownership and the
// smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9). Its zero value is
// ready to use.
type liveState struct {
	mu sync.Mutex
	// prompts maps a submission ID to its app and chat; an entry goes at
	// its RunComplete.
	prompts map[string]promptInfo
	// seq is the acceptance counter for promptInfo.seq.
	seq uint64
	// lastSender maps a chat root to the app that last sent in it (only
	// for processes no hook saw start).
	lastSender map[string]string
	// owners maps a helper or job to its app.
	owners map[workKey]string
	// workChats maps a helper or job to the chat it works for.
	workChats map[workKey]string
	// workGens maps a helper or job to when it was last recorded (workGen
	// counts the records), so that forgetting ended work never drops a
	// record made while the agent was being asked whether it ended.
	workGens map[workKey]uint64
	workGen  uint64
	// lastInit is the app of the latest interactive InitAgent, and
	// initSeq its place among every workspace's (initSeqs).
	lastInit string
	initSeq  uint64
	// stopWatch ends the watchers of prompts and helpers that end; nil
	// until they start. watchMu makes a second starter wait until the first
	// subscribed.
	stopWatch context.CancelFunc
	watchMu   sync.Mutex
	// hooked is set once the work hook counts this workspace.
	hooked bool
	// closed is set at teardown: nothing is recorded after it.
	closed bool
	// pendingQ2 lists the apps that left as the last one. Their Q2 waits
	// for the teardown that follows: it runs only if an app joins first,
	// aborting that teardown (runPendingQ2).
	pendingQ2 []string

	// initMu makes InitAgent calls take turns; nonInteractive records that
	// the coordinator running is a non-interactive one, which a start makes
	// only while no interactive one runs.
	initMu         sync.Mutex
	nonInteractive bool
}

// promptInfo is one prompt an app sent.
type promptInfo struct {
	client    string // the app that sent it
	sessionID string // the chat it was sent to
	root      string // that chat's root
	seq       uint64 // acceptance order in this workspace
}

// workKey names a helper (by task ID) or a job (by job ID, or "pid:…" for a
// process no hook saw start).
type workKey struct {
	kind agent.WorkKind
	id   string
}

// CheckClientRegistered fails with ErrClientNotAttached when clientID holds
// no claim on the workspace (engine-design.md, section 0.1).
func (b *Backend) CheckClientRegistered(workspaceID, clientID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	if !isRegistered(ws, clientID) {
		return ErrClientNotAttached
	}
	return nil
}

// liveHooks are piece B's client hooks.
func (b *Backend) liveHooks() clientHooks {
	return clientHooks{
		joined:      b.liveJoined,
		messageSent: b.liveMessageSent,
		agentInit:   b.liveAgentInit,
		gone:        b.liveGone,
		teardown:    b.liveTeardown,
	}
}

// liveJoined starts the engine-wide work a workspace needs: the limits
// refresh at the first one, and the memory checks while any exists. A join
// also aborts the teardown the last app's departure began, so that app's Q2
// runs now.
func (b *Backend) liveJoined(ws *Workspace, _ string) {
	b.liveEngine().limitsOnce.Do(b.startLimits)
	b.startMemoryChecks()
	b.runPendingQ2(ws)
}

// liveTeardown stops recording and watching for the workspace, and the
// engine's memory checks with its last workspace.
func (b *Backend) liveTeardown(ws *Workspace) {
	l := &ws.live
	l.mu.Lock()
	l.closed = true
	stop := l.stopWatch
	l.stopWatch = nil
	hooked := l.hooked
	l.hooked = false
	l.prompts, l.lastSender, l.owners, l.workChats, l.workGens = nil, nil, nil, nil, nil
	l.pendingQ2 = nil
	l.mu.Unlock()
	if stop != nil {
		stop()
	}
	if hooked {
		removeWorkHook(ws)
	}
	b.stopMemoryChecksIfIdle()
}
