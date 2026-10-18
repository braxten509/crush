package backend

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
)

// Ownership (Q2, engine-design.md 5.5): each prompt, helper and job records
// the app that started it, so that when an app leaves, only its own work
// stops, and work another open app views keeps going under that app.

// maxPrompts bounds liveState.prompts; the entry with the lowest seq goes
// first.
const maxPrompts = 4096

// initSeqs orders the interactive InitAgent calls of every workspace in the
// process, for the owner of a resumed helper.
var initSeqs atomic.Uint64

// workHooks lists the workspaces whose apps own work. The work hook is
// process-wide, so it is installed while the list is not empty.
var workHooks struct {
	sync.Mutex
	ws map[*Workspace]struct{}
}

// addWorkHook counts ws for the work hook, installing it with the first.
func addWorkHook(ws *Workspace) {
	workHooks.Lock()
	defer workHooks.Unlock()
	if workHooks.ws == nil {
		workHooks.ws = make(map[*Workspace]struct{})
	}
	workHooks.ws[ws] = struct{}{}
	if len(workHooks.ws) == 1 {
		agent.SetWorkStartHook(liveWorkStarted)
	}
}

// removeWorkHook stops counting ws, uninstalling the hook with the last.
func removeWorkHook(ws *Workspace) {
	workHooks.Lock()
	defer workHooks.Unlock()
	if _, ok := workHooks.ws[ws]; !ok {
		return
	}
	delete(workHooks.ws, ws)
	if len(workHooks.ws) == 0 {
		agent.SetWorkStartHook(nil)
	}
}

// liveWorkStarted is the work hook. A resumed helper belongs to the app of
// the latest interactive InitAgent; other work to the app whose prompt
// started the running turn of its chat, or to no app.
func liveWorkStarted(w agent.WorkStart) string {
	workHooks.Lock()
	list := slices.Collect(maps.Keys(workHooks.ws))
	workHooks.Unlock()
	key := workKey{kind: w.Kind, id: w.ID}
	if w.Resumed {
		var newest *Workspace
		var seq uint64
		for _, ws := range list {
			ws.live.mu.Lock()
			if !ws.live.closed && (newest == nil || ws.live.initSeq > seq) {
				newest, seq = ws, ws.live.initSeq
			}
			ws.live.mu.Unlock()
		}
		if newest == nil {
			return ""
		}
		l := &newest.live
		l.mu.Lock()
		defer l.mu.Unlock()
		l.recordLocked(key, w.SessionID, l.lastInit)
		return l.lastInit
	}
	for _, ws := range list {
		owner, running := turnOwner(ws, w.SessionID)
		if !running {
			continue
		}
		ws.live.mu.Lock()
		ws.live.recordLocked(key, w.SessionID, owner)
		ws.live.mu.Unlock()
		return owner
	}
	return ""
}

// recordLocked stores the owner of a helper or job and the chat it works
// for. Callers hold l.mu.
func (l *liveState) recordLocked(key workKey, sessionID, owner string) {
	if l.closed {
		return
	}
	if l.owners == nil {
		l.owners = make(map[workKey]string)
		l.workChats = make(map[workKey]string)
	}
	l.owners[key] = owner
	l.workChats[key] = sessionID
}

// coordinator returns the workspace's agent coordinator, or nil.
func (w *Workspace) coordinator() agent.Coordinator {
	if w.App == nil {
		return nil
	}
	return w.AgentCoordinator
}

// queuedIDs lists the submission IDs QueuedPromptSummaries lists for the
// chat: handed to an agent CLI, waiting through an interrupt, or queued.
func queuedIDs(c agent.Coordinator, sessionID string) []string {
	if c == nil {
		return nil
	}
	var ids []string
	for _, q := range agent.QueueSummaries(c, sessionID) {
		ids = append(ids, q.SubmissionID)
	}
	return ids
}

// runningPromptsLocked lists the chat's running prompts, oldest first: its
// entries in prompts that queued does not list. Callers hold l.mu.
func (l *liveState) runningPromptsLocked(sessionID string, queued []string) []promptInfo {
	var out []promptInfo
	for id, p := range l.prompts {
		if p.sessionID == sessionID && !slices.Contains(queued, id) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b promptInfo) int { return cmp.Compare(a.seq, b.seq) })
	return out
}

// turnOwner returns the app whose prompt started the chat's running turn,
// "" for a turn with no known prompt. running reports whether a turn runs.
// It reads the agent before it takes l.mu.
func turnOwner(ws *Workspace, sessionID string) (owner string, running bool) {
	c := ws.coordinator()
	if c == nil || !c.IsSessionBusy(sessionID) {
		return "", false
	}
	queued := queuedIDs(c, sessionID)
	ws.live.mu.Lock()
	defer ws.live.mu.Unlock()
	if run := ws.live.runningPromptsLocked(sessionID, queued); len(run) > 0 {
		return run[0].client, true
	}
	return "", true
}

