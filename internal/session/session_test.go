package session

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
)

func TestEstimatedUsageStateSurvivesFetchModifySave(t *testing.T) {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})

	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)

	sessions := NewService(db.New(conn), conn)

	created, err := sessions.Create(t.Context(), "test")
	require.NoError(t, err)
	created.PromptTokens = 100
	created.CompletionTokens = 50
	created.EstimatedUsage = true

	saved, err := sessions.Save(t.Context(), created)
	require.NoError(t, err)
	require.True(t, saved.EstimatedUsage)

	fetched, err := sessions.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.True(t, fetched.EstimatedUsage)

	fetched.Todos = []Todo{{
		Content:    "Check estimate state",
		Status:     TodoStatusInProgress,
		ActiveForm: "Checking estimate state",
	}}

	updated, err := sessions.Save(t.Context(), fetched)
	require.NoError(t, err)
	require.True(t, updated.EstimatedUsage)

	refetched, err := sessions.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.True(t, refetched.EstimatedUsage)
}

func TestSessionChannelPersists(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})

	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := NewService(db.New(conn), conn)

	created, err := sessions.Create(t.Context(), "channel")
	require.NoError(t, err)
	updated, err := sessions.SetChannel(t.Context(), created.ID, "signal")
	require.NoError(t, err)
	require.Equal(t, "signal", updated.Channel)

	fetched, err := sessions.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, "signal", fetched.Channel)
}

func TestMCPServerDisabledRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})

	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)

	sessions := NewService(db.New(conn), conn)

	disabled, err := sessions.MCPDisabledServers(t.Context())
	require.NoError(t, err)
	require.Empty(t, disabled, "a new repository must default to the config")

	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "docker", true))
	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "serena", true))
	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "docker", true), "disabling twice must be idempotent")

	disabled, err = sessions.MCPDisabledServers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"docker", "serena"}, disabled)

	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "docker", false))
	disabled, err = sessions.MCPDisabledServers(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"serena"}, disabled)

	// Enabling records an enabled override so a config-disabled server
	// stays enabled across restarts; disabling removes it again.
	enabled, err := sessions.MCPServersEnabled(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"docker"}, enabled)

	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "docker", true))
	enabled, err = sessions.MCPServersEnabled(t.Context())
	require.NoError(t, err)
	require.Empty(t, enabled)
}

func TestEstimatedUsageStateCanBeClearedByExplicitSave(t *testing.T) {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})

	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)

	sessions := NewService(db.New(conn), conn)

	created, err := sessions.Create(t.Context(), "test")
	require.NoError(t, err)
	created.PromptTokens = 100
	created.CompletionTokens = 50
	created.EstimatedUsage = true

	saved, err := sessions.Save(t.Context(), created)
	require.NoError(t, err)
	require.True(t, saved.EstimatedUsage)

	saved.EstimatedUsage = false
	updated, err := sessions.Save(t.Context(), saved)
	require.NoError(t, err)
	require.False(t, updated.EstimatedUsage)

	refetched, err := sessions.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.False(t, refetched.EstimatedUsage)
}

func TestDeleteSucceedsWhenHistoryCleanupFails(t *testing.T) {
	directory := t.TempDir()
	conn, err := db.Connect(t.Context(), directory)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(directory) })
	called := false
	sessions := NewService(db.New(conn), conn, func(context.Context) error { called = true; return errors.New("disk unavailable") })
	chat, err := sessions.Create(t.Context(), "test")
	require.NoError(t, err)
	require.NoError(t, sessions.Delete(t.Context(), chat.ID))
	require.True(t, called)
	_, err = sessions.Get(t.Context(), chat.ID)
	require.Error(t, err)
}

// testSessions is a session service on a fresh database, with the database
// for raw edits.
func testSessions(t *testing.T) (Service, *sql.DB) {
	t.Helper()
	directory := t.TempDir()
	conn, err := db.Connect(t.Context(), directory)
	require.NoError(t, err)
	t.Cleanup(func() { db.Release(directory) })
	return NewService(db.New(conn), conn), conn
}

