package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTreeCLIStartsFreshAndHandoffExcludesAbandonedBranch(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()
	sess, err := env.sessions.Create(ctx, "branches")
	require.NoError(t, err)
	root, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "ROOT_AMBER"}}})
	require.NoError(t, err)
	old, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "OLD_VIOLET"}}})
	require.NoError(t, err)
	provider := cliagent.NewProvider(config.TypeClaudeCode, t.TempDir(), t.TempDir(), nil, nil, "", false, "", false)
	model, err := provider.LanguageModel(ctx, "haiku")
	require.NoError(t, err)
	cli := model.(*cliagent.Model)
	require.NoError(t, cli.Links.Set(sess.ID, cli.Kind, cliagent.Link{Native: "original-native-id", Through: old.ID}))
	agent := &sessionAgent{messages: env.messages}
	tree := env.messages.(message.TreeService)
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, root.ID))
	link, err := agent.treeCLILink(ctx, cli, sess.ID)
	require.NoError(t, err)
	require.Empty(t, link.Native)
	history, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	prompt, resume := cliHandoff(history, link, "NEW_COBALT")
	require.Empty(t, resume)
	require.Contains(t, prompt, "ROOT_AMBER")
	require.False(t, strings.Contains(prompt, "OLD_VIOLET"))
	require.Contains(t, prompt, "NEW_COBALT")
	agent.saveCLILink(ctx, cli, sess.ID, "new-native-id", false, "")
	link, err = agent.treeCLILink(ctx, cli, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "new-native-id", link.Native)
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, old.ID))
	link, err = agent.treeCLILink(ctx, cli, sess.ID)
	require.NoError(t, err)
	require.Empty(t, link.Native)
	history, err = env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	prompt, resume = cliHandoff(history, link, "continue original")
	require.Empty(t, resume)
	require.Contains(t, prompt, "OLD_VIOLET")
	require.NotContains(t, prompt, "NEW_COBALT")
}

func TestTreeSwitchRejectsAcceptedAndQueuedWork(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()
	sess, err := env.sessions.Create(ctx, "guard")
	require.NoError(t, err)
	root, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User})
	require.NoError(t, err)
	sa := testSessionAgent(env, &finishStreamModel{text: "done"}, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	c := &coordinator{mainAgent: sa, messages: env.messages}
	accepted := sa.BeginAccepted(sess.ID)
	require.ErrorContains(t, c.SwitchTree(ctx, sess.ID, root.ID), "wait")
	accepted.Close()
	sa.messageQueue.Set(sess.ID, []SessionAgentCall{{SessionID: sess.ID, Prompt: "queued"}})
	require.ErrorContains(t, c.SwitchTree(ctx, sess.ID, root.ID), "wait")
	sa.messageQueue.Del(sess.ID)
	require.NoError(t, c.SwitchTree(ctx, sess.ID, root.ID))
}

// Captures the actual model request, including cancellation and failed requests.
type treeSummaryModel struct {
	finishStreamModel
	prompt  string
	failure error
	cancel  context.CancelFunc
}

func (m *treeSummaryModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	m.prompt = fmt.Sprintf("%v", call.Prompt)
	if m.cancel != nil {
		m.cancel()
	}
	if m.failure != nil {
		return nil, m.failure
	}
	return m.finishStreamModel.Generate(ctx, call)
}

func TestTreeSummaryOptInCommonAncestorAndFailure(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()
	sess, err := env.sessions.Create(ctx, "summary")
	require.NoError(t, err)
	create := func(text string) message.Message {
		m, e := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}})
		require.NoError(t, e)
		return m
	}
	root := create("SHARED_ROOT")
	old := create("OLD_BRANCH")
	tree := env.messages.(message.TreeService)
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, root.ID))
	other := create("DESTINATION")
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, old.ID))
	model := &treeSummaryModel{finishStreamModel: finishStreamModel{text: "The old branch found amber."}}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	c := &coordinator{mainAgent: sa, messages: env.messages}
	require.NoError(t, c.NavigateTree(ctx, sess.ID, other.ID, false))
	require.Empty(t, model.prompt)
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, old.ID))
	require.NoError(t, c.NavigateTree(ctx, sess.ID, other.ID, true))
	require.Contains(t, model.prompt, "OLD_BRANCH")
	require.NotContains(t, model.prompt, "SHARED_ROOT")
	require.NotContains(t, model.prompt, "DESTINATION")
	path, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, path, 3)
	require.Equal(t, other.ID, path[1].ID)
	require.Contains(t, path[2].Content().Text, "found amber")
	require.NoError(t, tree.SwitchTree(ctx, sess.ID, old.ID))
	model.failure = errors.New("provider failed")
	require.ErrorContains(t, c.NavigateTree(ctx, sess.ID, other.ID, true), "provider failed")
	path, err = env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, old.ID, path[len(path)-1].ID)
	model.failure = nil
	cancelCtx, cancel := context.WithCancel(ctx)
	model.cancel = cancel
	require.Error(t, c.NavigateTree(cancelCtx, sess.ID, other.ID, true))
	path, err = env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, old.ID, path[len(path)-1].ID)
}

