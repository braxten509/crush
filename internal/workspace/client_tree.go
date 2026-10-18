package workspace

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"weak"

	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
)

// Tree actions and file restores over the wire (engine-design.md 5.6).
var (
	_ TreeManagement  = (*ClientWorkspace)(nil)
	_ FileRestoration = (*ClientWorkspace)(nil)
)

// maxClientPlans is how many file plans a ClientWorkspace remembers, as
// many as the engine keeps.
const maxClientPlans = 16

// clientTree is what a ClientWorkspace remembers for tree actions: the
// revision of its last tree read or tree change of each chat (sent as the
// expected revision), which chat each message it read is in, and the
// engine's plan ID of each file plan it received.
type clientTree struct {
	mu        sync.Mutex
	revisions map[string]int64
	chats     map[string]string
	plans     map[*filehistory.Plan]string
	order     []*filehistory.Plan
}

// clientTrees holds each live ClientWorkspace's clientTree. An entry goes
// once its workspace is collected.
var (
	clientTreesMu sync.Mutex
	clientTrees   = map[weak.Pointer[ClientWorkspace]]*clientTree{}
)

func (w *ClientWorkspace) tree() *clientTree {
	key := weak.Make(w)
	clientTreesMu.Lock()
	defer clientTreesMu.Unlock()
	if t, ok := clientTrees[key]; ok {
		return t
	}
	t := &clientTree{revisions: map[string]int64{}, chats: map[string]string{}, plans: map[*filehistory.Plan]string{}}
	clientTrees[key] = t
	runtime.AddCleanup(w, func(key weak.Pointer[ClientWorkspace]) {
		clientTreesMu.Lock()
		delete(clientTrees, key)
		clientTreesMu.Unlock()
	}, key)
	return t
}

