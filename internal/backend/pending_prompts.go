package backend

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
)

// The kinds of answer a chat can wait for (proto.PromptPending.Kind and
// proto.Session.PendingPrompt).
const (
	promptKindPermission = "permission"
	promptKindQuestion   = "question"
)

// PendingPrompt reports what the chat waits for: "permission", "question"
// or "". A permission wins when both are pending. A helper's prompt counts
// for its parent chat.
func (b *Backend) PendingPrompt(ctx context.Context, workspaceID, sessionID string) string {
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return ""
	}
	switch {
	case b.promptPending(ctx, ws, sessionID, promptKindPermission):
		return promptKindPermission
	case b.promptPending(ctx, ws, sessionID, promptKindQuestion):
		return promptKindQuestion
	}
	return ""
}

// promptPending reports whether the workspace's pending prompt of the given
// kind belongs to sessionID, directly or through the asking session's
// ancestry, as republishPendingPrompts decides. Workspaces with a stubbed
// app (tests) have no pending prompts.
func (b *Backend) promptPending(ctx context.Context, ws *Workspace, sessionID, kind string) bool {
	if sessionID == "" || ws.App == nil {
		return false
	}
	switch kind {
	case promptKindPermission:
		if ws.Permissions == nil {
			return false
		}
		req, ok := ws.Permissions.ActiveRequest()
		return ok && b.sessionMatches(ctx, ws, req.SessionID, sessionID)
	case promptKindQuestion:
		if ws.Questions == nil {
			return false
		}
		req, ok := ws.Questions.Pending()
		return ok && b.sessionMatches(ctx, ws, req.SessionID, sessionID)
	}
	return false
}

// promptWatcher sends prompt_pending to every app of one workspace when a
// chat starts or stops waiting for an answer.
type promptWatcher struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// promptSources are the five feeds after which the watcher recomputes a
// chat's state: permission requests and notifications, question requests
// and notifications, and run completions (a cancelled prompt ends without a
// notification). A nil channel never delivers.
type promptSources struct {
	permissionRequests      <-chan pubsub.Event[permission.PermissionRequest]
	permissionNotifications <-chan pubsub.Event[permission.PermissionNotification]
	questionRequests        <-chan pubsub.Event[question.Request]
	questionNotifications   <-chan pubsub.Event[question.Notification]
	runCompletions          <-chan pubsub.Event[notify.RunComplete]
}

// startPromptWatcher starts ws's watcher at its first agent start. A
// workspace that is already torn down (or never was live) gets none, so the
// teardown hook always finds the watchers it must stop.
func (b *Backend) startPromptWatcher(ws *Workspace) {
	if ws.App == nil {
		return
	}
	b.promptWatchersMu.Lock()
	defer b.promptWatchersMu.Unlock()
	if _, ok := b.promptWatchers[ws]; ok {
		return
	}
	if live, ok := b.workspaces.Get(ws.ID); !ok || live != ws {
		return
	}
	parent := ws.ctx
	if parent == nil {
		parent = b.ctx
	}
	ctx, cancel := context.WithCancel(parent)
	// Subscribe before returning, so no prompt raised after the agent
	// started is missed.
	var sources promptSources
	if ws.Permissions != nil {
		sources.permissionRequests = ws.Permissions.Subscribe(ctx)
		sources.permissionNotifications = ws.Permissions.SubscribeNotifications(ctx)
	}
	if ws.Questions != nil {
		sources.questionRequests = ws.Questions.Subscribe(ctx)
		sources.questionNotifications = ws.Questions.SubscribeNotifications(ctx)
	}
	if rc := ws.RunCompletions(); rc != nil {
		sources.runCompletions = rc.Subscribe(ctx)
	}
	w := &promptWatcher{cancel: cancel, done: make(chan struct{})}
	if b.promptWatchers == nil {
		b.promptWatchers = make(map[*Workspace]*promptWatcher)
	}
	b.promptWatchers[ws] = w
	go b.watchPrompts(ctx, ws, sources, w.done)
}

// stopPromptWatcher stops ws's watcher, if it has one, and waits for it.
func (b *Backend) stopPromptWatcher(ws *Workspace) {
	b.promptWatchersMu.Lock()
	w, ok := b.promptWatchers[ws]
	delete(b.promptWatchers, ws)
	b.promptWatchersMu.Unlock()
	if !ok {
		return
	}
	w.cancel()
	<-w.done
}

// promptKey names one mark: a root chat and a kind of prompt.
type promptKey struct {
	root, kind string
}

// watchPrompts recomputes, after each item from any source, both marks of
// the root chat of that item's session, and sends every mark that differs
// from the last one sent. The event is engine-sent, so it never reaches the
// phone.
func (b *Backend) watchPrompts(ctx context.Context, ws *Workspace, src promptSources, done chan<- struct{}) {
	defer close(done)
	// sent holds the marks last sent as pending; a missing key was last
	// sent (or started) as not pending.
	sent := map[promptKey]bool{}
	update := func(sessionID string) {
		if sessionID == "" || ctx.Err() != nil {
			return
		}
		root, err := rootSessionID(ctx, ws, sessionID)
		if err != nil {
			slog.Debug("Failed to resolve root session for a pending prompt", "session_id", sessionID, "error", err)
		}
		for _, kind := range []string{promptKindPermission, promptKindQuestion} {
			pending := b.promptPending(ctx, ws, root, kind)
			key := promptKey{root: root, kind: kind}
			if sent[key] == pending {
				continue
			}
			if pending {
				sent[key] = true
			} else {
				delete(sent, key)
			}
			b.SendAll(ws.ID, EngineEvent{
				Type:    pubsub.PayloadTypePromptPending,
				Kind:    pubsub.UpdatedEvent,
				Payload: proto.PromptPending{SessionID: root, Kind: kind, Pending: pending},
			})
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-src.permissionRequests:
			if !ok {
				src.permissionRequests = nil
				continue
			}
			update(ev.Payload.SessionID)
		case ev, ok := <-src.permissionNotifications:
			if !ok {
				src.permissionNotifications = nil
				continue
			}
			update(ev.Payload.SessionID)
		case ev, ok := <-src.questionRequests:
			if !ok {
				src.questionRequests = nil
				continue
			}
			update(ev.Payload.SessionID)
		case ev, ok := <-src.questionNotifications:
			if !ok {
				src.questionNotifications = nil
				continue
			}
			update(ev.Payload.SessionID)
		case ev, ok := <-src.runCompletions:
			if !ok {
				src.runCompletions = nil
				continue
			}
			update(ev.Payload.SessionID)
		}
	}
}
