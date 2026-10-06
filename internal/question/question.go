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
	BatchID string `json:"batch_id"`
}

// Service manages the lifecycle of question requests. Only one
// question can be pending at a time.
type Service interface {
	pubsub.Subscriber[Request]

	// SubscribeNotifications returns a channel for question
	// resolution notifications.
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification]

	// Ask publishes questions and blocks until the user answers
	// or the context is cancelled.
	Ask(ctx context.Context, req Request) ([]Answer, error)

	// Answer resolves the pending question with the given answers.
	Answer(answers []Answer) bool

	// AnswerRequest resolves only the matching request and session.
	AnswerRequest(id, sessionID string, answers []Answer) bool

	// Cancel cancels the pending question. Returns false if no
	// question is pending.
	Cancel() bool

	// CancelRequest cancels only the matching request and session.
	CancelRequest(id, sessionID string) bool
}

type questionService struct {
	broker             *pubsub.Broker[Request]
	notificationBroker *pubsub.Broker[Notification]
	mu                 sync.Mutex
	pending            chan []Answer
	cancelled          chan struct{}
	pendingID          string
	pendingReq         Request
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

	pending := make(chan []Answer, 1)
	cancelled := make(chan struct{})
	s.mu.Lock()
	s.pending = pending
	s.cancelled = cancelled
	s.pendingID = req.ID
	s.pendingReq = req
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.pending == pending {
			s.pending = nil
			s.cancelled = nil
			s.pendingID = ""
			s.pendingReq = Request{}
		}
		s.mu.Unlock()
	}()

	s.broker.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-cancelled:
		return nil, ErrCancelled
	case answers := <-pending:
		return answers, nil
	}
}

// Pending returns the questions that are waiting for answers, if any.
func (s *questionService) Pending() (Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return Request{}, false
	}
	return s.pendingReq, true
}

// Answer resolves the pending question. Returns false if no
// question is pending (already answered or cancelled).
func (s *questionService) Answer(answers []Answer) bool {
	return s.resolve("", "", false, answers, false)
}

func (s *questionService) AnswerRequest(id, sessionID string, answers []Answer) bool {
	return s.resolve(id, sessionID, true, answers, false)
}

// Cancel cancels the pending question. Returns false if no
// question is pending.
func (s *questionService) Cancel() bool {
	return s.resolve("", "", false, nil, true)
}

func (s *questionService) CancelRequest(id, sessionID string) bool {
	return s.resolve(id, sessionID, true, nil, true)
}

// resolve takes the pending request under the lock so only one caller wins.
func (s *questionService) resolve(id, sessionID string, scoped bool, answers []Answer, cancel bool) bool {
	s.mu.Lock()
	if s.pending == nil || (scoped && (id == "" || sessionID == "" || s.pendingID != id || s.pendingReq.SessionID != sessionID)) {
		s.mu.Unlock()
		return false
	}
	batchID := s.pendingID
	ch := s.pending
	cancelCh := s.cancelled
	s.pending = nil
	s.cancelled = nil
	s.pendingID = ""
	s.pendingReq = Request{}
	s.mu.Unlock()

	if cancel {
		close(cancelCh)
	} else {
		ch <- answers
	}

	// Publish a notification so non-answering clients can dismiss
	// their open question forms.
	if batchID != "" {
		s.notificationBroker.Publish(pubsub.CreatedEvent, Notification{
			BatchID: batchID,
		})
	}
	return true
}