// Tree reads a chat's tree and remembers its revision.
func (w *ClientWorkspace) Tree(ctx context.Context, sessionID string) ([]message.TreeEntry, error) {
	t, err := w.client.Tree(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	state := w.tree()
	state.mu.Lock()
	state.revisions[sessionID] = t.Revision
	for _, e := range t.Entries {
		state.chats[e.MessageID] = sessionID
	}
	state.mu.Unlock()
	return proto.TreeToDomain(t), nil
}

// expectedRevision is the revision of the chat's last tree read or tree
// change. With none, the tree is read first.
func (w *ClientWorkspace) expectedRevision(ctx context.Context, sessionID string) (int64, error) {
	state := w.tree()
	state.mu.Lock()
	revision, ok := state.revisions[sessionID]
	state.mu.Unlock()
	if ok {
		return revision, nil
	}
	if _, err := w.Tree(ctx, sessionID); err != nil {
		return 0, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.revisions[sessionID], nil
}

// TreeMessage reads one whole message, from the chat a tree read found it
// in, or else from the chat this app views.
func (w *ClientWorkspace) TreeMessage(ctx context.Context, messageID string) (message.Message, error) {
	state := w.tree()
	state.mu.Lock()
	sessionID, ok := state.chats[messageID]
	state.mu.Unlock()
	if !ok {
		w.mu.RLock()
		sessionID = w.lastSession
		w.mu.RUnlock()
	}
	msg, err := w.client.TreeMessage(ctx, w.workspaceID(), sessionID, messageID)
	if err != nil {
		return message.Message{}, err
	}
	return proto.MessageToDomain(msg), nil
}

// rememberRevision keeps the revision a tree change answered with, so the
// next change needs no fresh read.
func (w *ClientWorkspace) rememberRevision(sessionID string, revision int64) {
	state := w.tree()
	state.mu.Lock()
	state.revisions[sessionID] = revision
	state.mu.Unlock()
}

// LabelTree labels a message of a chat's tree.
func (w *ClientWorkspace) LabelTree(ctx context.Context, sessionID, target, label string) error {
	revision, err := w.expectedRevision(ctx, sessionID)
	if err != nil {
		return err
	}
	res, err := w.client.LabelTree(ctx, w.workspaceID(), sessionID, proto.TreeLabelRequest{ExpectedRevision: revision, Target: target, Label: label})
	if err != nil {
		return err
	}
	w.rememberRevision(sessionID, res.Revision)
	return nil
}

// SwitchTree moves a chat to another branch.
func (w *ClientWorkspace) SwitchTree(ctx context.Context, sessionID, target string) error {
	return w.moveTree(ctx, sessionID, func(revision int64) (int64, error) {
		res, err := w.client.SwitchTree(ctx, w.workspaceID(), sessionID, proto.TreeMoveRequest{ExpectedRevision: revision, Target: target})
		return res.Revision, err
	})
}

// NavigateTree moves a chat to another branch, optionally with a summary
// of the branch left behind.
func (w *ClientWorkspace) NavigateTree(ctx context.Context, sessionID, target string, summary bool) error {
	return w.moveTree(ctx, sessionID, func(revision int64) (int64, error) {
		res, err := w.client.NavigateTree(ctx, w.workspaceID(), sessionID, proto.TreeMoveRequest{ExpectedRevision: revision, Target: target, Summarize: summary})
		return res.Revision, err
	})
}

// CopyTree forks or clones a chat into a new one. The undo record of a file
// restore applied first goes with the new chat, as in-process.
func (w *ClientWorkspace) CopyTree(ctx context.Context, sessionID, target string, fork bool) (newID, prompt string, err error) {
	err = w.moveTree(ctx, sessionID, func(revision int64) (int64, error) {
		req := proto.TreeCopyRequest{ExpectedRevision: revision, Target: target, Fork: fork}
		if request := message.FileRestoreFrom(ctx); request != nil {
			req.UndoID = request.Result.UndoID
		}
		res, err := w.client.CopyTree(ctx, w.workspaceID(), sessionID, req)
		newID, prompt = res.SessionID, res.Prompt
		return res.Revision, err
	})
	return newID, prompt, err
}

// moveTree runs a tree change with the expected revision and keeps the
// revision it answers with. An approved file restore riding on ctx is
// applied first, through its plan, as the in-process app applies it right
// before the change.
func (w *ClientWorkspace) moveTree(ctx context.Context, sessionID string, change func(revision int64) (int64, error)) error {
	revision, err := w.expectedRevision(ctx, sessionID)
	if err != nil {
		return err
	}
	request := message.FileRestoreFrom(ctx)
	if request == nil || request.Plan == nil {
		return w.changeTree(sessionID, revision, change)
	}
	// The restore and the change are two guarded calls, not one as
	// in-process. A change refused in between (another app moved the chat,
	// or a prompt started) leaves the files restored and the head where it
	// was. This is accepted as rare and recoverable: the error carries the
	// restore's notice, and the restore can be undone.
	if request.Result, err = w.UndoFiles(ctx, request.Plan); err != nil {
		return err
	}
	if err := w.changeTree(sessionID, revision, change); err != nil {
		return fmt.Errorf("%w. %s", err, request.Result.Notice())
	}
	return nil
}

// changeTree runs change and keeps the revision it answers with.
func (w *ClientWorkspace) changeTree(sessionID string, revision int64, change func(revision int64) (int64, error)) error {
	revision, err := change(revision)
	if err != nil {
		return err
	}
	w.rememberRevision(sessionID, revision)
	return nil
}

// PreviewFiles plans the file restore of a move to target.
func (w *ClientWorkspace) PreviewFiles(ctx context.Context, sessionID, target string, fork bool) (*filehistory.Plan, error) {
	plan, err := w.client.PreviewFiles(ctx, w.workspaceID(), sessionID, proto.FilePreviewRequest{Target: target, Fork: fork})
	if err != nil {
		return nil, err
	}
	return w.rememberPlan(plan)
}

// PreviewUndoFiles plans undoing the chat's last file restore.
func (w *ClientWorkspace) PreviewUndoFiles(ctx context.Context, sessionID string) (*filehistory.Plan, error) {
	plan, err := w.client.PreviewUndoFiles(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	return w.rememberPlan(plan)
}

// rememberPlan converts a plan and remembers its plan ID for UndoFiles.
func (w *ClientWorkspace) rememberPlan(p proto.FilePlan) (*filehistory.Plan, error) {
	plan, err := proto.FilePlanToDomain(p)
	if err != nil {
		return nil, err
	}
	state := w.tree()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.plans[plan] = p.PlanID
	state.order = append(state.order, plan)
	for len(state.order) > maxClientPlans {
		delete(state.plans, state.order[0])
		state.order = state.order[1:]
	}
	return plan, nil
}

// UndoFiles applies a plan this workspace received, by its plan ID. The
// engine refuses a plan it no longer keeps.
func (w *ClientWorkspace) UndoFiles(ctx context.Context, plan *filehistory.Plan) (filehistory.Result, error) {
	if plan == nil {
		return filehistory.Result{}, fmt.Errorf("no file plan to apply")
	}
	state := w.tree()
	state.mu.Lock()
	planID := state.plans[plan]
	state.mu.Unlock()
	res, err := w.client.UndoFiles(ctx, w.workspaceID(), plan.SessionID, planID)
	if err != nil {
		return filehistory.Result{}, err
	}
	return proto.FileRestoreResultToDomain(res), nil
}
