package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNativeClaudeDoesNotTriggerCrushCompaction(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r init
read -r prompt
echo '{"type":"system","subtype":"init","session_id":"native-context-test"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Done"}],"usage":{"input_tokens":600000,"output_tokens":100}}}'
echo '{"type":"result","subtype":"success"}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := cliagent.NewProvider(config.TypeClaudeCode, dir, dir, nil, nil, "", false)
	model, err := provider.LanguageModel(t.Context(), "opus")
	require.NoError(t, err)
	env := testEnv(t)
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	large := sa.largeModel.Get()
	large.CatwalkCfg.ContextWindow = 400000
	sa.largeModel.Set(large)
	sess, err := env.sessions.Create(t.Context(), "native compaction")
	require.NoError(t, err)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Finish"})
	require.NoError(t, err)
	saved, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, saved.SummaryMessageID, "neither the old 380k boundary nor the fallback limit may compact a native agent")
}
