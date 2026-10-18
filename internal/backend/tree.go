package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// Tree actions over the wire (engine-design.md 5.6). Each one does what the
// in-process app's workspace does (workspace/tree.go, file_restore.go),
// with the caller's expected revision checked under the chat lock.

var (
	// ErrFilePlanExpired refuses a plan_id the engine no longer keeps, or a
	// preview plan made before the chat's newest message.
	ErrFilePlanExpired = errors.New("the file preview expired; open it again")
	// ErrTreeMessageNotFound refuses a tree message read for a message that
	// is not in the chat named.
	ErrTreeMessageNotFound = errors.New("message not found in this chat")
)

const (
	// maxFilePlans is how many file plans one workspace keeps.
	maxFilePlans = 16
	// filePlanTTL is how long a file plan is kept.
	filePlanTTL = 10 * time.Minute
)

// treeState is piece C's state per workspace (tree actions and branch copy,
// engine-design.md 5.6 and 5.11): the file plans handed out, by plan ID,
// and the undo record of each chat's last file restore through UndoFiles,
// by chat. Both live in memory only.
type treeState struct {
	mu    sync.Mutex
	plans map[string]keptPlan
	undos map[string]string
}

type keptPlan struct {
	plan  *filehistory.Plan
	added time.Time
}

// keep stores plan and returns its new ID. The oldest plan goes when the
// workspace already keeps maxFilePlans.
func (t *treeState) keep(plan *filehistory.Plan, now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(now)
	if t.plans == nil {
		t.plans = map[string]keptPlan{}
	}
	for len(t.plans) >= maxFilePlans {
		oldest := ""
		for id, kept := range t.plans {
			if oldest == "" || kept.added.Before(t.plans[oldest].added) {
				oldest = id
			}
		}
		delete(t.plans, oldest)
	}
	id := uuid.NewString()
	t.plans[id] = keptPlan{plan: plan, added: now}
	return id
}

// take removes and returns the plan kept under id for sessionID. A plan is
// used once.
func (t *treeState) take(id, sessionID string, now time.Time) (keptPlan, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(now)
	kept, ok := t.plans[id]
	if !ok || kept.plan.SessionID != sessionID {
		return keptPlan{}, false
	}
	delete(t.plans, id)
	return kept, true
}

// putBack returns a plan that could not be used yet, unless it expired.
func (t *treeState) putBack(id string, kept keptPlan, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.Sub(kept.added) >= filePlanTTL {
		return
	}
	if t.plans == nil {
		t.plans = map[string]keptPlan{}
	}
	t.plans[id] = kept
}

func (t *treeState) expireLocked(now time.Time) {
	for id, kept := range t.plans {
		if now.Sub(kept.added) >= filePlanTTL {
			delete(t.plans, id)
		}
	}
}

// rememberUndo keeps undoID as the undo record of sessionID's last file
// restore, in place of the one before; "" forgets it.
func (t *treeState) rememberUndo(sessionID, undoID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if undoID == "" {
		delete(t.undos, sessionID)
		return
	}
	if t.undos == nil {
		t.undos = map[string]string{}
	}
	t.undos[sessionID] = undoID
}

// takeUndo reports whether undoID is the undo record of sessionID's last
// file restore, and forgets it if so.
func (t *treeState) takeUndo(sessionID, undoID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if undoID == "" || t.undos[sessionID] != undoID {
		return false
	}
	delete(t.undos, sessionID)
	return true
}

// clear forgets every plan and undo record.
func (t *treeState) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.plans, t.undos = nil, nil
}

// treeHooks are piece C's client hooks: a torn-down workspace forgets its
// plans and undo records.
func (b *Backend) treeHooks() clientHooks {
	return clientHooks{teardown: func(ws *Workspace) { ws.tree.clear() }}
}

// treeService is the workspace's tree storage.
func treeService(ws *Workspace) (message.TreeService, error) {
	tree, ok := ws.Messages.(message.TreeService)
	if !ok {
		return nil, errors.New("tree storage unavailable")
	}
	return tree, nil
}

// treeAgent is the workspace's coordinator, which runs the guarded tree
// actions.
func treeAgent(ws *Workspace) (agent.Coordinator, error) {
	if ws.AgentCoordinator == nil {
		return nil, ErrAgentNotInitialized
	}
	return ws.AgentCoordinator, nil
}

