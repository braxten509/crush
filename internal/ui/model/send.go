package model

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
)

// isAgentBusy returns true if the agent coordinator exists and is currently
// busy processing a request. It only reads the memoized state (it runs in
// per-message paths like the textarea placeholder, where a workspace probe
// would be an HTTP round-trip per keystroke in client/server mode); the
// value is refreshed off-thread, see workspace_cache.go.
func (m *UI) isAgentBusy() bool {
	if m.bangCancel != nil {
		return true
	}
	for _, submission := range m.submittingPrompts {
		if submission.sessionID == m.currentSessionID() {
			return true
		}
	}
	return m.agentBusyCache.val
}

// ensureSession makes sure a session is active, creating one if none is. It
// returns a command that loads the freshly created session (nil when a session
// already existed) and an error if creation failed. It mutates UI state, so
// callers must run on the Update goroutine.
func (m *UI) ensureSession() (tea.Cmd, error) {
	if m.hasSession() {
		return nil, nil
	}
	newSession, err := m.com.Workspace.CreateSession(context.Background(), "New Session")
	if err != nil {
		return nil, err
	}
	if m.forceCompactMode {
		m.isCompact = true
	}
	var cmd tea.Cmd
	if newSession.ID != "" {
		m.session = &newSession
		cmd = m.loadSession(newSession.ID)
	}
	m.setState(uiChat, m.focus)
	return cmd, nil
}

// sendMessage sends a message with the given content and attachments.
func (m *UI) sendMessage(content string, attachments ...message.Attachment) tea.Cmd {
	return m.sendMessageInternal(content, false, attachments...)
}

// sendMessageInternal can hide a generated continuation from the chat.
func (m *UI) sendMessageInternal(content string, hidden bool, attachments ...message.Attachment) tea.Cmd {
	if !m.agentReady {
		return util.ReportError(workspace.ErrAgentNotInitialized)
	}
	// While another session opens, the prompt is meant for that one: hold
	// it until the switch lands instead of sending it to the old session.
	if m.deferSendDuringSwitch(deferredSend{content: content, hidden: hidden, attachments: attachments}) {
		return nil
	}

	// Any new prompt supersedes a pending, unconfirmed plan.
	m.setPlanReadyPending("")

	var cmds []tea.Cmd
	priorSessionID := m.currentSessionID()
	var pendingKept []session.KeptSkill
	if !m.hasSession() {
		pendingKept = m.pendingKeptSkills
	}
	loadCmd, err := m.ensureSession()
	if err != nil {
		return util.ReportError(err)
	}
	m.pendingKeptSkills = nil
	if loadCmd != nil {
		cmds = append(cmds, loadCmd)
	}

	ctx := context.Background()
	ws, sessionID := m.com.Workspace, m.session.ID
	fileReads := slices.Clone(m.sessionFileReads)
	cmds = append(cmds, func() tea.Msg {
		for _, path := range fileReads {
			ws.FileTrackerRecordRead(ctx, sessionID, path)
			ws.LSPStart(ctx, path)
		}
		return nil
	})

	// Capture session ID to avoid race with main goroutine updating m.session.
	queued := priorSessionID != "" && priorSessionID == sessionID && (m.isAgentBusy() || m.interruptInFlight[sessionID])
	submissionID := ""
	if !hidden {
		submissionID = m.appendPendingPrompt(sessionID, content, attachments, queued)
	}
	runCtx, cancelSubmission := context.WithCancel(context.Background())
	runCtx = message.WithSubmissionID(runCtx, submissionID)
	if hidden {
		runCtx = message.WithHiddenUserMessage(runCtx)
	}
	dispatchID := m.trackPromptSubmission(submissionID, sessionID, cancelSubmission, queued)
	// Optimistically mark the agent busy: the prompt we are about to submit
	// either starts a run or is enqueued behind one. This keeps esc pressed
	// right after enter routing to cancelAgent instead of reading a stale
	// idle value; the authoritative state arrives via agentRunSubmittedMsg.
	// Bump the busy/queue generations so any probe started before this
	// optimistic write is discarded rather than reverting us to idle.
	m.agentBusyCache.set(true)
	m.chat.SetAgentBusy(true)
	m.chat.SetAnimationsAllowed(true)
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}
	m.busyFetchGen++
	m.invalidatePromptQueue()
	cmds = append(cmds, func() tea.Msg {
		defer cancelSubmission()
		// AgentRun is fire-and-forget: it returns once the prompt has
		// been accepted (HTTP 202) or synchronously with a validation
		// or transport error. Run failures and cancellation surface
		// through SSE-derived events, not this return value.
		if err := runCtx.Err(); err != nil {
			return agentRunSubmittedMsg{submissionID: submissionID, dispatchID: dispatchID, err: err}
		}
		// Save skills turned on before the chat existed first, so the
		// first turn already follows them.
		for _, skill := range pendingKept {
			if _, err := ws.SetKeptSkill(runCtx, sessionID, skill, true); err != nil {
				slog.Warn("Failed to keep skill on for new chat", "skill", skill.Name, "error", err)
			}
		}
		err := ws.AgentRun(runCtx, sessionID, content, attachments...)
		return agentRunSubmittedMsg{submissionID: submissionID, dispatchID: dispatchID, err: err}
	})
	return tea.Batch(cmds...)
}

