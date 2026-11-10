// Package question provides services for asking the user questions
// via the TUI and blocking until an answer is received. It mirrors
// the permission service pattern: publish a request over pubsub,
// block on a channel, and resolve when the UI sends back answers.
//
// Remote answers are correlated with the pending request and session.
package question

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// ErrCancelled is returned by Ask when the user cancels the question.
var ErrCancelled = errors.New("question cancelled by user")

// Type identifies the kind of question to present.
type Type string

const (
	TypeYesNo        Type = "yes_no"
	TypeSingleChoice Type = "single_choice"
	TypeMultiChoice  Type = "multi_choice"
	TypeFreeText     Type = "free_text"
	// TypeSecureEntry is a masked field that saves straight to a prepared
	// file. Only `crush ask` in the local terminal shows it; the shared
	// service refuses it, so it never reaches remote clients.
	TypeSecureEntry Type = "secure_entry"
)

// ErrSecureEntryLocal is returned when a secure entry would leave the local
// terminal.
var ErrSecureEntryLocal = errors.New("secure_entry questions only work through crush ask in the local Crush terminal")

// Choice represents a single selectable option.
type Choice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Image is an optional absolute path to a PNG or JPEG sketch of the
	// choice, shown in the form while the choice is under the cursor.
	Image string `json:"image,omitempty"`
}

// Question is a single question definition within a Request.
type Question struct {
	ID          string   `json:"id"`
	Type        Type     `json:"type"`
	Label       string   `json:"label,omitempty"`
	Text        string   `json:"question"`
	Description string   `json:"description,omitempty"`
	Choices     []Choice `json:"choices,omitempty"`
	// Selected lists the choice IDs a multi_choice question opens with
	// checked.
	Selected []string `json:"selected,omitempty"`
}

// Answer carries the user's response to a single Question.
type Answer struct {
	QuestionID  string            `json:"question_id"`
	SelectedIDs []string          `json:"selected_ids,omitempty"`
	FillInText  string            `json:"fill_in_text,omitempty"`
	Yes         *bool             `json:"yes,omitempty"`
	Notes       map[string]string `json:"notes,omitempty"`
}

// HasNotes reports whether any notes were attached.
func (a Answer) HasNotes() bool { return len(a.Notes) > 0 }

// Request is the service envelope published to the UI. It contains
// one or more Questions. A single question renders without tabs;
// multiple questions render as a tabbed form with confirmation.
type Request struct {
	ID                 string     `json:"id"`
	SessionID          string     `json:"session_id"`
	ToolCallID         string     `json:"tool_call_id"`
	Questions          []Question `json:"questions"`
	ConfirmTitle       string     `json:"confirm_title,omitempty"`
	ConfirmDescription string     `json:"confirm_description,omitempty"`
}

// Validate checks that a Request has valid fields. For multiple
// questions, ConfirmTitle and ConfirmDescription are required.
func (r Request) Validate() error {
	if len(r.Questions) == 0 {
		return fmt.Errorf("at least one question is required")
	}
	if len(r.Questions) > MaxQuestions {
		return fmt.Errorf("questions exceed maximum of %d (got %d)", MaxQuestions, len(r.Questions))
	}
	for i, q := range r.Questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("question %d: %w", i+1, err)
		}
	}
	return nil
}