// treeRaceMessages is a message service for the tree guard's tests. A hook
// can run once right after the next head read or switch, and each revision
// read notes whether the chat lock was held.
type treeRaceMessages struct {
	message.Service
	tree    message.TreeService
	history message.FileHistoryService

	afterHeadRead func()      // run once, after the next head read
	afterSwitch   func()      // run once, after the next switch
	lockHeld      func() bool // whether the chat lock is held now
	headReads     int
	lockedReads   []bool // lockHeld at each revision read
}

func newTreeRaceMessages(messages message.Service) *treeRaceMessages {
	history, _ := messages.(message.FileHistoryService)
	return &treeRaceMessages{Service: messages, tree: messages.(message.TreeService), history: history}
}

func (m *treeRaceMessages) Tree(ctx context.Context, id string) ([]message.TreeEntry, error) {
	return m.tree.Tree(ctx, id)
}

func (m *treeRaceMessages) TreeHead(ctx context.Context, id string) (string, error) {
	head, err := m.tree.TreeHead(ctx, id)
	m.headReads++
	if hook := m.afterHeadRead; hook != nil {
		m.afterHeadRead = nil
		hook()
	}
	return head, err
}

func (m *treeRaceMessages) TreeRevision(ctx context.Context, id string) (int64, error) {
	if m.lockHeld != nil {
		m.lockedReads = append(m.lockedReads, m.lockHeld())
	}
	return m.tree.TreeRevision(ctx, id)
}

func (m *treeRaceMessages) SwitchTree(ctx context.Context, id, target string) error {
	err := m.tree.SwitchTree(ctx, id, target)
	if hook := m.afterSwitch; hook != nil {
		m.afterSwitch = nil
		hook()
	}
	return err
}

func (m *treeRaceMessages) LabelTree(ctx context.Context, id, target, label string) error {
	return m.tree.LabelTree(ctx, id, target, label)
}

func (m *treeRaceMessages) SwitchTreeNote(ctx context.Context, id, target string, note message.BranchSummary) error {
	return m.tree.SwitchTreeNote(ctx, id, target, note)
}

func (m *treeRaceMessages) CopyTree(ctx context.Context, id, target string, fork bool) (string, string, error) {
	return m.tree.CopyTree(ctx, id, target, fork)
}

func (m *treeRaceMessages) FileHistory() *filehistory.Store { return m.history.FileHistory() }