// runShellCommand executes a shell command server-side without triggering
// the LLM. The result is displayed as a tool-style item in the chat.
func (m *UI) runShellCommand(command string) tea.Cmd {
	return m.runShellCommandInternal(command, false)
}

// runShellCommandInternal is the shared implementation for bang-mode shell
// execution. isFirstMessage indicates the command is the first user message
// in a newly created session, which triggers title generation.
func (m *UI) runShellCommandInternal(command string, isFirstMessage bool) tea.Cmd {
	if m.deferSendDuringSwitch(deferredSend{content: command, shell: true, first: isFirstMessage}) {
		return nil
	}
	var cmds []tea.Cmd
	if !m.hasSession() {
		newSession, err := m.com.Workspace.CreateSession(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}
		if m.forceCompactMode {
			m.isCompact = true
		}
		if newSession.ID != "" {
			m.session = &newSession
			cmds = append(cmds, m.loadSession(newSession.ID))
		}
		m.setState(uiChat, m.focus)
		// Defer shell execution until loadSessionMsg fires so the chat
		// list is stable before we add items or start streaming.
		m.pendingBangCommand = command
		return tea.Batch(cmds...)
	}

	sessionID := m.session.ID
	contentWidth := min(m.layout.main.Dx()-2, 120)

	// Append a pending shell item immediately so the user sees feedback.
	pendingItem := chat.NewPendingShellItem(m.com.Styles, command)
	// Bang mode runs without the agent, so re-enable the animation clock
	// that a non-busy session reload may have frozen.
	m.chat.SetAnimationsAllowed(true)
	m.chat.AppendMessages(pendingItem)
	m.chat.ScrollToBottom()

	// Stream output via channel. The progress callback writes chunks
	// to streamCh; a reader cmd converts them to shellStreamMsg values.
	streamCh := make(chan string, 64)
	pendingID := pendingItem.ID()

	onProgress := func(chunk string) {
		select {
		case streamCh <- chunk:
		default:
			// Drop if UI can't keep up.
		}
	}

	// Reader cmd: drains streamCh into shellStreamMsg until closed.
	cmds = append(cmds, func() tea.Msg {
		chunk, ok := <-streamCh
		if !ok {
			return nil
		}
		return shellStreamMsg{PendingID: pendingID, Chunk: chunk, streamCh: streamCh}
	})

	ctx, cancel := context.WithCancel(context.Background())
	m.bangCancel = cancel

	cmds = append(cmds, func() tea.Msg {
		resp, err := m.com.Workspace.AgentRunShellCommand(ctx, sessionID, command, contentWidth, onProgress, isFirstMessage)
		close(streamCh)
		if err != nil && !errors.Is(err, context.Canceled) {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("shell: %v", err),
			}
		}
		exitCode := resp.ExitCode
		if errors.Is(err, context.Canceled) {
			exitCode = 130 // conventional SIGINT exit code
		}
		return shellResultMsg{
			PendingID: pendingID,
			Command:   command,
			Output:    resp.Output,
			ExitCode:  exitCode,
		}
	})
	return tea.Batch(cmds...)
}

