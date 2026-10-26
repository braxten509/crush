package model

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/prompthistory"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// promptHistoryLoadedMsg is sent when prompt history is loaded.
type promptHistoryLoadedMsg struct {
	messages []string
}

// loadPromptHistory loads the prompts sent from every folder and app
// (prompthistory), newest first, for history navigation.
func (m *UI) loadPromptHistory() tea.Cmd {
	return func() tea.Msg {
		return promptHistoryLoadedMsg{messages: prompthistory.List()}
	}
}

// savePrompt saves a sent prompt to the shared history, then loads it.
func (m *UI) savePrompt(text string) tea.Cmd {
	return func() tea.Msg {
		prompthistory.Add(text)
		return promptHistoryLoadedMsg{messages: prompthistory.List()}
	}
}

// handleHistoryUp handles up arrow for history navigation.
func (m *UI) handleHistoryUp(msg tea.Msg) tea.Cmd {
	prevHeight := m.textarea.Height()
	// Navigate to an older history entry from cursor position (0,0), or
	// from anywhere while a recalled prompt is shown unchanged.
	if m.textarea.Length() == 0 || m.isAtEditorStart() || m.promptHistory.index >= 0 {
		if !m.bangMode && len(m.queue.recalledDrafts[m.currentSessionID()]) > 0 {
			return m.restoreRecalledDrafts()
		}
		if !m.bangMode && m.hasSession() && m.agentReady && (m.isAgentBusy() || m.queue.count > 0 || m.queue.failedRecalls[m.currentSessionID()]) {
			return m.recallQueuedPrompt()
		}
		return m.historyUpFromStart(prevHeight)
	}

	// First move cursor to start before entering history.
	if m.textarea.Line() == 0 {
		m.textarea.CursorStart()
		return nil
	}

	// Let textarea handle normal cursor movement.
	return m.updateTextarea(msg)
}

func (m *UI) historyUpFromStart(prevHeight int) tea.Cmd {
	if m.historyPrev() {
		// we send this so that the textarea moves the view to the correct position
		// without this the cursor will show up in the wrong place.
		return m.updateTextareaWithPrevHeight(nil, prevHeight)
	}
	return nil
}

type queuedPromptRecalledMsg struct {
	sessionID    string
	draft        string
	historyIndex int
	prompt       *message.QueuedPrompt
	err          error
}

func (m *UI) recallQueuedPrompt() tea.Cmd {
	if m.queue.recallInFlight {
		return nil
	}
	m.queue.recallInFlight = true
	ws, sessionID, draft := m.com.Workspace, m.currentSessionID(), m.textarea.Value()
	historyIndex := m.promptHistory.index
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prompt, err := ws.AgentRecallQueuedPrompt(ctx, sessionID)
		return queuedPromptRecalledMsg{sessionID: sessionID, draft: draft, historyIndex: historyIndex, prompt: prompt, err: err}
	}
}

func (m *UI) applyRecalledPrompt(msg queuedPromptRecalledMsg) tea.Cmd {
	m.queue.recallInFlight = false
	if msg.err != nil {
		if m.queue.failedRecalls == nil {
			m.queue.failedRecalls = make(map[string]bool)
		}
		m.queue.failedRecalls[msg.sessionID] = true
		return util.ReportError(msg.err)
	}
	delete(m.queue.failedRecalls, msg.sessionID)
	if msg.prompt != nil {
		m.finishPendingPrompt(msg.prompt.SubmissionID, false)
		m.queue.recallDraft(msg.sessionID, *msg.prompt)
	}
	if msg.sessionID != m.currentSessionID() {
		return nil
	}
	m.queue.invalidate()
	m.queue.checkedAt = time.Time{}
	m.invalidateBusyCaches()
	cmds := []tea.Cmd{m.dispatchPromptQueueRefresh(), m.dispatchBusyRefresh()}
	if msg.prompt != nil {
		cmds = append(cmds, m.restoreRecalledDrafts())
		if m.bangMode {
			cmds = append(cmds, util.ReportInfo("Queued message recovered. Press Up outside shell mode to edit it."))
		}
	} else if m.textarea.Value() == msg.draft && m.promptHistory.index == msg.historyIndex && (m.textarea.Length() == 0 || m.isAtEditorStart()) {
		// The CLI may have taken it before Up arrived. Only navigate history
		// if the user hasn't started editing while the request was in flight.
		cmds = append(cmds, m.historyUpFromStart(m.textarea.Height()))
	}
	return tea.Batch(cmds...)
}

