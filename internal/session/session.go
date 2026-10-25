package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/zeebo/xxh3"
)

type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

// HashID returns the XXH3 hash of a session ID (UUID) as a hex string.
func HashID(id string) string {
	h := xxh3.New()
	h.WriteString(id)
	return fmt.Sprintf("%x", h.Sum(nil))
}

type Todo struct {
	Content    string     `json:"content"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"active_form"`
}

// HasIncompleteTodos returns true if there are any non-completed todos.
func HasIncompleteTodos(todos []Todo) bool {
	for _, todo := range todos {
		if todo.Status != TodoStatusCompleted {
			return true
		}
	}
	return false
}

type Session struct {
	// Directory and DataDirectory identify a saved chat in the all-project catalog.
	Directory        string `json:",omitempty"`
	DataDirectory    string `json:",omitempty"`
	ContextBudget    ContextBudget
	ID               string
	ParentSessionID  string
	Title            string
	MessageCount     int64
	PromptTokens     int64
	CompletionTokens int64
	EstimatedUsage   bool
	SummaryMessageID string
	Cost             float64
	Todos            []Todo
	Channel          string
	// KeptSkills stay on for this chat: every turn reminds the agent of them
	// until the user turns them off.
	KeptSkills []KeptSkill
	// Model is the model this chat runs on; zero means the workspace's.
	Model     ModelChoice
	CreatedAt int64
	UpdatedAt int64
}

// KeptSkill is a skill turned on for a whole chat. ID is the skill's
// catalog ID (its SKILL.md path or builtin ID).
type KeptSkill struct {
	ID   string
	Name string
	// ByAgent marks the skill toggleable: the agent may turn it off. Skills
	// the agent turns on are toggleable; the user picks either state.
	ByAgent bool
}

// ContextBudget is the native agent's compaction boundary, scoped to the
// selected provider/model. Zero tokens means the agent has not reported it.
type ContextBudget struct {
	Provider  string
	Model     string
	Tokens    int64
	Estimated bool
}

// ModelChoice is the model one chat runs on, as config.SelectedModel JSON so
// every setting of it (effort, FAST, thinking, account) stays with the chat.
// A chat without one uses the workspace's model: a model picked in a chat
// never reaches another (owner, 2026-10-23).
type ModelChoice = json.RawMessage

type Service interface {
	pubsub.Subscriber[Session]
	Create(ctx context.Context, title string) (Session, error)
	CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error)
	CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	GetLast(ctx context.Context) (Session, error)
	List(ctx context.Context) ([]Session, error)
	Save(ctx context.Context, session Session) (Session, error)
	// SetModel saves the model this chat runs on; nil clears it, so the chat
	// follows the workspace again.
	SetModel(ctx context.Context, sessionID string, model ModelChoice) (Session, error)
	// SetNewChatModel sets where a new chat's model comes from (the last
	// pick); Create saves it with the chat.
	SetNewChatModel(fn func(context.Context) ModelChoice)
	// AdoptModels gives every top-level chat without a model the one pick
	// returns, and reports how many it gave one.
	AdoptModels(ctx context.Context, pick func(context.Context, Session) ModelChoice) (int, error)
	SetChannel(ctx context.Context, sessionID, channel string) (Session, error)
	UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens int64, cost float64) error
	Rename(ctx context.Context, id string, title string) error
	Delete(ctx context.Context, id string) error
	MCPDisabledServers(ctx context.Context) ([]string, error)
	SetMCPServerDisabled(ctx context.Context, name string, disabled bool) error
	MCPServersEnabled(ctx context.Context) ([]string, error)
	// SetKeptSkill turns a skill on or off for a whole chat.
	SetKeptSkill(ctx context.Context, sessionID string, skill KeptSkill, on bool) (Session, error)
	// TurnedOffSkills lists the skills that were on in a chat and are now
	// off.
	TurnedOffSkills(ctx context.Context, sessionID string) ([]KeptSkill, error)

	// Agent tool session management
	CreateAgentToolSessionID(messageID, toolCallID string) string
	ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool)
	IsAgentToolSession(sessionID string) bool
}