// interruptAgent stops the active run without the double-press confirm
// and without clearing follow-ups, so the queued batch runs.
func (m *UI) interruptAgent() tea.Cmd {
	if !m.hasSession() || !m.agentReady {
		return nil
	}
	if m.interruptInFlight[m.currentSessionID()] {
		return nil
	}
	if m.interruptInFlight == nil {
		m.interruptInFlight = make(map[string]bool)
	}
	m.interruptInFlight[m.currentSessionID()] = true
	cancelled := make(map[string]bool)
	var cancelSubmissions []context.CancelFunc
	for id, submission := range m.submittingPrompts {
		if submission.sessionID == m.currentSessionID() && !submission.queued {
			cancelSubmissions = append(cancelSubmissions, submission.cancel)
			delete(m.submittingPrompts, id)
			cancelled[id] = true
		}
	}
	continuing := m.promptQueue > 0
	for _, pending := range m.pendingPrompts {
		if pending.SessionID == m.currentSessionID() && !cancelled[pending.ID] {
			continuing = true
		}
	}
	if m.bangCancel != nil {
		m.bangCancel()
		m.bangCancel = nil
	}
	if m.canceledMessages == nil {
		m.canceledMessages = make(map[string]struct{})
	}
	for _, id := range m.chat.CancelRunning(continuing) {
		m.canceledMessages[id] = struct{}{}
	}
	if continuing {
		m.releaseHeldPrompts()
	}
	m.todoIsSpinning = false
	m.agentBusyCache.set(continuing)
	m.invalidateBusyCaches()
	m.chat.SetAgentBusy(continuing)
	if continuing {
		m.chat.SetAnimationsAllowed(true)
	}
	m.renderPills()
	ws, sessionID := m.com.Workspace, m.session.ID
	return func() tea.Msg {
		ws.AgentInterrupt(sessionID)
		// Claim/cancel the active run first, so its natural drain cannot
		// start the next batch and have that batch interrupted afterward.
		for _, cancel := range cancelSubmissions {
			cancel()
		}
		return agentInterruptedMsg{sessionID: sessionID}
	}
}

// editorWantsEsc reports whether esc has another job in the focused editor.
func (m *UI) editorWantsEsc() bool {
	return m.focus == uiFocusEditor && (m.mention.open || m.attachments.Deleting() || m.promptHistory.index >= 0)
}

// cancelAgent handles the cancel key press: with prompts queued it stops
// the active run and sends them, otherwise it cancels the active run.
func (m *UI) cancelAgent() tea.Cmd {
	// Let the agent claim its current queue atomically: the UI's cached
	// count may still be zero just after Enter. Interrupt also cancels
	// normally when there are no queued prompts.
	return m.interruptAgent()
}

// clearPromptQueue drops the prompts queued behind the running turn.
func (m *UI) clearPromptQueue() {
	m.com.Workspace.AgentClearQueue(m.session.ID)
	m.promptQueue = 0
	m.promptQueueItems = nil
	m.promptQueueCheckedAt = time.Now()
	// Bump the queue generation so a fetch started before this clear
	// cannot land and repopulate the pill we just emptied.
	m.invalidatePromptQueue()
	m.updateLayoutAndSize()
}

// summarizeSession compacts the session into a summary.
func (m *UI) summarizeSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := m.com.Workspace.AgentSummarize(context.Background(), sessionID)
		if err != nil {
			return util.ReportError(err)()
		}
		return nil
	}
}
