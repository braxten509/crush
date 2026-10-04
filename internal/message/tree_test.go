package message

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTreeBranchesRemainSeparate(t *testing.T) {
	svc, id := newTestService(t)
	ctx := t.Context()
	tree := svc.(TreeService)
	create := func(text string, role MessageRole) Message {
		msg, err := svc.Create(ctx, id, CreateMessageParams{Role: role, Parts: []ContentPart{TextContent{Text: text}}})
		require.NoError(t, err)
		return msg
	}
	root := create("root", User)
	old := create("old reply", Assistant)
	require.NoError(t, tree.LabelTree(ctx, id, old.ID, "original"))
	require.NoError(t, tree.SwitchTree(ctx, id, root.ID))
	create("alternative", User)
	alternate := create("new reply", Assistant)
	msgs, err := svc.List(ctx, id)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	require.Equal(t, alternate.ID, msgs[2].ID)
	for _, msg := range msgs {
		require.NotEqual(t, old.ID, msg.ID)
	}
	require.NoError(t, tree.SwitchTree(ctx, id, old.ID))
	msgs, err = svc.List(ctx, id)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, old.ID, msgs[1].ID)
	nodes, err := tree.Tree(ctx, id)
	require.NoError(t, err)
	require.Len(t, nodes, 4)
	require.Equal(t, "original", nodes[1].Label)
	rev, err := tree.TreeRevision(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 2, rev)
	require.Error(t, tree.SwitchTree(ctx, "wrong-session", old.ID))
	require.NoError(t, tree.SwitchTree(ctx, id, alternate.ID))
	last, err := svc.GetLastAssistantMessage(ctx, id)
	require.NoError(t, err)
	require.Equal(t, alternate.ID, last.ID)
}
func TestTreeSummaryDoesNotLeakAcrossBranches(t *testing.T) {
	svc, id := newTestService(t)
	ctx := t.Context()
	tree := svc.(TreeService)
	root, err := svc.Create(ctx, id, CreateMessageParams{Role: User})
	require.NoError(t, err)
	summary, err := svc.Create(ctx, id, CreateMessageParams{Role: Assistant, IsSummaryMessage: true})
	require.NoError(t, err)
	_, err = svc.Create(ctx, id, CreateMessageParams{Role: User})
	require.NoError(t, err)
	require.NoError(t, tree.SwitchTree(ctx, id, root.ID))
	alternate, err := svc.Create(ctx, id, CreateMessageParams{Role: User})
	require.NoError(t, err)
	msgs, err := svc.ListFromSummary(ctx, id, summary.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, alternate.ID, msgs[1].ID)
}
func TestTreeFailedSummaryDeletionPreservesChildren(t *testing.T) {
	svc, id := newTestService(t)
	ctx := t.Context()
	root, err := svc.Create(ctx, id, CreateMessageParams{Role: User})
	require.NoError(t, err)
	placeholder, err := svc.Create(ctx, id, CreateMessageParams{Role: Assistant, IsSummaryMessage: true})
	require.NoError(t, err)
	child, err := svc.Create(ctx, id, CreateMessageParams{Role: User})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, placeholder.ID))
	msgs, err := svc.List(ctx, id)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, root.ID, msgs[0].ID)
	require.Equal(t, child.ID, msgs[1].ID)
}
