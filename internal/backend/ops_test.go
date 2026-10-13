package backend

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// renamingSessions records Rename and Delete calls.
type renamingSessions struct {
	session.Service
	renamed map[string]string
	deleted []string
}

func (s *renamingSessions) Rename(_ context.Context, id, title string) error {
	s.renamed[id] = title
	return nil
}

func (s *renamingSessions) Delete(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func newOpsWithSessions(t *testing.T, sessions session.Service) (Ops, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := config.NewTestStore(&config.Config{Options: &config.Options{DataDirectory: dataDir}})
	return Ops{App: &app.App{Sessions: sessions}, Cfg: cfg}, dataDir
}

// TestOpsChangeSavedSessionUsesSessionService is the regression for the
// server editing chats of its own project directly in the database: the
// session service never published the rename or deletion, so attached
// clients kept showing the old title (the in-process workspace already
// went through the service).
func TestOpsChangeSavedSessionUsesSessionService(t *testing.T) {
	t.Parallel()
	sessions := &renamingSessions{renamed: map[string]string{}}
	o, dataDir := newOpsWithSessions(t, sessions)

	require.NoError(t, o.ChangeSavedSession(t.Context(), session.Session{
		ID: "s1", Title: "New title", DataDirectory: dataDir,
	}, false))
	require.Equal(t, map[string]string{"s1": "New title"}, sessions.renamed)

	require.NoError(t, o.ChangeSavedSession(t.Context(), session.Session{
		ID: "s2", DataDirectory: dataDir,
	}, true))
	require.Equal(t, []string{"s2"}, sessions.deleted)
}

// reviewMessages serves a single message for review lookups.
type reviewMessages struct {
	message.Service
	msg message.Message
}

func (m *reviewMessages) Get(context.Context, string) (message.Message, error) { return m.msg, nil }

func (m *reviewMessages) LoadReview(context.Context, string) (message.Message, error) {
	return m.msg, nil
}

func TestOpsLoadMessageReviewChecksSession(t *testing.T) {
	t.Parallel()
	o := Ops{App: &app.App{Messages: &reviewMessages{msg: message.Message{ID: "m1", SessionID: "s1"}}}}

	got, err := o.LoadMessageReview(t.Context(), "s1", "m1")
	require.NoError(t, err)
	require.Equal(t, "m1", got.ID)

	_, err = o.LoadMessageReview(t.Context(), "other", "m1")
	require.ErrorIs(t, err, ErrReviewNotInSession)
}

func TestOpsConfigChangedRunsOnlyOnSuccess(t *testing.T) {
	t.Parallel()
	calls := 0
	o := Ops{ConfigChanged: func() { calls++ }}

	require.NoError(t, o.mutateConfig(func() error { return nil }))
	require.Equal(t, 1, calls)

	require.Error(t, o.mutateConfig(func() error { return context.Canceled }))
	require.Equal(t, 1, calls, "a failed write must not announce a config change")

	// In-process frontends leave the hook unset.
	require.NotPanics(t, func() { _ = Ops{}.mutateConfig(func() error { return nil }) })
}
