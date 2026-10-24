package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// kimiLimitModel makes one tool-call step, then holds the next stream open
// until the test lets it finish, so the test can look at the usage meter
// while the turn is still working.
type kimiLimitModel struct {
	finishStreamModel
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (m *kimiLimitModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	switch m.calls.Add(1) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "work", ToolCallName: "work", ToolCallInput: "{}"}) {
				return
			}
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
		}, nil
	case 2:
		close(m.entered)
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.finishStreamModel.Stream(ctx, call)
}

// Moonshot AI's plan is read over HTTP, so its stream carries no new usage:
// the meter must be refreshed as the turn works, not only when it ends.
func TestKimiLimitsRefreshDuringTurn(t *testing.T) {
	env := testEnv(t)
	// Kimi only runs inside a git project (kimi.go).
	require.NoError(t, os.Mkdir(filepath.Join(env.workingDir, ".git"), 0o755))
	model := &kimiLimitModel{
		finishStreamModel: finishStreamModel{text: "done"},
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-model.release:
		default:
			close(model.release)
		}
	})
	work := fantasy.NewAgentTool("work", "Do work", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("work done"), nil
	})
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system", work).(*sessionAgent)
	agent.cfg = config.NewTestStoreAt(env.workingDir, &config.Config{
		Options:   &config.Options{DisableInstructionFiles: true},
		Providers: csync.NewMap[string, config.ProviderConfig](),
	})
	agent.SetModels(
		Model{Model: model, CatwalkCfg: catwalk.Model{ContextWindow: 200_000}, ModelCfg: config.SelectedModel{Provider: KimiProviderID, Model: "k3"}},
		Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: catwalk.Model{ContextWindow: 200_000}, ModelCfg: config.SelectedModel{Provider: KimiProviderID, Model: "k3"}},
	)

	fetched := make(chan struct{}, 1)
	restore := cliagent.SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){
		catwalk.Type(catwalk.InferenceKimiCoding): func(context.Context) ([]cliagent.Limit, error) {
			select {
			case fetched <- struct{}{}:
			default:
			}
			return []cliagent.Limit{{Name: "5h", Used: 42}}, nil
		},
	})
	defer restore()

	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "user request"})
		done <- err
	}()
	select {
	case <-model.entered:
	case err := <-done:
		t.Fatalf("the turn ended before its second step: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the turn did not reach its second step")
	}
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("Moonshot AI limits did not refresh while the turn was still running")
	}
	close(model.release)
	require.NoError(t, <-done)
}