// isRegistered reports whether clientID holds a claim on ws.
func isRegistered(ws *Workspace, clientID string) bool {
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	_, ok := ws.clients[clientID]
	return ok
}

// liveMessageSent records the app that sent a prompt. An unnamed or
// unregistered app records nothing.
func (b *Backend) liveMessageSent(ws *Workspace, clientID string, msg proto.AgentMessage) {
	if clientID == "" || msg.SubmissionID == "" || !isRegistered(ws, clientID) {
		return
	}
	// A prompt sent before the first interactive InitAgent still needs its
	// RunComplete; the watcher subscribes before the prompt is stored.
	b.watchCompletions(ws)
	root, _ := rootSessionID(context.Background(), ws, msg.SessionID)
	l := &ws.live
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if l.prompts == nil {
		l.prompts = make(map[string]promptInfo)
		l.lastSender = make(map[string]string)
	}
	if _, ok := l.prompts[msg.SubmissionID]; !ok && len(l.prompts) >= maxPrompts {
		oldest := ""
		for id, p := range l.prompts {
			if oldest == "" || p.seq < l.prompts[oldest].seq {
				oldest = id
			}
		}
		slog.Debug("Too many open prompts; forgetting the oldest one's app", "submission_id", oldest)
		delete(l.prompts, oldest)
	}
	l.seq++
	l.prompts[msg.SubmissionID] = promptInfo{client: clientID, sessionID: msg.SessionID, root: root, seq: l.seq}
	l.lastSender[root] = clientID
}

// watchCompletions starts the completion watcher once: it forgets each
// prompt at its RunComplete. Teardown stops it.
func (b *Backend) watchCompletions(ws *Workspace) {
	if ws.App == nil || ws.RunCompletions() == nil {
		return
	}
	l := &ws.live
	l.watchMu.Lock()
	defer l.watchMu.Unlock()
	l.mu.Lock()
	if l.closed || l.stopWatch != nil {
		l.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.stopWatch = cancel
	l.mu.Unlock()
	events := ws.RunCompletions().Subscribe(ctx)
	go func() {
		for ev := range events {
			if id := ev.Payload.SubmissionID; id != "" {
				l.mu.Lock()
				delete(l.prompts, id)
				l.mu.Unlock()
			}
		}
	}()
}

// liveAgentInit records the app of an interactive InitAgent, for resumed
// helpers, and counts the workspace for the work hook.
func (b *Backend) liveAgentInit(ws *Workspace, clientID string, interactive bool) {
	if !interactive {
		return
	}
	b.watchCompletions(ws)
	registered := clientID != "" && isRegistered(ws, clientID)
	l := &ws.live
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if registered {
		l.lastInit = clientID
		l.initSeq = initSeqs.Add(1)
	}
	if !l.hooked {
		l.hooked = true
		// Under l.mu, so teardown, which removes it under l.mu, cannot
		// miss it. The hook itself never holds workHooks while it takes
		// l.mu.
		addWorkHook(ws)
	}
}

// liveGone runs Q2 for the app that left; while other apps stay, in the
// background. For the last app it runs here, before the teardown that
// follows: an app that joins before that teardown takes the backend's lock
// aborts it, and the leaver's work must not run on with no owner. Run first,
// it is done before a later workspace can resume a helper under the same
// task ID; when the teardown goes ahead, the teardown stops the rest.
func (b *Backend) liveGone(ws *Workspace, clientID string, reason GoneReason, workspaceEmpty bool) {
	slog.Debug("An app left a workspace", "workspace", ws.ID, "client", clientID, "reason", reason, "last", workspaceEmpty)
	if workspaceEmpty {
		b.applyQ2(ws, clientID)
		return
	}
	go b.applyQ2(ws, clientID)
}

// viewers maps each chat root that another open app shows to that app.
// With two apps on one root, the lowest client ID wins.
func viewers(ctx context.Context, ws *Workspace, leaving string) map[string]string {
	ws.clientsMu.Lock()
	shown := make(map[string]string)
	for id, cs := range ws.clients {
		if id != leaving && cs.currentSessionID != "" {
			shown[id] = cs.currentSessionID
		}
	}
	ws.clientsMu.Unlock()
	out := make(map[string]string)
	for id, sessionID := range shown {
		root, _ := rootSessionID(ctx, ws, sessionID)
		if cur, ok := out[root]; !ok || id < cur {
			out[root] = id
		}
	}
	return out
}

// setOwner hands a helper or job to another app.
func (l *liveState) setOwner(key workKey, sessionID, owner string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recordLocked(key, sessionID, owner)
}

// forget drops a helper or job that ended.
func (l *liveState) forget(key workKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.owners, key)
	delete(l.workChats, key)
}