// Validate checks that a Question has valid fields. Error messages
// are written for LLM consumption: specific and actionable.
func (q Question) Validate() error {
	label := q.identifier()
	if q.Text == "" {
		return fmt.Errorf("%s: question text is required", label)
	}
	if len(q.Text) > MaxQuestionLength {
		return fmt.Errorf("%s: text exceeds %d characters (got %d)", label, MaxQuestionLength, len(q.Text))
	}
	if q.Description == "" {
		return fmt.Errorf("%s: description is required", label)
	}
	if len(q.Description) > MaxDescriptionLength {
		return fmt.Errorf("%s: description exceeds %d characters (got %d)", label, MaxDescriptionLength, len(q.Description))
	}
	switch q.Type {
	case TypeYesNo, TypeFreeText:
		// No choices needed.
	case TypeSecureEntry:
		if len(q.Choices) > 0 {
			return fmt.Errorf("%s: secure_entry takes no choices", label)
		}
	case TypeSingleChoice, TypeMultiChoice:
		if len(q.Choices) < 2 {
			return fmt.Errorf("%s: %s requires at least 2 choices in the \"choices\" array (got %d). Use \"choices\", not \"options\"", label, q.Type, len(q.Choices))
		}
		if len(q.Choices) > MaxChoices {
			return fmt.Errorf("%s: choices exceed maximum of %d (got %d)", label, MaxChoices, len(q.Choices))
		}
		seen := make(map[string]bool, len(q.Choices))
		for i, c := range q.Choices {
			if c.ID == "" {
				return fmt.Errorf("%s: choice %d must have an \"id\" field", label, i+1)
			}
			if seen[c.ID] {
				return fmt.Errorf("%s: choice %d has duplicate id %q", label, i+1, c.ID)
			}
			seen[c.ID] = true
			if c.Label == "" {
				return fmt.Errorf("%s: choice %d (%s) must have a \"label\" field", label, i+1, c.ID)
			}
			if len(c.Label) > MaxChoiceLabelLength {
				return fmt.Errorf("%s: choice %d label exceeds %d characters (got %d)", label, i+1, MaxChoiceLabelLength, len(c.Label))
			}
			if len(c.Description) > MaxChoiceDescriptionLength {
				return fmt.Errorf("%s: choice %d description exceeds %d characters (got %d)", label, i+1, MaxChoiceDescriptionLength, len(c.Description))
			}
			if c.Image != "" {
				ext := strings.ToLower(filepath.Ext(c.Image))
				if !filepath.IsAbs(c.Image) || (ext != ".png" && ext != ".jpg" && ext != ".jpeg") {
					return fmt.Errorf("%s: choice %d (%s) image must be an absolute path to a .png or .jpg file", label, i+1, c.ID)
				}
				if _, err := os.Stat(c.Image); err != nil {
					return fmt.Errorf("%s: choice %d (%s) image can't be read: %v", label, i+1, c.ID, err)
				}
			}
		}
	default:
		return fmt.Errorf("%s: unknown type %q (must be yes_no, single_choice, multi_choice, or free_text)", label, q.Type)
	}
	return nil
}

// HasSecureEntry reports whether any question is a secure entry.
func (r Request) HasSecureEntry() bool {
	for _, q := range r.Questions {
		if q.Type == TypeSecureEntry {
			return true
		}
	}
	return false
}

// Prepare fills in missing IDs and the confirm defaults of multi-question
// batches.
func (r *Request) Prepare() {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	for i := range r.Questions {
		if r.Questions[i].ID == "" {
			r.Questions[i].ID = uuid.New().String()
		}
	}
	if len(r.Questions) >= 2 {
		if r.ConfirmTitle == "" {
			r.ConfirmTitle = "Ready to go?"
		}
		if r.ConfirmDescription == "" {
			r.ConfirmDescription = "Review your answers above and confirm."
		}
	}
}

// identifier returns a human-readable label for error messages.
// Uses the question label, text excerpt, or a fallback.
func (q Question) identifier() string {
	if q.Label != "" {
		return fmt.Sprintf("[%s]", q.Label)
	}
	if q.Text != "" {
		t := q.Text
		if len(t) > 40 {
			t = t[:40] + "…"
		}
		return fmt.Sprintf("[%s]", t)
	}
	return "[unnamed question]"
}

const (
	MaxQuestionLength          = 240
	MaxDescriptionLength       = 600
	MaxChoiceLabelLength       = 200
	MaxChoiceDescriptionLength = 200
	MaxChoices                 = 5
	MaxQuestions               = 11 // the prompter skill asks up to 10, plus overall notes
)

// Notification is published when a question batch is resolved so
// that non-answering clients can dismiss their open forms.
type Notification struct {
	BatchID   string `json:"batch_id"`
	SessionID string `json:"session_id"`
}

// Service manages the lifecycle of question requests. Several batches can
// wait at once (one per chat, as the ask handler allows); answers and
// cancels name the batch they resolve.
type Service interface {
	pubsub.Subscriber[Request]

	// SubscribeNotifications returns a channel for question
	// resolution notifications.
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification]

	// Ask publishes questions and blocks until the user answers
	// or the context is cancelled.
	Ask(ctx context.Context, req Request) ([]Answer, error)

	// Answer resolves the only waiting batch; it does nothing when
	// several wait.
	Answer(answers []Answer) bool

	// AnswerBatch resolves the batch with the given ID. An empty ID
	// acts as Answer.
	AnswerBatch(id string, answers []Answer) bool

	// AnswerRequest resolves only the matching request and session.
	AnswerRequest(id, sessionID string, answers []Answer) bool

	// Cancel cancels the only waiting batch, under the same rule as
	// Answer. Returns false if none was cancelled.
	Cancel() bool

	// CancelBatch cancels the batch with the given ID. An empty ID acts
	// as Cancel.
	CancelBatch(id string) bool

	// CancelRequest cancels only the matching request and session.
	CancelRequest(id, sessionID string) bool

	// Pending returns the oldest batch awaiting an answer, or false when
	// none waits.
	Pending() (Request, bool)

	// PendingAll returns every waiting batch, oldest first.
	PendingAll() []Request
}

// waiting is one batch Ask is blocked on.
type waiting struct {
	req       Request
	answers   chan []Answer
	cancelled chan struct{}
}