// The guard refuses a file plan once the chat's head left the plan's start,
// with the chat lock held: for a message that landed before the guard took
// the lock, and for one that a writer skipping the lock (a bang shell
// command) lands right before the apply. Neither restores a file.
func TestTreeGuardRefusesMovedHead(t *testing.T) {
	data := t.TempDir()
	conn, err := db.Connect(t.Context(), data)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(data) })
	q := db.New(conn)
	store := filehistory.New(conn, data)
	env := testEnv(t)
	env.sessions = session.NewService(q, conn)
	env.messages = message.NewService(q, message.WithFileHistory(store))
	ctx := t.Context()

	// File history skips temporary folders, so the file lives in a folder of
	// the test's own, as in its own tests.
	root, err := os.MkdirTemp(".", "tree-guard-")
	require.NoError(t, err)
	root, err = filepath.Abs(root)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	path := filepath.Join(root, "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("v2\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))
	read := func() string {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		return string(data)
	}

	sess, err := env.sessions.Create(ctx, "Notes")
	require.NoError(t, err)
	add := func(role message.MessageRole, parts ...message.ContentPart) string {
		m, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: role, Parts: parts})
		require.NoError(t, err)
		return m.ID
	}
	state := func(text string) *filechange.State {
		return &filechange.State{Content: text, Size: int64(len(text)), Mode: 0o644}
	}
	add(message.User, message.TextContent{Text: "Edit the notes"})
	edit := add(message.Assistant, message.ToolCall{ID: "c1", Name: "edit", Input: "{}", Finished: true}, message.Finish{Reason: message.FinishReasonToolUse})
	add(message.Tool, message.ToolResult{ToolCallID: "c1", Name: "edit", Content: "edited", Review: &filechange.Review{
		Root: root, Changes: []filechange.Change{{Path: path, Order: 1, Before: state("v1\n"), After: state("v2\n")}},
	}})
	add(message.Assistant, message.TextContent{Text: "Done."}, message.Finish{Reason: message.FinishReasonEndTurn})

	messages := newTreeRaceMessages(env.messages)
	sa := testSessionAgent(env, &finishStreamModel{text: "done"}, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	c := &coordinator{mainAgent: sa, messages: messages}
	preview := func() *filehistory.Plan {
		plan, err := store.Preview(ctx, sess.ID, edit)
		require.NoError(t, err)
		require.Len(t, plan.Entries, 1)
		return plan
	}
	apply := func(plan *filehistory.Plan) (filehistory.Result, error) {
		messages.headReads = 0
		return c.UndoFiles(WithTreeHead(WithTreeRevision(ctx, plan.Revision), plan.Source), plan)
	}

	// A message before the guard takes the lock: refused before the apply.
	plan := preview()
	add(message.User, message.TextContent{Text: "Thanks"})
	result, err := apply(plan)
	require.ErrorIs(t, err, ErrTreeHeadMoved)
	require.Empty(t, result.UndoID)
	require.Equal(t, 1, messages.headReads, "only the guard's head check ran")
	require.Equal(t, "v2\n", read())

	// A message right after the guard's head check: the apply refuses it,
	// and the guard's second look under the lock says why.
	plan = preview()
	messages.afterHeadRead = func() { add(message.User, message.TextContent{Text: "One more thing"}) }
	result, err = apply(plan)
	require.ErrorIs(t, err, ErrTreeHeadMoved)
	require.Empty(t, result.UndoID)
	require.Equal(t, 2, messages.headReads, "the guard looked again after the apply failed")
	require.Equal(t, "v2\n", read())

	// With the head where the plan started, the same restore goes through.
	plan = preview()
	result, err = apply(plan)
	require.NoError(t, err)
	require.Equal(t, 1, result.Restored)
	require.Equal(t, "v1\n", read())
}

// The guard reads the revision it hands back right after the action, with
// the chat lock still held. Every revision change takes that lock, so a
// switch by another app queued behind it lands after the answer.
func TestTreeGuardRevisionAfter(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()
	sess, err := env.sessions.Create(ctx, "revision")
	require.NoError(t, err)
	create := func(text string) message.Message {
		m, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}})
		require.NoError(t, err)
		return m
	}
	root := create("ROOT")
	old := create("OLD")
	tree := env.messages.(message.TreeService)
	revision := func() int64 {
		r, err := tree.TreeRevision(ctx, sess.ID)
		require.NoError(t, err)
		return r
	}
	sa := testSessionAgent(env, &finishStreamModel{text: "done"}, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	messages := newTreeRaceMessages(env.messages)
	mu := sa.sessionMu(sess.ID)
	messages.lockHeld = func() bool {
		if mu.TryLock() {
			mu.Unlock()
			return false
		}
		return true
	}
	c := &coordinator{mainAgent: sa, messages: messages}

	// Both the check and the answer are read with the lock held.
	before := revision()
	var after int64
	require.NoError(t, c.SwitchTree(WithTreeRevisionAfter(WithTreeRevision(ctx, before), &after), sess.ID, root.ID))
	require.Equal(t, before+1, after)
	require.Equal(t, after, revision())
	require.Equal(t, []bool{true, true}, messages.lockedReads)

	// Another app's switch, started right after this one's change, waits
	// for the lock, so the answer does not include it.
	messages.lockHeld = nil
	before = after
	other := make(chan error, 1)
	messages.afterSwitch = func() { go func() { other <- c.SwitchTree(ctx, sess.ID, root.ID) }() }
	require.NoError(t, c.SwitchTree(WithTreeRevisionAfter(WithTreeRevision(ctx, before), &after), sess.ID, old.ID))
	require.NoError(t, <-other)
	require.Equal(t, before+1, after)
	require.Equal(t, after+1, revision())
}
