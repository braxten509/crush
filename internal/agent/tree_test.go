package agent

import (
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
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
	provider := cliagent.NewProvider(config.TypeClaudeCode, t.TempDir(), t.TempDir(), nil, nil, "", false)
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
