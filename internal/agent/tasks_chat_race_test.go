package agent

import (
	"context"
	"sync"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// An app's message in a sub-agent's chat reads the sub-agent's agent and
// model under the lock the fallback order moves them under; run with -race.
func TestChatMessageReadsTheRunUnderTheLock(t *testing.T) {
	reply := func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
		return agentResultWithText("done"), nil
	}
	first, second := newMockAgent("a", 100, reply), newMockAgent("b", 200, reply)
	run := &taskRun{ctx: t.Context(), sub: first, model: first.model, provider: config.ProviderConfig{ID: "a"}}
	h := &taskHub{
		tasks: map[string]*Task{"t1": {ID: "t1", ChildID: "child", Status: TaskRunning}},
		runs:  map[string]*taskRun{"t1": run},
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			next := first
			if i%2 == 0 {
				next = second
			}
			h.mu.Lock()
			run.sub, run.model, run.provider = next, next.model, config.ProviderConfig{ID: next.model.ModelCfg.Provider}
			h.mu.Unlock()
		}
	})
	for range 200 {
		_, ok, err := h.chatMessage(t.Context(), nil, "child", "hello", nil)
		require.True(t, ok)
		require.NoError(t, err)
	}
	wg.Wait()
}