// `crush run --continue` resumes the last top-level chat, never a
// sub-agent's or title chat under it.
func TestGetLastSkipsChildChats(t *testing.T) {
	sessions, conn := testSessions(t)
	parent, err := sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	child, err := sessions.CreateTaskSession(t.Context(), "call-1", parent.ID, "helper")
	require.NoError(t, err)
	// The trigger stamps updated_at with now, so drop it to set the times.
	_, err = conn.ExecContext(t.Context(), `DROP TRIGGER update_sessions_updated_at`)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE sessions SET updated_at = CASE id WHEN ? THEN 1000 ELSE 2000 END`, parent.ID)
	require.NoError(t, err)

	last, err := sessions.GetLast(t.Context())
	require.NoError(t, err)
	require.Equal(t, parent.ID, last.ID, "not the child %s", child.ID)
}

// Deleting a chat deletes its sub-agent and title chats too, all the way
// down (owner, 2026-10-24).
func TestDeleteRemovesChildChats(t *testing.T) {
	sessions, _ := testSessions(t)
	parent, err := sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	child, err := sessions.CreateTaskSession(t.Context(), "call-1", parent.ID, "helper")
	require.NoError(t, err)
	title, err := sessions.CreateTitleSession(t.Context(), parent.ID)
	require.NoError(t, err)
	grandchild, err := sessions.CreateTaskSession(t.Context(), "call-2", child.ID, "helper's helper")
	require.NoError(t, err)

	require.NoError(t, sessions.Delete(t.Context(), parent.ID))
	for _, id := range []string{parent.ID, child.ID, title.ID, grandchild.ID} {
		_, err = sessions.Get(t.Context(), id)
		require.Error(t, err, id)
	}
}

// Renaming a chat keeps its place in the list: only its title changes.
func TestRenameKeepsTheChatsPlace(t *testing.T) {
	sessions, conn := testSessions(t)
	chat, err := sessions.Create(t.Context(), "old name")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `UPDATE sessions SET updated_at = 1000 WHERE id = ?`, chat.ID)
	require.NoError(t, err)

	require.NoError(t, sessions.Rename(t.Context(), chat.ID, "new name"))
	got, err := sessions.Get(t.Context(), chat.ID)
	require.NoError(t, err)
	require.Equal(t, "new name", got.Title)
	require.EqualValues(t, 1000, got.UpdatedAt)
}

func TestKeptSkillsPersistInOrder(t *testing.T) {
	// Not parallel: parallel tests' db.ResetPool closes every pooled
	// database, including this one.
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})

	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := NewService(db.New(conn), conn)

	created, err := sessions.Create(t.Context(), "test")
	require.NoError(t, err)
	require.Empty(t, created.KeptSkills)

	alpha := KeptSkill{ID: "/skills/alpha/SKILL.md", Name: "alpha"}
	beta := KeptSkill{ID: "/skills/beta/SKILL.md", Name: "beta"}
	updated, err := sessions.SetKeptSkill(t.Context(), created.ID, alpha, true)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{alpha}, updated.KeptSkills)

	_, err = sessions.SetKeptSkill(t.Context(), created.ID, beta, true)
	require.NoError(t, err)
	// Turning a skill on again keeps its place and takes the new name.
	alpha.Name = "alpha renamed"
	_, err = sessions.SetKeptSkill(t.Context(), created.ID, alpha, true)
	require.NoError(t, err)

	fetched, err := sessions.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{alpha, beta}, fetched.KeptSkills)

	updated, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: alpha.ID}, false)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{beta}, updated.KeptSkills)
	off, err := sessions.TurnedOffSkills(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{alpha}, off, "a turned-off skill is remembered as off")

	// Turning it back on takes it off the turned-off list.
	_, err = sessions.SetKeptSkill(t.Context(), created.ID, alpha, true)
	require.NoError(t, err)
	off, err = sessions.TurnedOffSkills(t.Context(), created.ID)
	require.NoError(t, err)
	require.Empty(t, off)
	updated, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: alpha.ID}, false)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{beta}, updated.KeptSkills)

	// Turning off a skill that is not on is harmless.
	_, err = sessions.SetKeptSkill(t.Context(), created.ID, alpha, false)
	require.NoError(t, err)

	// Kept skills belong to their own chat.
	other, err := sessions.Create(t.Context(), "other")
	require.NoError(t, err)
	fetchedOther, err := sessions.Get(t.Context(), other.ID)
	require.NoError(t, err)
	require.Empty(t, fetchedOther.KeptSkills)
}

// The chat remembers who turned each skill on. The agent turning on a skill
// the user already has on leaves it the user's; the user turning on the
// agent's skill takes it over.
func TestKeptSkillsRememberWhoTurnedThemOn(t *testing.T) {
	// Not parallel, like TestKeptSkillsPersistInOrder.
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := NewService(db.New(conn), conn)
	created, err := sessions.Create(t.Context(), "chat")
	require.NoError(t, err)

	mine := KeptSkill{ID: "/skills/mine/SKILL.md", Name: "mine"}
	theirs := KeptSkill{ID: "/skills/theirs/SKILL.md", Name: "theirs", ByAgent: true}
	_, err = sessions.SetKeptSkill(t.Context(), created.ID, mine, true)
	require.NoError(t, err)
	updated, err := sessions.SetKeptSkill(t.Context(), created.ID, theirs, true)
	require.NoError(t, err)
	require.Equal(t, []KeptSkill{mine, theirs}, updated.KeptSkills)

	updated, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: mine.ID, Name: mine.Name, ByAgent: true}, true)
	require.NoError(t, err)
	require.False(t, updated.KeptSkills[0].ByAgent, "the agent can't take over the user's skill")

	updated, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: theirs.ID, Name: theirs.Name}, true)
	require.NoError(t, err)
	require.False(t, updated.KeptSkills[1].ByAgent, "the user turning it on makes it theirs")

	_, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: mine.ID}, false)
	require.NoError(t, err)
	updated, err = sessions.SetKeptSkill(t.Context(), created.ID, KeptSkill{ID: mine.ID, Name: mine.Name, ByAgent: true}, true)
	require.NoError(t, err)
	require.Equal(t, mine.ID, updated.KeptSkills[0].ID, "it keeps its place in the list")
	require.True(t, updated.KeptSkills[0].ByAgent, "after the user turned it off, the agent may turn it back on")
}
