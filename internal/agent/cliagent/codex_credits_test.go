package cliagent

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
)

// A replayed usage snapshot or overlapping chat must not add credits to the
// wrong total. Changes to model display names or footer wording are harmless.
func TestCodexCreditUsageStaysWithItsChat(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	conn, err := db.Connect(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(dir) })
	queries := db.New(conn)
	newRun := func(chat, thread, turn string) *codexRun {
		run := newCodexRun(ctx, &Model{ID: "gpt-6.2-sol"}, Turn{
			Emit: func(Event) error { return nil },
			OnCodexUsage: func(usage CodexUsage) {
				require.NoError(t, queries.RecordCodexCredits(ctx, chat, usage.ThreadID, usage.TurnID, usage.Credits))
			},
		})
		run.active.Store(&[2]string{thread, turn})
		return run
	}
	for _, chat := range []string{"chat-a", "chat-b"} {
		_, err := queries.CreateSession(ctx, db.CreateSessionParams{ID: chat, Title: chat})
		require.NoError(t, err)
	}
	send := func(run *codexRun, thread, turn string, total, last codexTokenCounts) {
		payload, err := json.Marshal(map[string]any{
			"threadId": thread, "turnId": turn,
			"tokenUsage": map[string]any{"total": total, "last": last},
		})
		require.NoError(t, err)
		_, err = run.handleNotification(rpcMessage{Method: "thread/tokenUsage/updated", Params: payload})
		require.NoError(t, err)
	}

	a := newRun("chat-a", "thread-a", "turn-a")
	b := newRun("chat-b", "thread-b", "turn-b")
	old := codexTokenCounts{InputTokens: 500_000, OutputTokens: 20_000}
	first := codexTokenCounts{InputTokens: 550_000, CachedInputTokens: 20_000, OutputTokens: 24_000}
	last := codexTokenCounts{InputTokens: 50_000, CachedInputTokens: 20_000, OutputTokens: 4_000}
	next := codexTokenCounts{InputTokens: 600_000, CachedInputTokens: 40_000, OutputTokens: 28_000}
	other := codexTokenCounts{InputTokens: 10_000, OutputTokens: 2_000}

	send(a, "thread-a", "old-turn", old, old)
	send(a, "thread-a", "turn-a", first, last)
	send(b, "thread-b", "turn-b", other, other)
	send(a, "thread-b", "turn-b", other, other)
	send(a, "thread-a", "turn-a", next, last)
	send(a, "thread-a", "turn-a", next, last)
	send(a, "thread-a", "turn-a", first, last)
	send(newRun("chat-a", "thread-a", "turn-a"), "thread-a", "turn-a", next, last)

	credits, err := queries.CodexCreditsUsed(ctx, "chat-a")
	require.NoError(t, err)
	require.InDelta(t, 5.1, credits, 1e-9)
	credits, err = queries.CodexCreditsUsed(ctx, "chat-b")
	require.NoError(t, err)
	require.InDelta(t, 1.0, credits, 1e-9)
}
