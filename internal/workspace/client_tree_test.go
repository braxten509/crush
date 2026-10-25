package workspace_test

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// The assertions of client_tree.go, where a test sees them fail.
var (
	_ workspace.TreeManagement  = (*workspace.ClientWorkspace)(nil)
	_ workspace.FileRestoration = (*workspace.ClientWorkspace)(nil)
)

// treeHead is the message an entry marks as the chat's head.
func treeHead(entries []message.TreeEntry) string {
	for _, e := range entries {
		if e.Active {
			return e.MessageID
		}
	}
	return ""
}

// A ClientWorkspace does tree actions and file restores through the engine:
// it sends the revision of its last tree read or change, and applies a file
// plan by the plan ID it came with.
func TestClientWorkspaceTreeAndFiles(t *testing.T) {
	xdgIsolate(t)
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	rt := newRuntimeServer(t)
	cwd, dataDir := t.TempDir(), t.TempDir()
	c := rt.newClient(t, cwd)
	ctx := t.Context()
	created, err := c.CreateWorkspace(ctx, prototest.CreateRequest(proto.Workspace{Path: cwd, DataDir: dataDir}))
	require.NoError(t, err)
	engine, err := rt.srv.Backend().GetWorkspace(created.ID)
	require.NoError(t, err)

	// The tree guard needs an agent; nothing here calls its model.
	const providerID, modelID = "test-openai-compat", "test-model"
	engine.Cfg.Config().Providers.Set(providerID, config.ProviderConfig{
		ID: providerID, Name: "Test", Type: openaicompat.Name,
		BaseURL: "http://127.0.0.1:0/v1", APIKey: "test",
		Models: []catwalk.Model{{ID: modelID, DefaultMaxTokens: 4096}},
	})
	engine.Cfg.OverrideHelperModel(config.SelectedModel{Provider: providerID, Model: modelID})
	engine.Cfg.SetupAgents()
	require.NoError(t, c.InitiateAgentProcessing(ctx, created.ID, false))

	// Two answers to the first prompt, the head on the second.
	chat, err := engine.Sessions.Create(ctx, "Trip")
	require.NoError(t, err)
	add := func(role message.MessageRole, text string) string {
		parts := []message.ContentPart{message.TextContent{Text: text}}
		if role == message.Assistant {
			parts = append(parts, message.Finish{Reason: message.FinishReasonEndTurn})
		}
		m, err := engine.Messages.Create(ctx, chat.ID, message.CreateMessageParams{Role: role, Parts: parts})
		require.NoError(t, err)
		return m.ID
	}
	prompt := add(message.User, "Plan a trip")
	coast := add(message.Assistant, "The coast.")
	require.NoError(t, engine.Messages.(message.TreeService).SwitchTree(ctx, chat.ID, prompt))
	mountains := add(message.Assistant, "The mountains.")

	ws := workspace.NewClientWorkspace(c, *created)
	var tree workspace.TreeManagement = ws
	entries, err := tree.Tree(ctx, chat.ID)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, mountains, treeHead(entries))

	require.NoError(t, tree.SwitchTree(ctx, chat.ID, coast))
	entries, err = tree.Tree(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, coast, treeHead(entries))
	msg, err := tree.TreeMessage(ctx, coast)
	require.NoError(t, err)
	require.Equal(t, "The coast.", msg.Content().Text)

	// A change without a fresh read sends the revision the last change
	// answered with, so changes in a row go through.
	require.NoError(t, tree.SwitchTree(ctx, chat.ID, mountains))
	require.NoError(t, tree.LabelTree(ctx, chat.ID, coast, "coast"))
	require.NoError(t, tree.SwitchTree(ctx, chat.ID, coast))

	// A change by another app in between makes that revision stale: the
	// engine refuses until this app reads the tree again.
	otherClient := rt.newClient(t, cwd)
	otherInfo, err := otherClient.CreateWorkspace(ctx, prototest.CreateRequest(proto.Workspace{Path: cwd, DataDir: dataDir}))
	require.NoError(t, err)
	require.Equal(t, created.ID, otherInfo.ID)
	other := workspace.NewClientWorkspace(otherClient, *otherInfo)
	require.NoError(t, other.SwitchTree(ctx, chat.ID, mountains))
	require.ErrorContains(t, tree.SwitchTree(ctx, chat.ID, coast), "the chat's branches changed")
	_, err = tree.Tree(ctx, chat.ID)
	require.NoError(t, err)
	require.NoError(t, tree.SwitchTree(ctx, chat.ID, coast))

	// A plan is applied by its plan ID, once.
	_, err = tree.Tree(ctx, chat.ID)
	require.NoError(t, err)
	var files workspace.FileRestoration = ws
	plan, err := files.PreviewFiles(ctx, chat.ID, mountains, false)
	require.NoError(t, err)
	require.Equal(t, chat.ID, plan.SessionID)
	require.Empty(t, plan.Entries, "no file was changed")
	result, err := files.UndoFiles(ctx, plan)
	require.NoError(t, err)
	require.Zero(t, result.Restored)
	_, err = files.UndoFiles(ctx, plan)
	require.ErrorContains(t, err, "expired")
}