// treeChanged tells every app that a chat's tree changed, with its revision
// now.
func (b *Backend) treeChanged(ctx context.Context, ws *Workspace, tree message.TreeService, sessionID string) error {
	revision, err := tree.TreeRevision(ctx, sessionID)
	if err != nil {
		return err
	}
	b.SendAll(ws.ID, EngineEvent{
		Type: pubsub.PayloadTypeTreeChanged, Kind: pubsub.UpdatedEvent,
		Payload: proto.TreeChanged{SessionID: sessionID, Revision: revision},
	})
	return nil
}

// TreeClient checks the optional client_id of a tree or branch route: ""
// passes, and an app that is not registered on the workspace fails with
// ErrClientNotAttached (engine-design.md 0.1).
func (b *Backend) TreeClient(workspaceID, clientID string) error {
	if clientID == "" {
		return nil
	}
	if _, err := validateClientID(clientID); err != nil {
		return err
	}
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.clientsMu.Lock()
	_, ok := ws.clients[clientID]
	ws.clientsMu.Unlock()
	if !ok {
		return ErrClientNotAttached
	}
	return nil
}

// Tree returns a chat's tree with one text preview per message. The
// revision is read first, so it is never newer than the entries.
func (b *Backend) Tree(ctx context.Context, workspaceID, sessionID string) (proto.Tree, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Tree{}, err
	}
	tree, err := treeService(ws)
	if err != nil {
		return proto.Tree{}, err
	}
	revision, err := tree.TreeRevision(ctx, sessionID)
	if err != nil {
		return proto.Tree{}, err
	}
	entries, err := tree.Tree(ctx, sessionID)
	if err != nil {
		return proto.Tree{}, err
	}
	return proto.TreeFromDomain(revision, entries), nil
}

// TreeMessage returns one whole message of a chat's tree.
func (b *Backend) TreeMessage(ctx context.Context, workspaceID, sessionID, messageID string) (proto.Message, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Message{}, err
	}
	msg, err := ws.Messages.Get(ctx, messageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && msg.SessionID != sessionID) {
		return proto.Message{}, ErrTreeMessageNotFound
	}
	if err != nil {
		return proto.Message{}, err
	}
	return proto.MessageFromDomain(msg), nil
}

// LabelTree labels a message. A label does not move the head, so it runs
// without the chat lock; the revision is compared just before. A label
// leaves the revision as it is, so the one compared is the answer: one
// read after could include another app's change made since.
func (b *Backend) LabelTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeLabelRequest) (proto.TreeResult, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.TreeResult{}, err
	}
	tree, err := treeService(ws)
	if err != nil {
		return proto.TreeResult{}, err
	}
	revision, err := tree.TreeRevision(ctx, sessionID)
	if err != nil {
		return proto.TreeResult{}, err
	}
	if revision != req.ExpectedRevision {
		return proto.TreeResult{}, agent.ErrTreeRevisionChanged
	}
	if err := tree.LabelTree(ctx, sessionID, req.Target, req.Label); err != nil {
		return proto.TreeResult{}, err
	}
	if err := b.treeChanged(ctx, ws, tree, sessionID); err != nil {
		return proto.TreeResult{}, err
	}
	return proto.TreeResult{Revision: revision}, nil
}

// SwitchTree moves a chat's head to another branch.
func (b *Backend) SwitchTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	return b.moveTree(ctx, workspaceID, sessionID, req, func(coordinator agent.Coordinator, ctx context.Context) error {
		switcher, ok := coordinator.(interface {
			SwitchTree(context.Context, string, string) error
		})
		if !ok {
			return fmt.Errorf("tree switching requires a local agent")
		}
		return switcher.SwitchTree(ctx, sessionID, req.Target)
	})
}

// NavigateTree moves a chat's head to another branch, optionally leaving a
// summary of the branch left behind.
func (b *Backend) NavigateTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	return b.moveTree(ctx, workspaceID, sessionID, req, func(coordinator agent.Coordinator, ctx context.Context) error {
		manager, ok := coordinator.(interface {
			NavigateTree(context.Context, string, string, bool) error
		})
		if !ok {
			return fmt.Errorf("session trees need a local workspace")
		}
		return manager.NavigateTree(ctx, sessionID, req.Target, req.Summarize)
	})
}

