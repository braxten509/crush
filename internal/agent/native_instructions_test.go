package agent

import (
	"context"
	"strings"
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
	model := sa.model.Get()
	model.Model = probe
	sa.model.Set(model)
	sess, err := env.sessions.Create(t.Context(), "native instructions")
	require.NoError(t, err)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "hello"})
	require.NoError(t, err)
	var text strings.Builder
	for _, m := range probe.prompt {
		for _, part := range m.Content {
			if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
				text.WriteString(tp.Text)
			}
		}
	}
	require.Contains(t, text.String(), sharedCLIInstructions)
	require.Contains(t, text.String(), sa.tasks.instructions())
	require.Equal(t, sa.tasks.env(sess.ID), probe.env)
}