type questionService struct {
	broker             *pubsub.Broker[Request]
	notificationBroker *pubsub.Broker[Notification]
	mu                 sync.Mutex
	// pending holds the waiting batches, oldest first.
	pending []*waiting
}

// NewService creates a new question service.
func NewService() *questionService {
	return &questionService{
		broker:             pubsub.NewBroker[Request](),
		notificationBroker: pubsub.NewBroker[Notification](),
	}
}

// Subscribe returns a channel for question events.
func (s *questionService) Subscribe(ctx context.Context) <-chan pubsub.Event[Request] {
	return s.broker.Subscribe(ctx)
}

// SubscribeNotifications returns a channel for question resolution
// notifications.
func (s *questionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification] {
	return s.notificationBroker.Subscribe(ctx)
}

// Ask publishes a request and blocks until the user answers.
func (s *questionService) Ask(ctx context.Context, req Request) ([]Answer, error) {
	// Requests published here reach every client, including phones and
	// servers, so secure entries never come through.
	if req.HasSecureEntry() {
		return nil, ErrSecureEntryLocal
	}
	req.Prepare()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	w := &waiting{req: req, answers: make(chan []Answer, 1), cancelled: make(chan struct{})}
	s.mu.Lock()
	s.pending = append(s.pending, w)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		owned := s.remove(w)
		s.mu.Unlock()
		// A wait that ends without an answer (its run finished or was
		// cancelled) leaves clients showing a form nothing can answer:
		// tell them to close it.
		if owned {
			s.notificationBroker.Publish(pubsub.CreatedEvent, Notification{
				BatchID:   req.ID,
				SessionID: req.SessionID,
			})
		}
	}()

	s.broker.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.cancelled:
		return nil, ErrCancelled
	case answers := <-w.answers:
		return answers, nil
	}
}

// remove drops w from the waiting list and reports whether it was there.
// The caller holds s.mu.
func (s *questionService) remove(w *waiting) bool {
	for i, p := range s.pending {
		if p == w {
			s.pending = append(s.pending[:i:i], s.pending[i+1:]...)
			return true
		}
	}
	return false
}

// Pending returns the oldest batch waiting for answers, if any.
func (s *questionService) Pending() (Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return Request{}, false
	}
	return s.pending[0].req, true
}

// PendingAll returns every waiting batch, oldest first.
func (s *questionService) PendingAll() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.pending))
	for i, w := range s.pending {
		out[i] = w.req
	}
	return out
}

// Answer resolves the only waiting batch. Returns false if none (or
// several) wait.
func (s *questionService) Answer(answers []Answer) bool {
	return s.resolve("", "", false, answers, false)
}

// AnswerBatch resolves the batch with the given ID (or the only one when id
// is empty). Returns false if no such batch waits (already answered or
// cancelled).
func (s *questionService) AnswerBatch(id string, answers []Answer) bool {
	return s.resolve(id, "", false, answers, false)
}

func (s *questionService) AnswerRequest(id, sessionID string, answers []Answer) bool {
	return s.resolve(id, sessionID, true, answers, false)
}

// Cancel cancels the only waiting batch. Returns false if none (or
// several) wait.
func (s *questionService) Cancel() bool {
	return s.resolve("", "", false, nil, true)
}

// CancelBatch cancels the batch with the given ID (or the only one when id
// is empty). Returns false if no such batch waits.
func (s *questionService) CancelBatch(id string) bool {
	return s.resolve(id, "", false, nil, true)
}

func (s *questionService) CancelRequest(id, sessionID string) bool {
	return s.resolve(id, sessionID, true, nil, true)
}

// find returns the batch a resolve names, or nil. The caller holds s.mu.
func (s *questionService) find(id, sessionID string, scoped bool) *waiting {
	if scoped && (id == "" || sessionID == "") {
		return nil
	}
	if id == "" {
		if len(s.pending) == 1 {
			return s.pending[0]
		}
		return nil
	}
	for _, w := range s.pending {
		if w.req.ID == id && (!scoped || w.req.SessionID == sessionID) {
			return w
		}
	}
	return nil
}

// resolve takes the named batch under the lock so only one caller wins.
func (s *questionService) resolve(id, sessionID string, scoped bool, answers []Answer, cancel bool) bool {
	s.mu.Lock()
	w := s.find(id, sessionID, scoped)
	if w == nil {
		s.mu.Unlock()
		return false
	}
	s.remove(w)
	s.mu.Unlock()

	if cancel {
		close(w.cancelled)
	} else {
		w.answers <- answers
	}

	// Publish a notification so non-answering clients can dismiss
	// their open question forms.
	s.notificationBroker.Publish(pubsub.CreatedEvent, Notification{
		BatchID:   w.req.ID,
		SessionID: w.req.SessionID,
	})
	return true
}
