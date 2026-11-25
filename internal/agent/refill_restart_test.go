package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/openchats"
	"github.com/stretchr/testify/require"
)

// A prompt picked up after a summary runs outside the coordinator's original
// call. Its quota error still needs a visible, durable retry.
func TestQueuedRefillSurvivesRestartForOpenChat(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	env := testEnv(t)
	clock := &fakeClock{now: time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC)}
	at := clock.Now().Add(time.Hour + time.Minute)
	first := pendingTestCoordinator(t, env, clock)
	order := useBlock("mock", "", "mock-model")
	first.cfg.Config().Options.Fallback = &config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{order}}
	model := &completionOutcomeModel{err: errors.New("You've hit your 5-hour limit. Resets at 2026-05-12T14:00:00Z")}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.cfg = first.cfg
	large := sa.model.Get()
	large.ModelCfg = fallbackModelOf(order)
	sa.model.Set(large)
	sa.handleQueuedAccountError = first.handleAccountError
	first.mainAgent = sa
	list, err := openchats.Open()
	require.NoError(t, err)
	sid := pendingTestSession(t, env, "continue after refill")
	setChatModel(t, first, sid, large.ModelCfg)
	require.NoError(t, list.Add(openchats.Chat{ID: sid, Dir: env.workingDir}))
	sa.enqueueCall(SessionAgentCall{SessionID: sid, Prompt: "keep working"})
	require.Error(t, sa.runQueuedAfterSummary(t.Context(), sid, func() {}))
	require.Contains(t, strings.Join(chatLines(t, first, sid), "\n"), at.Local().Format("3:04 PM"))
	waits := readPendingFile(t, first).Waits
	require.Len(t, waits, 1)
	require.True(t, at.Equal(waits[0].At))

	closed := pendingTestSession(t, env, "closed while offline")
	require.NoError(t, list.Add(openchats.Chat{ID: closed, Dir: env.workingDir}))
	first.planWait(savedWait{SessionID: closed, At: at, Kind: waitKindLimit}, func() { t.Error("old engine sent a retry") })
	StopAllWork(first)
	require.NoError(t, list.Remove(closed))
	next := pendingTestCoordinator(t, env, clock)
	sent := make(chan string, 3)
	next.fallback.send = func(id string) { sent <- id }
	next.restoreWaits()
	next.restoreWaits()
	require.Equal(t, 1, next.limitRetries.count())
	require.True(t, next.limitRetries.has(sid))
	clock.Set(at)
	select {
	case id := <-sent:
		require.Equal(t, sid, id)
	case <-time.After(3 * time.Second):
		t.Fatal("restarted engine missed the refill")
	}
	require.Empty(t, readPendingFile(t, next).Waits)
	next.restoreWaits()
	require.Empty(t, sent)
}