type service struct {
	*pubsub.Broker[Session]
	db                 *sql.DB
	q                  *db.Queries
	fileHistoryCleanup []func(context.Context) error

	// Estimated usage stays in memory so fetch-modify-save paths (e.g.,
	// updating todos or parent-session cost) do not rebuild a session from
	// SQLite and incorrectly clear the UI "~" marker.
	estimatedUsageMu sync.RWMutex
	estimatedUsage   map[string]bool

	newChatModel atomic.Pointer[func(context.Context) ModelChoice]
}

func (s *service) SetNewChatModel(fn func(context.Context) ModelChoice) {
	s.newChatModel.Store(&fn)
}

// createWithModel makes a chat and saves model with it in one transaction,
// so no chat is ever seen without its model.
func (s *service) createWithModel(ctx context.Context, params db.CreateSessionParams, model ModelChoice) (Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	dbSession, err := s.q.WithTx(tx).CreateSession(ctx, params)
	if err != nil {
		return Session{}, err
	}
	if len(model) > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_models (session_id, choice) VALUES (?, ?)`, dbSession.ID, string(model)); err != nil {
			return Session{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return s.fromDBItem(dbSession), nil
}

func (s *service) Create(ctx context.Context, title string) (Session, error) {
	var model ModelChoice
	if fn := s.newChatModel.Load(); fn != nil && *fn != nil {
		model = (*fn)(ctx)
	}
	session, err := s.createWithModel(ctx, db.CreateSessionParams{
		ID:    uuid.New().String(),
		Title: title,
	}, model)
	if err != nil {
		return Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, session)
	event.SessionCreated()
	return session, nil
}

// CreateTaskSession makes a sub-agent's chat. It starts on its parent's
// model; a sub-agent on another model saves its own over it.
func (s *service) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error) {
	model, err := s.readModel(parentSessionID)
	if err != nil && err != sql.ErrNoRows {
		return Session{}, err
	}
	session, err := s.createWithModel(ctx, db.CreateSessionParams{
		ID:              toolCallID,
		ParentSessionID: sql.NullString{String: parentSessionID, Valid: true},
		Title:           title,
	}, model)
	if err != nil {
		return Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, session)
	return session, nil
}

// AdoptModels gives each top-level chat that has no model the one pick
// returns; an empty choice leaves the chat as it is.
func (s *service) AdoptModels(ctx context.Context, pick func(context.Context, Session) ModelChoice) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM sessions
		WHERE (parent_session_id IS NULL OR parent_session_id = '')
		AND id NOT IN (SELECT session_id FROM session_models)`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	adopted := 0
	for _, id := range ids {
		chat, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		model := pick(ctx, chat)
		if len(model) == 0 {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO session_models (session_id, choice) VALUES (?, ?)
			ON CONFLICT(session_id) DO NOTHING`, id, string(model)); err != nil {
			return adopted, err
		}
		adopted++
	}
	return adopted, nil
}

func (s *service) CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error) {
	dbSession, err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:              "title-" + parentSessionID,
		ParentSessionID: sql.NullString{String: parentSessionID, Valid: true},
		Title:           "Generate a title",
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.Publish(pubsub.CreatedEvent, session)
	return session, nil
}

func (s *service) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := s.q.WithTx(tx)

	dbSession, err := qtx.GetSessionByID(ctx, id)
	if err != nil {
		return err
	}
	if err = qtx.DeleteSessionMessages(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session messages: %w", err)
	}
	if err = qtx.DeleteSessionFiles(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session files: %w", err)
	}
	if err = qtx.DeleteSession(ctx, dbSession.ID); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	session := s.fromDBItem(dbSession)
	s.clearEstimatedUsageState(dbSession.ID)
	s.Publish(pubsub.DeletedEvent, session)
	event.SessionDeleted()
	for _, cleanup := range s.fileHistoryCleanup {
		if err := cleanup(ctx); err != nil {
			slog.Warn("File history cleanup failed after chat deletion", "error", err)
		}
	}
	return nil
}

func (s *service) Get(ctx context.Context, id string) (Session, error) {
	dbSession, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.applyEstimatedUsageState(&session)
	return session, nil
}

func (s *service) GetLast(ctx context.Context) (Session, error) {
	dbSession, err := s.q.GetLastSession(ctx)
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.applyEstimatedUsageState(&session)
	return session, nil
}

func (s *service) Save(ctx context.Context, session Session) (Session, error) {
	todosJSON, err := marshalTodos(session.Todos)
	if err != nil {
		return Session{}, err
	}

	dbSession, err := s.q.UpdateSession(ctx, db.UpdateSessionParams{
		ID:               session.ID,
		Title:            session.Title,
		PromptTokens:     session.PromptTokens,
		CompletionTokens: session.CompletionTokens,
		SummaryMessageID: sql.NullString{
			String: session.SummaryMessageID,
			Valid:  session.SummaryMessageID != "",
		},
		Cost: session.Cost,
		Todos: sql.NullString{
			String: todosJSON,
			Valid:  todosJSON != "",
		},
		Channel: sql.NullString{
			String: session.Channel,
			Valid:  session.Channel != "",
		},
	})
	if err != nil {
		return Session{}, err
	}
	estimatedUsage := session.EstimatedUsage
	budget := session.ContextBudget
	if budget.Provider != "" {
		_, err = s.db.ExecContext(ctx, `INSERT INTO session_context_budgets
			(session_id, provider, model, token_limit, estimated) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET provider=excluded.provider,
			model=excluded.model, token_limit=excluded.token_limit, estimated=excluded.estimated`,
			session.ID, budget.Provider, budget.Model, budget.Tokens, budget.Estimated)
		if err != nil {
			return Session{}, err
		}
	}
	s.setEstimatedUsageState(session.ID, estimatedUsage)
	session = s.fromDBItem(dbSession)
	session.EstimatedUsage = estimatedUsage
	s.Publish(pubsub.UpdatedEvent, session)
	return session, nil
}

func (s *service) SetChannel(ctx context.Context, sessionID, channel string) (Session, error) {
	dbSession, err := s.q.SetSessionChannel(ctx, db.SetSessionChannelParams{
		ID: sessionID,
		Channel: sql.NullString{
			String: channel,
			Valid:  channel != "",
		},
	})
	if err != nil {
		return Session{}, err
	}
	session := s.fromDBItem(dbSession)
	s.applyEstimatedUsageState(&session)
	s.Publish(pubsub.UpdatedEvent, session)
	return session, nil
}

// UpdateTitleAndUsage updates only the title and usage fields atomically.
// This is safer than fetching, modifying, and saving the entire session.
func (s *service) UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens int64, cost float64) error {
	if err := s.q.UpdateSessionTitleAndUsage(ctx, db.UpdateSessionTitleAndUsageParams{
		ID:               sessionID,
		Title:            title,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Cost:             cost,
	}); err != nil {
		return err
	}
	s.publishSessionUpdate(ctx, sessionID)
	return nil
}

// Rename updates only the title of a session without touching updated_at or
// usage fields.
func (s *service) Rename(ctx context.Context, id string, title string) error {
	if err := s.q.RenameSession(ctx, db.RenameSessionParams{
		ID:    id,
		Title: title,
	}); err != nil {
		return err
	}
	s.publishSessionUpdate(ctx, id)
	return nil
}

func (s *service) List(ctx context.Context) ([]Session, error) {
	dbSessions, err := s.q.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, len(dbSessions))
	for i, dbSession := range dbSessions {
		sessions[i] = s.fromDBItem(dbSession)
		s.applyEstimatedUsageState(&sessions[i])
	}
	return sessions, nil
}

// publishSessionUpdate re-fetches a session and publishes an UpdatedEvent so
// that UI subscribers reflect title or usage changes.
func (s *service) publishSessionUpdate(ctx context.Context, sessionID string) {
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		slog.Error("Failed to re-fetch session for event publish", "error", err, "sessionID", sessionID)
		return
	}
	s.Publish(pubsub.UpdatedEvent, session)
}

func (s *service) applyEstimatedUsageState(session *Session) {
	s.estimatedUsageMu.RLock()
	session.EstimatedUsage = s.estimatedUsage[session.ID]
	s.estimatedUsageMu.RUnlock()
}

func (s *service) setEstimatedUsageState(sessionID string, estimatedUsage bool) {
	s.estimatedUsageMu.Lock()
	defer s.estimatedUsageMu.Unlock()
	if estimatedUsage {
		s.estimatedUsage[sessionID] = true
		return
	}
	delete(s.estimatedUsage, sessionID)
}

func (s *service) clearEstimatedUsageState(sessionID string) {
	s.estimatedUsageMu.Lock()
	delete(s.estimatedUsage, sessionID)
	s.estimatedUsageMu.Unlock()
}

func (s *service) fromDBItem(item db.Session) Session {
	var budget ContextBudget
	errBudget := s.db.QueryRow(`SELECT provider, model, token_limit, estimated
		FROM session_context_budgets WHERE session_id = ?`, item.ID).
		Scan(&budget.Provider, &budget.Model, &budget.Tokens, &budget.Estimated)
	if errBudget != nil && errBudget != sql.ErrNoRows {
		slog.Error("Failed to read context budget", "error", errBudget, "session_id", item.ID)
	}
	model, errModel := s.readModel(item.ID)
	if errModel != nil && errModel != sql.ErrNoRows {
		slog.Error("Failed to read the chat's model", "error", errModel, "session_id", item.ID)
	}
	kept, errKept := s.keptSkills(item.ID)
	if errKept != nil {
		slog.Error("Failed to read kept skills", "error", errKept, "session_id", item.ID)
	}
	todos, err := unmarshalTodos(item.Todos.String)
	if err != nil {
		slog.Error("Failed to unmarshal todos", "session_id", item.ID, "error", err)
	}
	return Session{
		ContextBudget:    budget,
		Model:            model,
		ID:               item.ID,
		ParentSessionID:  item.ParentSessionID.String,
		Title:            item.Title,
		MessageCount:     item.MessageCount,
		PromptTokens:     item.PromptTokens,
		CompletionTokens: item.CompletionTokens,
		SummaryMessageID: item.SummaryMessageID.String,
		Cost:             item.Cost,
		Todos:            todos,
		Channel:          item.Channel.String,
		KeptSkills:       kept,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

// readModel reads the model the chat runs on; sql.ErrNoRows means the chat
// follows the workspace's model.
func (s *service) readModel(sessionID string) (ModelChoice, error) {
	var choice string
	err := s.db.QueryRow(`SELECT choice FROM session_models WHERE session_id = ?`, sessionID).Scan(&choice)
	if err != nil {
		return nil, err
	}
	return ModelChoice(choice), nil
}

// SetModel saves the model this chat runs on. An empty choice clears it, so
// the chat follows the workspace again.
func (s *service) SetModel(ctx context.Context, sessionID string, model ModelChoice) (Session, error) {
	if len(model) == 0 {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM session_models WHERE session_id = ?`, sessionID); err != nil {
			return Session{}, err
		}
	} else if _, err := s.db.ExecContext(ctx, `INSERT INTO session_models (session_id, choice)
		VALUES (?, ?) ON CONFLICT(session_id) DO UPDATE SET choice=excluded.choice`,
		sessionID, string(model)); err != nil {
		return Session{}, err
	}
	current, err := s.Get(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	s.Publish(pubsub.UpdatedEvent, current)
	return current, nil
}

func (s *service) keptSkills(sessionID string) ([]KeptSkill, error) {
	return s.keptSkillsWhere(context.Background(), sessionID, false)
}

// TurnedOffSkills lists the skills that were on in a chat and are now off.
func (s *service) TurnedOffSkills(ctx context.Context, sessionID string) ([]KeptSkill, error) {
	return s.keptSkillsWhere(ctx, sessionID, true)
}

func (s *service) keptSkillsWhere(ctx context.Context, sessionID string, turnedOff bool) ([]KeptSkill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill_id, name, by_agent FROM session_kept_skills
		WHERE session_id = ? AND turned_off = ? ORDER BY position`, sessionID, turnedOff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var kept []KeptSkill
	for rows.Next() {
		var skill KeptSkill
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.ByAgent); err != nil {
			return nil, err
		}
		kept = append(kept, skill)
	}
	return kept, rows.Err()
}

// SetKeptSkill turns a skill on or off for a whole chat and publishes the
// updated session so every client shows the change. skill.ByAgent makes it
// toggleable; turning on a skill the user has on without it keeps it that
// way, so the agent can't make the user's skill toggleable.
func (s *service) SetKeptSkill(ctx context.Context, sessionID string, skill KeptSkill, on bool) (Session, error) {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, `INSERT INTO session_kept_skills (session_id, skill_id, name, position, by_agent)
			VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM session_kept_skills WHERE session_id = ?), ?)
			ON CONFLICT(session_id, skill_id) DO UPDATE SET name = excluded.name,
				by_agent = CASE WHEN turned_off = 0 AND by_agent = 0 THEN 0 ELSE excluded.by_agent END,
				turned_off = 0`,
			sessionID, skill.ID, skill.Name, sessionID, skill.ByAgent)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE session_kept_skills SET turned_off = 1 WHERE session_id = ? AND skill_id = ?`,
			sessionID, skill.ID)
	}
	if err != nil {
		return Session{}, err
	}
	updated, err := s.Get(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	s.Publish(pubsub.UpdatedEvent, updated)
	return updated, nil
}

