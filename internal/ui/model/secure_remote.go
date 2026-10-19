package model

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/workspace"
)

// engineSecure is a secure form held by the shared engine (engine-design.md
// 5.8, 5.13). The values typed here go to the engine in one submit over the
// local socket and are cleared; the engine writes them to their files.
type engineSecure struct {
	ws   *workspace.ClientWorkspace
	form proto.SecureForm

	mu     sync.Mutex
	closed bool
	saved  map[string]bool
	open   map[string]bool
}

func newEngineSecure(ws *workspace.ClientWorkspace, form proto.SecureForm) *engineSecure {
	s := &engineSecure{ws: ws, form: form, saved: map[string]bool{}, open: map[string]bool{}}
	for _, f := range form.Fields {
		switch f.Status {
		case "saved":
			s.saved[f.QuestionID] = true
		case "open":
			s.open[f.QuestionID] = true
		}
	}
	return s
}

func (s *engineSecure) Questions() question.Request {
	return proto.QuestionRequestToDomain(s.form.Request)
}

func (s *engineSecure) Slot(questionID string) questionform.SecureSlot {
	for _, f := range s.form.Fields {
		if f.QuestionID == questionID {
			return engineSlot{s: s, id: questionID}
		}
	}
	return nil
}

func (s *engineSecure) Submit(answers []question.Answer, values map[string][]byte) error {
	submit := proto.SecureSubmit{
		Answers: []proto.QuestionAnswer{proto.QuestionAnswerFromDomain(answers)},
		Values:  make(map[string]string, len(values)),
	}
	first := ""
	for id, value := range values {
		submit.Values[id] = string(value)
		if first == "" {
			first = id
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.ws.SubmitSecureForm(ctx, s.form.FormID, submit)
	clear(submit.Values)
	switch {
	case err == nil:
		s.finish(true)
		return nil
	case strings.Contains(err.Error(), "secure entry not found"):
		s.finish(false)
		return secureentry.ErrClosed
	}
	return &secureentry.FieldError{QuestionID: first, Err: err}
}

func (s *engineSecure) Cancel() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.ws.CancelSecureForm(ctx, s.form.FormID)
	}()
}

func (s *engineSecure) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// finish marks the form closed; saved marks every open field saved.
func (s *engineSecure) finish(saved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if saved {
		for id := range s.open {
			s.saved[id] = true
		}
	}
}

type engineSlot struct {
	s  *engineSecure
	id string
}

func (e engineSlot) Saved() bool {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	return e.s.saved[e.id]
}

func (e engineSlot) Available() bool {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	return !e.s.closed && e.s.open[e.id] && !e.s.saved[e.id]
}

// handleEngineSecureMsg shows and closes the engine's secure forms in a
// window that shares the engine. ok reports that msg goes no further.
func (m *UI) handleEngineSecureMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch typed := msg.(type) {
	case workspace.SecureFormMsg:
		ws, shared := m.com.Workspace.(*workspace.ClientWorkspace)
		if !shared {
			return nil, true
		}
		if form, ok := m.activeInline.(*questionform.QuestionForm); ok && form.OwnsSecureForm(typed.Form.FormID) {
			return nil, true // already shown
		}
		var cmd tea.Cmd
		if m.cliUpdatePromptOpen() {
			cmd = m.movePromptToStatus()
			m.activeInline = nil
		}
		m.dropQuestionForm()
		m.openInline(questionform.NewSecureBackendForm(m.com.Styles, newEngineSecure(ws, typed.Form), typed.Form.FormID))
		m.updateLayoutAndSize()
		m.chat.ScrollToBottom()
		return tea.Batch(cmd, m.playNotificationSound(notification.SoundQuestion)), true
	case workspace.SecureFormClosedMsg:
		if form, ok := m.activeInline.(*questionform.QuestionForm); ok && form.OwnsSecureForm(typed.FormID) {
			form.Close()
			m.activeInline = nil
			m.textarea.Focus()
			m.updateLayoutAndSize()
		}
		return nil, true
	}
	return nil, false
}
