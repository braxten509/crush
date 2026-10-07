package session

import (
	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContextBudgetSurvivesReloadAndUsageUpdates(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { require.NoError(t, db.Release(dir)); db.ResetPool() })
	conn, err := db.Connect(t.Context(), dir)
	require.NoError(t, err)
	svc := NewService(db.New(conn), conn)
	sess, err := svc.Create(t.Context(), "budget")
	require.NoError(t, err)
	sess.ContextBudget = ContextBudget{Provider: "claude-code", Model: "opus", Tokens: 467000}
	_, err = svc.Save(t.Context(), sess)
	require.NoError(t, err)
	reopened := NewService(db.New(conn), conn)
	require.NoError(t, reopened.UpdateTitleAndUsage(t.Context(), sess.ID, "updated", 300000, 42, 0))
	saved, err := reopened.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ContextBudget, saved.ContextBudget)
	saved.ContextBudget = ContextBudget{Provider: "codex-cli", Model: "other", Tokens: 900000, Estimated: true}
	_, err = reopened.Save(t.Context(), saved)
	require.NoError(t, err)
	listed, err := reopened.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, saved.ContextBudget, listed[0].ContextBudget)
}
