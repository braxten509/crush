package agent

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

type nativeInstructionProbe struct {
	finishStreamModel
	prompt fantasy.Prompt
	env    []string
}

func (m *nativeInstructionProbe) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.prompt = call.Prompt
	m.env, _ = ctx.Value(tools.ShellEnvContextKey).([]string)
	return m.finishStreamModel.Stream(ctx, call)
}

func TestNativeAgentReceivesSharedInstructionsAndTaskEnvironment(t *testing.T) {
	sa, env := newStreamTestAgent(t)
	store := config.NewTestStore(&config.Config{
		Options:   &config.Options{DisableInstructionFiles: true},
		Providers: csync.NewMap[string, config.ProviderConfig](),
	})
	sa.cfg = store
	sa.tasks = &taskHub{dir: t.TempDir(), c: &coordinator{cfg: store}}
	probe := &nativeInstructionProbe{finishStreamModel: finishStreamModel{text: "done"}}
	model := sa.largeModel.Get()
	model.Model = probe
	sa.largeModel.Set(model)
	sess, err := env.sessions.Create(t.Context(), "native instructions")
	require.NoError(t, err)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "hello"})
	require.NoError(t, err)
	encoded, err := json.Marshal(probe.prompt)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "crush_instruction_sources")
	require.Contains(t, string(encoded), "crush_questions")
	require.Contains(t, string(encoded), "crush_secure_entry")
	require.Equal(t, sa.tasks.env(sess.ID), probe.env)
}
