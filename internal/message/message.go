package message

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// defaultUpdateDebounce is the default debounce window for [Service.Update].
// Streaming deltas that arrive within the window are coalesced into a
// single SQL write and a single pubsub event. Terminal updates
// (finish/error/cancel/tool-call structural changes) bypass the
// debounce and flush synchronously.
const defaultUpdateDebounce = 33 * time.Millisecond

type CreateMessageParams struct {
	Role             MessageRole
	Parts            []ContentPart
	Model            string
	Provider         string
	IsSummaryMessage bool
}

// Service is the public interface to the message store.
//
// [Service.Update] is eventually consistent: it accepts new state into
// an in-memory buffer and writes it to SQLite plus publishes a
// [pubsub.UpdatedEvent] on the next debounce tick (default
// [defaultUpdateDebounce]) or on the next terminal-state update,
// whichever comes first. Terminal-state updates — those that finish
// the message, add or finish a tool call, or end a reasoning section —
// flush synchronously before [Service.Update] returns.
//
// Callers that need stronger ordering (e.g. tests, shutdown,
// session-switch reads) must use [Service.Flush] or [Service.FlushAll]
// before reading via [Service.Get] / [Service.List]. Without an
// explicit flush, a read can race the debounce timer and miss the
// most recent in-memory state.
type Service interface {
	pubsub.Subscriber[Message]
	Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error)
	Update(ctx context.Context, message Message) error
	Get(ctx context.Context, id string) (Message, error)
	LoadReview(ctx context.Context, id string) (Message, error)
	List(ctx context.Context, sessionID string) ([]Message, error)
	// ListFromSummary returns the messages at or after summaryMessageID,
	// which is all a compacted session still sends.
	ListFromSummary(ctx context.Context, sessionID, summaryMessageID string) ([]Message, error)
	ListUserMessages(ctx context.Context, sessionID string) ([]Message, error)
	ListAllUserMessages(ctx context.Context) ([]Message, error)
	GetLastAssistantMessage(ctx context.Context, sessionID string) (Message, error)
	Delete(ctx context.Context, id string) error
	DeleteSessionMessages(ctx context.Context, sessionID string) error

	// Flush synchronously drains any pending debounced state for the
	// given message ID, performs the SQL write, and publishes the
	// resulting [pubsub.UpdatedEvent]. Idempotent; cheap no-op if no
	// updates are pending. Use this before any read that must observe
	// the latest [Service.Update].
	Flush(ctx context.Context, id string) error

	// FlushAll synchronously drains pending debounced state for every
	// message known to the service. Intended for shutdown and
	// session-switch paths.
	FlushAll(ctx context.Context) error
}

type service struct {
	*pubsub.Broker[Message]
	q        db.Querier
	debounce time.Duration

	mu          sync.Mutex
	pending     map[string]*pendingState
	reviewMu    sync.Mutex
	fileHistory *filehistory.Store
}

// ServiceOption configures a [Service] at construction.
type ServiceOption func(*service)

// WithDebounce overrides the debounce window for [Service.Update]. A
// zero or negative value disables debouncing entirely (every update
// flushes synchronously). Intended primarily for tests.
func WithDebounce(d time.Duration) ServiceOption {
	return func(s *service) {
		s.debounce = d
	}
}

func NewService(q db.Querier, opts ...ServiceOption) Service {
	s := &service{
		Broker:   pubsub.NewBroker[Message](),
		q:        q,
		debounce: defaultUpdateDebounce,
		pending:  make(map[string]*pendingState),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *service) Delete(ctx context.Context, id string) error {
	message, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	err = s.q.DeleteMessage(ctx, message.ID)
	if err != nil {
		return err
	}
	// Drop any pending coalesced state for this ID. We never want to
	// flush back over a deleted row.
	s.mu.Lock()
	if p, ok := s.pending[id]; ok {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(s.pending, id)
	}
	s.mu.Unlock()
	// Clone the message before publishing to avoid race conditions with
	// concurrent modifications to the Parts slice.
	s.Publish(pubsub.DeletedEvent, message.Clone())
	return nil
}

func (s *service) Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error) {
	if params.Role != Assistant {
		params.Parts = append(params.Parts, Finish{
			Reason: "stop",
		})
	}
	id := uuid.New().String()
	if s.fileHistory != nil {
		if err := s.fileHistory.RecordPoint(ctx, sessionID, id); err != nil {
			slog.Warn("Could not record file history point", "error", err)
		}
	}
	s.captureFiles(ctx, id, sessionID, params.Parts)
	var payload []byte
	storage, separate := s.q.(reviewStorage)
	if separate {
		var err error
		params.Parts, payload, err = separateReviews(id, sessionID, params.Parts)
		if err != nil {
			return Message{}, err
		}
	}
	partsJSON, err := marshalParts(params.Parts)
	if err != nil {
		return Message{}, err
	}
	isSummary := int64(0)
	if params.IsSummaryMessage {
		isSummary = 1
	}
	arg := db.CreateMessageParams{
		ID:               id,
		SessionID:        sessionID,
		Role:             string(params.Role),
		Parts:            string(partsJSON),
		Model:            sql.NullString{String: string(params.Model), Valid: true},
		Provider:         sql.NullString{String: params.Provider, Valid: params.Provider != ""},
		IsSummaryMessage: isSummary,
	}
	var dbMessage db.Message
	if separate && len(payload) > 0 {
		dbMessage, err = storage.CreateMessageWithReview(ctx, arg, payload)
	} else {
		dbMessage, err = s.q.CreateMessage(ctx, arg)
	}
	if err != nil {
		return Message{}, err
	}
	message, err := s.fromDBItem(dbMessage)
	if err != nil {
		return Message{}, err
	}
	if separate && message.Role == User {
		if err := storage.PruneMessageReviews(ctx, sessionID); err != nil {
			return Message{}, err
		}
	}
	// Clone the message before publishing to avoid race conditions with
	// concurrent modifications to the Parts slice.
	s.Publish(pubsub.CreatedEvent, message.Clone())
	return message, nil
}

