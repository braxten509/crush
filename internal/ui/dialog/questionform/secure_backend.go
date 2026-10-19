package questionform

import (
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
)

// SecureSlot is one prepared secure field.
type SecureSlot interface {
	Saved() bool
	Available() bool
}

// SecureBackend saves a form's secure fields: the local secure-entry form
// when this window runs its chats itself, or the engine's form when it
// shares the engine (engine-design.md 5.8, 5.13). Values only pass through
// Submit and are never kept.
type SecureBackend interface {
	Questions() question.Request
	// Slot returns the prepared field for a secure question, or nil.
	Slot(questionID string) SecureSlot
	Submit(answers []question.Answer, values map[string][]byte) error
	Cancel()
	Closed() bool
}

// localSecure is the in-process secure-entry form.
type localSecure struct{ form *secureentry.Form }

func (l localSecure) Questions() question.Request { return l.form.Request }

func (l localSecure) Slot(questionID string) SecureSlot {
	if field := l.form.Field(questionID); field != nil {
		return field
	}
	return nil
}

func (l localSecure) Submit(answers []question.Answer, values map[string][]byte) error {
	return l.form.Submit(answers, values)
}
func (l localSecure) Cancel()      { l.form.Cancel() }
func (l localSecure) Closed() bool { return l.form.Closed() }
