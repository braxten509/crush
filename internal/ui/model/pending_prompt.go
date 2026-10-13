package model

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/google/uuid"
)

// appendPendingPrompt records a sent prompt until its saved copy arrives.
// A held prompt (sent mid-turn) stays out of the chat meanwhile.
func (m *UI) appendPendingPrompt(sessionID, content string, attachments []message.Attachment, held bool) string {
	id := uuid.NewString()
	pending := message.Message{ID: id, SessionID: sessionID, Role: message.User, CreatedAt: time.Now().Unix(),
		Parts: []message.ContentPart{message.TextContent{Text: content, SubmissionID: id}}}
	for _, attachment := range attachments {
		path := attachment.FilePath
		if path == "" {
			path = attachment.FileName
		}
		pending.Parts = append(pending.Parts, message.BinaryContent{Path: path, MIMEType: attachment.MimeType, Data: attachment.Content})
	}
	m.queue.pending = append(m.queue.pending, pending)
	if held {
		m.queue.hold(id)
		if m.status != nil {
			m.updateLayoutAndSize() // the queue list grows
		}
		return id
	}
	m.chat.BeginInput(id)
	m.lastUserMessageTime = pending.CreatedAt
	m.chat.AppendMessages(chat.ExtractMessageItems(m.com.Styles, &pending, nil, m.com.Workspace.WorkingDir())...)
	m.chat.ScrollToBottom()
	return id
}

// releaseHeldPrompts moves this chat's held prompts into the chat, for
// when an interrupt sends them now.
func (m *UI) releaseHeldPrompts() {
	released := false
	for _, pending := range m.queue.pending {
		if pending.SessionID != m.currentSessionID() || !m.queue.held[pending.ID] {
			continue
		}
		delete(m.queue.held, pending.ID)
		m.chat.BeginInput(pending.ID)
		m.lastUserMessageTime = time.Now().Unix()
		// The chat item keeps its own copy: an item pointing into
		// pendingPrompts would change ID once the saved copy removes its
		// entry, so the saved copy could not replace it.
		m.chat.AppendMessages(chat.ExtractMessageItems(m.com.Styles, &pending, nil, m.com.Workspace.WorkingDir())...)
		released = true
	}
	if released {
		m.chat.ScrollToBottom()
		if m.status != nil {
			m.updateLayoutAndSize() // the queue list shrinks
		}
	}
}

func (m *UI) confirmPendingPrompt(id string) *message.Message {
	m.chat.ConfirmInput(id)
	pending := m.queue.take(id)
	if pending != nil && slices.Contains(m.queue.items, pending.Content().Text) {
		if m.queue.confirmedCopies == nil {
			m.queue.confirmedCopies = make(map[string]message.Message)
		}
		m.queue.confirmedCopies[id] = *pending
	}
	return pending
}

func (m *UI) finishPendingPrompt(id string, restore bool) tea.Cmd {
	pending := m.queue.take(id)
	if pending == nil {
		return nil
	}
	m.chat.RemoveMessage(pending.ID)
	if !restore {
		return nil
	}
	draft := message.QueuedPrompt{Prompt: pending.Content().Text}
	for _, binary := range pending.BinaryContent() {
		draft.Attachments = append(draft.Attachments, message.Attachment{FilePath: binary.Path, FileName: binary.Path, MimeType: binary.MIMEType, Content: binary.Data})
	}
	m.queue.recallDraft(pending.SessionID, draft)
	if pending.SessionID == m.currentSessionID() {
		return m.restoreRecalledDrafts()
	}
	return nil
}

type pendingPromptResolvedMsg struct {
	submissionID     string
	sessionID        string
	saved            *message.Message
	restoreIfMissing bool
	err              error
}

func (m *UI) resolvePendingPrompt(id string, restoreIfMissing bool) tea.Cmd {
	i := slices.IndexFunc(m.queue.pending, func(p message.Message) bool { return p.ID == id })
	if i < 0 {
		return nil
	}
	ws, sessionID := m.com.Workspace, m.queue.pending[i].SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := ws.ListMessages(ctx, sessionID)
		result := pendingPromptResolvedMsg{submissionID: id, sessionID: sessionID, restoreIfMissing: restoreIfMissing, err: err}
		for i := range msgs {
			if msgs[i].Role == message.User && msgs[i].Content().SubmissionID == id {
				result.saved = &msgs[i]
				break
			}
		}
		return result
	}
}

func (m *UI) applyResolvedPrompt(msg pendingPromptResolvedMsg) tea.Cmd {
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	if msg.saved != nil {
		if msg.sessionID == m.currentSessionID() {
			return m.appendSessionMessage(*msg.saved)
		}
		m.queue.take(msg.submissionID)
		return nil
	}
	// Only an authoritative failed completion proves no future save can
	// arrive. A failed transport acknowledgement leaves the send uncertain.
	if msg.restoreIfMissing {
		return m.finishPendingPrompt(msg.submissionID, true)
	}
	return nil
}