func marshalTodos(todos []Todo) (string, error) {
	if len(todos) == 0 {
		return "", nil
	}
	data, err := json.Marshal(todos)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalTodos(data string) ([]Todo, error) {
	if data == "" {
		return []Todo{}, nil
	}
	var todos []Todo
	if err := json.Unmarshal([]byte(data), &todos); err != nil {
		return []Todo{}, err
	}
	return todos, nil
}

// MCPDisabledServers returns the MCP servers disabled for this
// repository. The database is scoped to the project, so the override set
// is shared by every session in the repository, including sub-agent
// sessions. New repositories start empty and follow the config.
func (s *service) MCPDisabledServers(ctx context.Context) ([]string, error) {
	return s.q.ListMCPDisabledServers(ctx)
}

// SetMCPServerDisabled adds or removes a repository-scoped MCP override.
// Enabling a config-disabled server also records an enabled override so the
// runtime start survives a restart; the override is removed when the server
// is disabled again.
func (s *service) SetMCPServerDisabled(ctx context.Context, name string, disabled bool) error {
	var err error
	if disabled {
		err = s.q.InsertMCPDisabledServer(ctx, name)
	} else {
		err = s.q.DeleteMCPDisabledServer(ctx, name)
	}
	if err != nil {
		return err
	}
	if disabled {
		return s.q.DeleteMCPEnabledServer(ctx, name)
	}
	return s.q.InsertMCPEnabledServer(ctx, name)
}

// MCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override: config-disabled servers the user enabled for this
// repository. Startup force-starts them so the override survives restarts.
func (s *service) MCPServersEnabled(ctx context.Context) ([]string, error) {
	return s.q.ListMCPEnabledServers(ctx)
}

// HasMCPOverride reports whether the named MCP server has a
// repository-scoped override, which takes precedence over the config.
func HasMCPOverride(ctx context.Context, s Service, name string) (bool, error) {
	disabled, err := s.MCPDisabledServers(ctx)
	if err != nil {
		return false, err
	}
	enabled, err := s.MCPServersEnabled(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(disabled, name) || slices.Contains(enabled, name), nil
}

func NewService(q *db.Queries, conn *sql.DB, cleanup ...func(context.Context) error) Service {
	broker := pubsub.NewBroker[Session]()
	return &service{
		Broker:             broker,
		fileHistoryCleanup: cleanup,
		db:                 conn,
		q:                  q,
		estimatedUsage:     make(map[string]bool),
	}
}

// CreateAgentToolSessionID creates a session ID for agent tool sessions using the format "messageID$$toolCallID"
func (s *service) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return fmt.Sprintf("%s$$%s", messageID, toolCallID)
}

// ParseAgentToolSessionID parses an agent tool session ID into its components
func (s *service) ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool) {
	parts := strings.Split(sessionID, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// IsAgentToolSession checks if a session ID follows the agent tool session format
func (s *service) IsAgentToolSession(sessionID string) bool {
	_, _, ok := s.ParseAgentToolSessionID(sessionID)
	return ok
}
