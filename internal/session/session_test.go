package session

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

// A title and usage update that comes while another change is being made
// to the chat is kept: Edit changes the chat as it is when it writes, not
// as it was when the change started.
func TestEditKeepsAConcurrentUsageUpdate(t *testing.T) {
	sessions, _ := testSessions(t)
	chat, err := sessions.Create(t.Context(), "New Session")
	require.NoError(t, err)
	inEdit, release := make(chan struct{}), make(chan struct{})
	edited := make(chan error, 1)
	go func() {
		_, err := sessions.Edit(t.Context(), chat.ID, func(s *Session) bool {
			close(inEdit)
			<-release
			s.Todos = []Todo{{Content: "a", Status: TodoStatusPending}}
			return true
		})
		edited <- err
	}()
	<-inEdit
	usage := make(chan error, 1)
	go func() { usage <- sessions.UpdateTitleAndUsage(t.Context(), chat.ID, "Named", 10, 5, 0.25) }()
	select {
	case err := <-usage: // not held back: it lands in the middle of the edit
		usage <- err
	case <-time.After(200 * time.Millisecond): // waits for the edit
	}
	close(release)
	require.NoError(t, <-edited)
	require.NoError(t, <-usage)

	got, err := sessions.Get(t.Context(), chat.ID)
	require.NoError(t, err)
	require.Equal(t, "Named", got.Title)
	require.EqualValues(t, 10, got.PromptTokens)
	require.Len(t, got.Todos, 1)
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

// Every chat owns its model (owner, 2026-10-24): a new chat starts on the
// last pick, a task chat on its parent's model, and an old chat without one
// is given one once.
func TestNewChatStartsOnTheLastPickAndOldChatsAdoptTheirLastModel(t *testing.T) {
	dataDir := t.TempDir()
	t.Cleanup(func() {
		require.NoError(t, db.Release(dataDir))
		db.ResetPool()
	})
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := NewService(db.New(conn), conn)

	// Chats made before every chat had a model.
	old, err := sessions.Create(t.Context(), "old")
	require.NoError(t, err)
	require.Empty(t, old.Model)
	kept, err := sessions.Create(t.Context(), "kept")
	require.NoError(t, err)
	_, err = sessions.SetModel(t.Context(), kept.ID, ModelChoice(`{"provider":"p","model":"own"}`))
	require.NoError(t, err)

	pick := ModelChoice(`{"provider":"codex-cli","model":"gpt-6-sol","reasoning_effort":"max","account":"codex-2"}`)
	sessions.SetNewChatModel(func(context.Context) ModelChoice { return pick })
	created, err := sessions.Create(t.Context(), "new")
	require.NoError(t, err)
	require.JSONEq(t, string(pick), string(created.Model), "a new chat starts on the last pick")

	_, err = sessions.SetModel(t.Context(), created.ID, ModelChoice(`{"provider":"claude-code","model":"opus"}`))
	require.NoError(t, err)
	task, err := sessions.CreateTaskSession(t.Context(), "task-1", created.ID, "task")
	require.NoError(t, err)
	require.JSONEq(t, `{"provider":"claude-code","model":"opus"}`, string(task.Model), "a task chat starts on its parent's model")

	var asked []string
	adopted, err := sessions.AdoptModels(t.Context(), func(_ context.Context, s Session) ModelChoice {
		asked = append(asked, s.Title)
		return ModelChoice(`{"provider":"claude-code","model":"sonnet"}`)
	})
	require.NoError(t, err)
	require.Equal(t, 1, adopted)
	require.Equal(t, []string{"old"}, asked, "only top-level chats without a model are asked")
	read, err := sessions.Get(t.Context(), old.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"provider":"claude-code","model":"sonnet"}`, string(read.Model))
	read, err = sessions.Get(t.Context(), kept.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"provider":"p","model":"own"}`, string(read.Model), "a chat's own model is left alone")
}