func (s *service) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	// Session deletion is intentionally across all branches, not just the view.
	rows, err := s.q.ListMessagesBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.Delete(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) Get(ctx context.Context, id string) (Message, error) {
	dbMessage, err := s.q.GetMessage(ctx, id)
	if err != nil {
		return Message{}, err
	}
	msg, err := s.fromDBItem(dbMessage)
	if err != nil {
		return Message{}, err
	}
	for _, result := range msg.ToolResults() {
		if result.Review != nil && result.Review.Summary != nil {
			full, err := s.LoadReview(ctx, id)
			if errors.Is(err, ErrReviewExpired) {
				return msg, nil
			}
			return full, err
		}
	}
	return msg, nil
}

func (s *service) List(ctx context.Context, sessionID string) ([]Message, error) {
	if err := s.migrateReviews(ctx, sessionID); err != nil {
		return nil, err
	}
	if err := s.countReviews(ctx, sessionID); err != nil {
		slog.Warn("Could not add line counts to saved reviews", "session", sessionID, "error", err)
	}
	var dbMessages []db.Message
	var err error
	if tree, ok := s.q.(treeStorage); ok {
		dbMessages, err = tree.ListTreeMessages(ctx, sessionID)
	} else {
		dbMessages, err = s.q.ListMessagesBySession(ctx, sessionID)
	}
	if err != nil {
		return nil, err
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMessage := range dbMessages {
		messages[i], err = s.fromDBItem(dbMessage)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *service) ListFromSummary(ctx context.Context, sessionID, summaryMessageID string) ([]Message, error) {
	messages, err := s.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	for i, msg := range messages {
		if msg.ID == summaryMessageID {
			return messages[i:], nil
		}
	}
	return messages, nil
}

func (s *service) ListUserMessages(ctx context.Context, sessionID string) ([]Message, error) {
	messages, err := s.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var users []Message
	for i := len(messages) - 1; i >= 0 && len(users) < 200; i-- {
		if messages[i].Role == User {
			users = append(users, messages[i])
		}
	}
	return users, nil
}

func (s *service) ListAllUserMessages(ctx context.Context) ([]Message, error) {
	dbMessages, err := s.q.ListAllUserMessages(ctx)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMessage := range dbMessages {
		messages[i], err = s.fromDBItem(dbMessage)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *service) GetLastAssistantMessage(ctx context.Context, sessionID string) (Message, error) {
	messages, err := s.List(ctx, sessionID)
	if err != nil {
		return Message{}, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == Assistant && !messages[i].IsSummaryMessage {
			return messages[i], nil
		}
	}
	return Message{}, sql.ErrNoRows
}

func (s *service) fromDBItem(item db.Message) (Message, error) {
	parts, err := unmarshalParts([]byte(item.Parts))
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:                      item.ID,
		SessionID:               item.SessionID,
		Role:                    MessageRole(item.Role),
		Parts:                   parts,
		Model:                   item.Model.String,
		Provider:                item.Provider.String,
		CreatedAt:               item.CreatedAt,
		UpdatedAt:               item.UpdatedAt,
		IsSummaryMessage:        item.IsSummaryMessage != 0,
		PrismModelID:            item.PrismModelID.String,
		PrismModelName:          item.PrismModelName.String,
		PrismHypercreditSavings: floatPtr(item.PrismHypercreditSavings),
		PrismDollarSavings:      floatPtr(item.PrismDollarSavings),
	}, nil
}