func (m *UI) restoreRecalledDrafts() tea.Cmd {
	drafts := m.queue.recalledDrafts[m.currentSessionID()]
	if len(drafts) == 0 || m.bangMode {
		return nil
	}
	delete(m.queue.recalledDrafts, m.currentSessionID())
	prevHeight := m.textarea.Height()
	text := m.textarea.Value()
	for _, draft := range drafts {
		if text != "" && draft.Prompt != "" {
			text += "\n\n"
		}
		text += draft.Prompt
		for _, attachment := range draft.Attachments {
			m.attachments.Update(attachment)
		}
	}
	m.textarea.SetValue(text)
	// Recovered messages are agent prompts, even if they begin with '!'.
	m.setEditorPrompt(m.yoloModeCached())
	m.textarea.MoveToEnd()
	m.promptHistory.index = -1
	m.promptHistory.draft = text
	m.mention.close()
	m.updateLayoutAndSize()
	return m.updateTextareaWithPrevHeight(nil, prevHeight)
}

// handleHistoryDown handles down arrow for history navigation.
func (m *UI) handleHistoryDown(msg tea.Msg) tea.Cmd {
	prevHeight := m.textarea.Height()
	// Navigate to newer history entry from end of text.
	if m.isAtEditorEnd() {
		if m.historyNext() {
			// we send this so that the textarea moves the view to the correct position
			// without this the cursor will show up in the wrong place.
			return m.updateTextareaWithPrevHeight(nil, prevHeight)
		}
	}

	// First move cursor to end before navigating history.
	if m.textarea.Line() == max(m.textarea.LineCount()-1, 0) {
		m.textarea.MoveToEnd()
		return m.updateTextarea(nil)
	}

	// Let textarea handle normal cursor movement.
	return m.updateTextarea(msg)
}

// handleHistoryEscape handles escape for exiting history navigation.
func (m *UI) handleHistoryEscape(msg tea.Msg) tea.Cmd {
	prevHeight := m.textarea.Height()
	// Return to current draft when browsing history.
	if m.promptHistory.index >= 0 {
		m.promptHistory.index = -1
		m.textarea.Reset()
		m.textarea.InsertString(m.promptHistory.draft)
		m.syncBangModeFromTextarea()
		return m.updateTextareaWithPrevHeight(nil, prevHeight)
	}

	// Let textarea handle escape normally.
	return m.updateTextarea(msg)
}

// syncBangModeFromTextarea engages or disengages bang mode based on
// whether the current textarea value starts with "!". The "!" prefix
// is stripped when entering bang mode and re-added when leaving it so
// the visible text always reflects the correct state.
func (m *UI) syncBangModeFromTextarea() {
	val := m.textarea.Value()
	hasBang := strings.HasPrefix(val, "!")
	if hasBang {
		if !m.bangMode {
			m.bangMode = true
			m.bangWasEmpty = false
		}
		m.textarea.SetValue(strings.TrimPrefix(val, "!"))
		m.textarea.MoveToBegin()
	} else if m.bangMode {
		m.bangMode = false
		m.bangWasEmpty = false
	}
	m.setEditorPrompt(m.yoloModeCached())
}

// historyPrev changes the text area content to the previous message in the history
// it returns false if it could not find the previous message.
func (m *UI) historyPrev() bool {
	if len(m.promptHistory.messages) == 0 {
		return false
	}
	if m.promptHistory.index == -1 {
		m.promptHistory.draft = m.textarea.Value()
	}
	nextIndex := m.promptHistory.index + 1
	if nextIndex >= len(m.promptHistory.messages) {
		return false
	}
	m.promptHistory.index = nextIndex
	m.textarea.Reset()
	m.textarea.InsertString(m.promptHistory.messages[nextIndex])
	m.syncBangModeFromTextarea()
	m.textarea.MoveToEnd()
	return true
}

// historyNext changes the text area content to the next message in the history
// it returns false if it could not find the next message.
func (m *UI) historyNext() bool {
	if m.promptHistory.index < 0 {
		return false
	}
	nextIndex := m.promptHistory.index - 1
	if nextIndex < 0 {
		m.promptHistory.index = -1
		m.textarea.Reset()
		m.textarea.InsertString(m.promptHistory.draft)
		m.syncBangModeFromTextarea()
		return true
	}
	m.promptHistory.index = nextIndex
	m.textarea.Reset()
	m.textarea.InsertString(m.promptHistory.messages[nextIndex])
	m.syncBangModeFromTextarea()
	return true
}