func (b *Backend) moveTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeMoveRequest, move func(agent.Coordinator, context.Context) error) (proto.TreeResult, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.TreeResult{}, err
	}
	tree, err := treeService(ws)
	if err != nil {
		return proto.TreeResult{}, err
	}
	coordinator, err := treeAgent(ws)
	if err != nil {
		return proto.TreeResult{}, err
	}
	// The answer is the revision read under the chat lock, right after the
	// move.
	var revision int64
	guarded := agent.WithTreeRevisionAfter(agent.WithTreeRevision(ctx, req.ExpectedRevision), &revision)
	if err := move(coordinator, guarded); err != nil {
		return proto.TreeResult{}, err
	}
	if err := b.treeChanged(ctx, ws, tree, sessionID); err != nil {
		return proto.TreeResult{}, err
	}
	return proto.TreeResult{Revision: revision}, nil
}

// CopyTree forks or clones a chat into a new one, which every app then sees
// as a created session.
func (b *Backend) CopyTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeCopyRequest) (proto.TreeCopyResult, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.TreeCopyResult{}, err
	}
	tree, err := treeService(ws)
	if err != nil {
		return proto.TreeCopyResult{}, err
	}
	coordinator, err := treeAgent(ws)
	if err != nil {
		return proto.TreeCopyResult{}, err
	}
	manager, ok := coordinator.(interface {
		CopyTree(context.Context, string, string, bool) (string, string, error)
	})
	if !ok {
		return proto.TreeCopyResult{}, fmt.Errorf("session trees need a local workspace")
	}
	// The answer is the source's revision read under the chat lock, right
	// after the copy.
	var revision int64
	guarded := agent.WithTreeRevisionAfter(agent.WithTreeRevision(ctx, req.ExpectedRevision), &revision)
	newID, prompt, err := manager.CopyTree(guarded, sessionID, req.Target, req.Fork)
	if err != nil {
		return proto.TreeCopyResult{}, err
	}
	// The undo record of a file restore applied just before goes with the
	// new chat, as message.CopyTree moves it in-process; but only the
	// record of this chat's last restore through UndoFiles. After an engine
	// restart none is known, and the record stays with the source chat.
	if req.UndoID != "" {
		if ws.tree.takeUndo(sessionID, req.UndoID) {
			// The copy is made: a failed move must not hide it. The record
			// then stays with the source chat.
			if err := moveUndo(ctx, ws, req.UndoID, newID); err != nil {
				slog.Warn("Could not move the undo record to the copied chat; it stays with the source chat",
					"session_id", sessionID, "new_session_id", newID, "undo_id", req.UndoID, "error", err)
			}
		} else {
			slog.Warn("Copy names an undo record that is not this chat's last file restore; it stays where it is",
				"session_id", sessionID, "undo_id", req.UndoID)
		}
	}
	if err := announce(ctx, ws, newID); err != nil {
		return proto.TreeCopyResult{}, err
	}
	if err := b.treeChanged(ctx, ws, tree, sessionID); err != nil {
		return proto.TreeCopyResult{}, err
	}
	return proto.TreeCopyResult{SessionID: newID, Prompt: prompt, Revision: revision}, nil
}

// moveUndo moves the undo record undoID to the chat sessionID.
func moveUndo(ctx context.Context, ws *Workspace, undoID, sessionID string) error {
	store, err := fileHistory(ws)
	if err != nil {
		return err
	}
	return store.MoveUndo(ctx, undoID, sessionID)
}

// announce publishes a created event for a chat written directly to the
// database, so every app sees it.
func announce(ctx context.Context, ws *Workspace, sessionID string) error {
	announcer, ok := ws.Sessions.(interface {
		Announce(context.Context, string) error
	})
	if !ok {
		return nil
	}
	return announcer.Announce(ctx, sessionID)
}

// fileHistory is the workspace's file history store.
func fileHistory(ws *Workspace) (*filehistory.Store, error) {
	service, ok := ws.Messages.(message.FileHistoryService)
	if !ok || service.FileHistory() == nil {
		return nil, errors.New("file history unavailable")
	}
	return service.FileHistory(), nil
}