// jobKey names a listed job: by its job ID, or for a process no hook saw
// start, by its PID and start time.
func jobKey(p agent.Process) workKey {
	if p.JobID != "" {
		return workKey{kind: agent.WorkJob, id: p.JobID}
	}
	return workKey{kind: agent.WorkJob, id: fmt.Sprintf("pid:%d:%d", p.PID, p.Started.UnixNano())}
}

// runningTask finds the running helper id among a chat's helpers.
func runningTask(sessionID, id string) (agent.Task, bool) {
	for _, t := range agent.SessionTasks(sessionID) {
		if t.ID == id && t.Status == agent.TaskRunning {
			return t, true
		}
	}
	return agent.Task{}, false
}

// applyQ2 ends the work of the app that left, except work another open app
// views, which that app takes over (engine-design.md 5.5). Helpers and jobs
// go first, so their end cannot start a turn in a chat whose turn is about
// to stop. It holds no lock while it calls the agent.
func (b *Backend) applyQ2(ws *Workspace, leaving string) {
	if isRegistered(ws, leaving) {
		// The app came back before this ran.
		return
	}
	ctx := context.Background()
	root := func(sessionID string) string {
		r, _ := rootSessionID(ctx, ws, sessionID)
		return r
	}
	shown := viewers(ctx, ws, leaving)
	l := &ws.live

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	var helpers []workKey
	chats := make(map[workKey]string)
	for key, owner := range l.owners {
		if key.kind == agent.WorkHelper && owner == leaving {
			helpers = append(helpers, key)
			chats[key] = l.workChats[key]
		}
	}
	l.mu.Unlock()

	for _, key := range helpers {
		t, ok := runningTask(chats[key], key.id)
		if !ok {
			l.forget(key)
			continue
		}
		if viewer, ok := shown[root(t.SessionID)]; ok {
			l.setOwner(key, t.SessionID, viewer)
			agent.SetTaskStartedBy(key.id, viewer)
			continue
		}
		if err := agent.InterruptTask(key.id); err != nil {
			slog.Debug("Could not interrupt a helper of an app that left", "task", key.id, "error", err)
		}
		l.forget(key)
	}

	for _, p := range agent.BackgroundProcesses() {
		key := jobKey(p)
		chatRoot := root(p.Session)
		l.mu.Lock()
		owner, known := l.owners[key]
		sender := l.lastSender[chatRoot]
		l.mu.Unlock()
		mine := known && owner == leaving
		if !known {
			if turn, running := turnOwner(ws, p.Session); running {
				mine = turn == leaving
			} else {
				mine = sender == leaving
			}
		}
		if !mine {
			continue
		}
		if viewer, ok := shown[chatRoot]; ok {
			l.setOwner(key, p.Session, viewer)
			continue
		}
		if err := agent.InterruptJob(p); err != nil {
			slog.Warn("Could not stop a job of an app that left", "job", p.JobID, "pid", p.PID, "error", err)
		}
		l.forget(key)
	}

	l.mu.Lock()
	chatsWithPrompts := make(map[string]bool)
	for _, p := range l.prompts {
		if p.client == leaving {
			chatsWithPrompts[p.sessionID] = true
		}
	}
	l.mu.Unlock()
	for _, chat := range slices.Sorted(maps.Keys(chatsWithPrompts)) {
		if viewer, ok := shown[root(chat)]; ok {
			l.mu.Lock()
			for id, p := range l.prompts {
				if p.client == leaving && p.sessionID == chat {
					p.client = viewer
					l.prompts[id] = p
				}
			}
			l.mu.Unlock()
			continue
		}
		stopOwnedPrompts(ws, chat, leaving)
	}
}

// stopOwnedPrompts drops the leaving app's prompts in one chat and
// interrupts the chat's turn when only its prompts run (agent.StopOwned).
// The lock order is the chat's dispatch mutex, then l.mu.
func stopOwnedPrompts(ws *Workspace, chat, leaving string) {
	c := ws.coordinator()
	stopper, ok := c.(agent.OwnedStopper)
	if !ok {
		return
	}
	l := &ws.live
	var owned []string
	stopper.StopOwned(chat, func(id string) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		if p, ok := l.prompts[id]; ok && p.client == leaving {
			owned = append(owned, id)
			return true
		}
		return false
	}, func(queued []string) bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		running := l.runningPromptsLocked(chat, queued)
		return len(running) > 0 && !slices.ContainsFunc(running, func(p promptInfo) bool { return p.client != leaving })
	})
	// Their cancelled RunComplete would forget the dropped ones too. A
	// prompt still queued (a batch with another app's prompt) stays.
	still := queuedIDs(c, chat)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, id := range owned {
		if !slices.Contains(still, id) {
			delete(l.prompts, id)
		}
	}
}