// PreviewFiles plans the file restore of a move to req.Target and keeps
// the plan for UndoFiles.
func (b *Backend) PreviewFiles(ctx context.Context, workspaceID, sessionID string, req proto.FilePreviewRequest) (proto.FilePlan, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FilePlan{}, err
	}
	store, err := fileHistory(ws)
	if err != nil {
		return proto.FilePlan{}, err
	}
	if err := ws.Messages.FlushAll(ctx); err != nil {
		return proto.FilePlan{}, err
	}
	target := req.Target
	if req.Fork {
		tree, err := treeService(ws)
		if err != nil {
			return proto.FilePlan{}, err
		}
		entries, err := tree.Tree(ctx, sessionID)
		if err != nil {
			return proto.FilePlan{}, err
		}
		found := false
		for _, entry := range entries {
			if entry.MessageID == target {
				target = entry.ParentID
				found = true
				break
			}
		}
		if !found {
			return proto.FilePlan{}, errors.New("choose a user prompt to fork")
		}
	}
	plan, err := store.Preview(ctx, sessionID, target)
	if err != nil {
		return proto.FilePlan{}, err
	}
	return b.keepPlan(ws, plan)
}

// PreviewUndoFiles plans undoing the chat's last file restore and keeps the
// plan for UndoFiles.
func (b *Backend) PreviewUndoFiles(ctx context.Context, workspaceID, sessionID string) (proto.FilePlan, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FilePlan{}, err
	}
	store, err := fileHistory(ws)
	if err != nil {
		return proto.FilePlan{}, err
	}
	plan, err := store.UndoPreview(ctx, sessionID)
	if err != nil {
		return proto.FilePlan{}, err
	}
	return b.keepPlan(ws, plan)
}

func (b *Backend) keepPlan(ws *Workspace, plan *filehistory.Plan) (proto.FilePlan, error) {
	id := ws.tree.keep(plan, time.Now())
	out, err := proto.FilePlanFromDomain(id, plan)
	if err != nil {
		ws.tree.take(id, plan.SessionID, time.Now())
		return proto.FilePlan{}, err
	}
	return out, nil
}

// UndoFiles applies a kept plan: a preview's restore or an undo. Each plan
// is used once. A preview plan is refused when the chat moved since the
// preview: as expired after a new message, and as a revision conflict
// after a branch change; the tree guard checks both under the chat lock. A
// busy chat keeps the plan for another try. The restore's undo record is
// remembered for a copy of the chat to take along.
func (b *Backend) UndoFiles(ctx context.Context, workspaceID, sessionID, planID string) (proto.FileRestoreResult, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FileRestoreResult{}, err
	}
	coordinator, err := treeAgent(ws)
	if err != nil {
		return proto.FileRestoreResult{}, err
	}
	restorer, ok := coordinator.(interface {
		UndoFiles(context.Context, *filehistory.Plan) (filehistory.Result, error)
	})
	if !ok {
		return proto.FileRestoreResult{}, errors.New("file restore needs a local agent")
	}
	kept, ok := ws.tree.take(planID, sessionID, time.Now())
	if !ok {
		return proto.FileRestoreResult{}, ErrFilePlanExpired
	}
	applyCtx := ctx
	if kept.plan.UndoID == "" {
		applyCtx = agent.WithTreeHead(agent.WithTreeRevision(ctx, kept.plan.Revision), kept.plan.Source)
	}
	result, err := restorer.UndoFiles(applyCtx, kept.plan)
	switch {
	case errors.Is(err, agent.ErrTreeBusy):
		ws.tree.putBack(planID, kept, time.Now())
	case errors.Is(err, agent.ErrTreeHeadMoved) && result.UndoID == "":
		// A new message moved the head but not the revision, and nothing
		// was restored: the plan can never apply again, so it expired.
		err = fmt.Errorf("%w: %w", agent.ErrTreeHeadMoved, ErrFilePlanExpired)
	}
	if err != nil {
		return proto.FileRestoreResult{}, err
	}
	ws.tree.rememberUndo(sessionID, result.UndoID)
	return proto.FileRestoreResultFromDomain(result), nil
}
