package model

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// nestedFetchConcurrency bounds the parallel fetches of agent tool
// transcripts while a session is prepared.
const nestedFetchConcurrency = 8

// sessionLoadState tracks the session load in flight. Only the latest load
// (seq) may apply: switching twice, starting a new chat or a failure makes
// any earlier completion stale.
type sessionLoadState struct {
	seq     uint64
	active  bool
	id      string
	title   string
	started time.Time
	frame   int
	cancel  context.CancelFunc
	// canceled is the interrupted-message set the transcript was prepared
	// with; anything interrupted later is applied once the load lands.
	canceled map[string]struct{}
	// events are message events for the loading session (and agent child
	// sessions) that arrived meanwhile, replayed on top of the snapshot so
	// nothing streamed during the load is lost. session is the latest
	// session update seen for it.
	events  []pubsub.Event[message.Message]
	session *session.Session
	// deferred holds prompts sent while switching; they go to the session
	// being opened, not the one still on screen.
	deferred []deferredSend
}

type deferredSend struct {
	content     string
	attachments []message.Attachment
	hidden      bool
	shell       bool
	first       bool
}

// sessionLoadFailedMsg ends the load with seq when it could not be read.
type sessionLoadFailedMsg struct {
	seq uint64
	err error
}

// sessionLoadTickMsg animates the loading indicator of load seq.
type sessionLoadTickMsg struct{ seq uint64 }

// openSessionMsg asks the UI thread to load a session found off-thread.
type openSessionMsg struct{ id string }

func sessionLoadTick(seq uint64) tea.Cmd {
	return tea.Tick(spinner.MiniDot.FPS, func(time.Time) tea.Msg {
		return sessionLoadTickMsg{seq: seq}
	})
}

// openSession loads s and names it in the loading indicator.
func (m *UI) openSession(s session.Session) tea.Cmd {
	if s.Directory != "" && (s.Directory != m.com.Workspace.WorkingDir() || s.DataDirectory != m.com.Config().Options.DataDirectory) {
		if m.isAgentBusy() {
			return util.ReportWarn("Agent is busy, please wait before switching folders...")
		}
		if _, remote := m.com.Workspace.(*workspace.ClientWorkspace); !remote {
			if info, err := os.Stat(s.Directory); err != nil || !info.IsDir() {
				return util.ReportWarn("That chat's folder is no longer available: " + s.Directory)
			}
		}
		m.relaunchDir, m.relaunchSessionID, m.relaunchDataDir = s.Directory, s.ID, s.DataDirectory
		return tea.Quit
	}

	cmd := m.loadSession(s.ID)
	m.sessionLoad.title = s.Title
	return cmd
}

// beginSessionLoad supersedes any load in flight and starts tracking one for
// sessionID. It reports whether the load switches away from the session on
// screen, which is when the loading indicator shows.
func (m *UI) beginSessionLoad(sessionID string) (context.Context, uint64, bool) {
	prev := m.sessionLoad
	if prev.cancel != nil {
		prev.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := sessionLoadState{
		seq:      prev.seq + 1,
		active:   true,
		id:       sessionID,
		started:  time.Now(),
		cancel:   cancel,
		canceled: maps.Clone(m.canceledMessages),
		deferred: prev.deferred,
	}
	if prev.active && prev.id == sessionID {
		next.title, next.started, next.frame = prev.title, prev.started, prev.frame
		next.events, next.session = prev.events, prev.session
	}
	m.sessionLoad = next
	return ctx, next.seq, m.sessionSwitchPending()
}

// sessionSwitchPending reports whether a different session than the one on
// screen is being opened.
func (m *UI) sessionSwitchPending() bool {
	return m.sessionLoad.active && m.sessionLoad.id != m.currentSessionID()
}

// endSessionLoad stops tracking the current load and returns its state.
func (m *UI) endSessionLoad() sessionLoadState {
	load := m.sessionLoad
	if load.cancel != nil {
		load.cancel()
	}
	m.sessionLoad = sessionLoadState{seq: load.seq}
	return load
}

// abandonSessionLoad drops the load in flight (e.g. a new chat was started)
// so its late completion is ignored, and gives deferred prompts back.
func (m *UI) abandonSessionLoad() tea.Cmd {
	if !m.sessionLoad.active {
		return nil
	}
	load := m.endSessionLoad()
	m.sessionLoad.seq++
	return m.restoreDeferredSends(load.deferred)
}

// deferSendDuringSwitch holds a send made while another session opens.
func (m *UI) deferSendDuringSwitch(send deferredSend) bool {
	if !m.sessionSwitchPending() {
		return false
	}
	m.sessionLoad.deferred = append(m.sessionLoad.deferred, send)
	return true
}

func (m *UI) runDeferredSends(sends []deferredSend) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(sends))
	for _, s := range sends {
		if s.shell {
			cmds = append(cmds, m.runShellCommandInternal(s.content, s.first))
		} else {
			cmds = append(cmds, m.sendMessageInternal(s.content, s.hidden, s.attachments...))
		}
	}
	return cmds
}

// restoreDeferredSends puts prompts typed during a switch that did not
// happen back into the editor instead of sending them to the old session.
func (m *UI) restoreDeferredSends(sends []deferredSend) tea.Cmd {
	var cmds []tea.Cmd
	id := m.currentSessionID()
	for _, s := range sends {
		switch {
		case s.shell:
			cmds = append(cmds, util.ReportWarn("Shell command not run: the conversation did not open"))
		case !s.hidden:
			if m.recalledDrafts == nil {
				m.recalledDrafts = make(map[string][]message.QueuedPrompt)
			}
			m.recalledDrafts[id] = append(m.recalledDrafts[id], message.QueuedPrompt{Prompt: s.content, Attachments: s.attachments})
		}
	}
	if len(m.recalledDrafts[id]) > 0 {
		cmds = append(cmds, m.restoreRecalledDrafts())
	}
	return tea.Batch(cmds...)
}

// holdSessionLoadEvent buffers a message event for the session being loaded
// or an agent child session. It reports whether the event must not touch the
// chat now, which is while the chat still shows another session.
func (m *UI) holdSessionLoadEvent(ev pubsub.Event[message.Message]) bool {
	load := &m.sessionLoad
	if !load.active {
		return false
	}
	if sid := ev.Payload.SessionID; sid != load.id {
		if _, _, child := m.com.Workspace.ParseAgentToolSessionID(sid); !child {
			return false
		}
	}
	// Streaming sends one update per chunk: keep only the latest one in a
	// row for a message so a slow load does not pile them up.
	if n := len(load.events); n > 0 && ev.Type == pubsub.UpdatedEvent {
		last := load.events[n-1]
		if last.Type == pubsub.UpdatedEvent && last.Payload.ID == ev.Payload.ID {
			load.events[n-1] = ev
			return m.sessionSwitchPending()
		}
	}
	load.events = append(load.events, ev)
	return m.sessionSwitchPending()
}

// holdSessionLoadSession keeps the latest update of the session being
// loaded, which may be newer than the one the load read. Deleting a session
// that is being opened abandons the switch.
func (m *UI) holdSessionLoadSession(ev pubsub.Event[session.Session]) tea.Cmd {
	if !m.sessionLoad.active || ev.Payload.ID != m.sessionLoad.id {
		return nil
	}
	if ev.Type == pubsub.DeletedEvent {
		if m.sessionSwitchPending() {
			return m.abandonSessionLoad()
		}
		m.sessionLoad.session = nil
		return nil
	}
	s := ev.Payload
	m.sessionLoad.session = &s
	return nil
}

func (m *UI) handleSessionLoadMsg(msg tea.Msg) []tea.Cmd {
	switch msg := msg.(type) {
	case openSessionMsg:
		return []tea.Cmd{m.loadSession(msg.id)}
	case sessionLoadTickMsg:
		if !m.sessionLoad.active || msg.seq != m.sessionLoad.seq {
			return nil
		}
		m.sessionLoad.frame++
		return []tea.Cmd{sessionLoadTick(msg.seq)}
	case sessionLoadFailedMsg:
		if !m.sessionLoad.active || msg.seq != m.sessionLoad.seq {
			return nil
		}
		load := m.endSessionLoad()
		return []tea.Cmd{m.restoreDeferredSends(load.deferred), util.ReportError(msg.err)}
	case loadSessionMsg:
		return m.applyLoadedSession(msg)
	}
	return nil
}

// applyLoadedSession swaps a loaded session in. All IO and item building
// already happened off-thread; what is left is proportional to the items
// that are on screen, plus bookkeeping over the item slice.
func (m *UI) applyLoadedSession(msg loadSessionMsg) []tea.Cmd {
	var load sessionLoadState
	if msg.seq != 0 {
		if !m.sessionLoad.active || msg.seq != m.sessionLoad.seq {
			return nil
		}
		load = m.endSessionLoad()
	}
	var cmds []tea.Cmd
	sess := msg.session
	if newer := load.session; newer != nil && newer.ID == sess.ID && newer.UpdatedAt >= sess.UpdatedAt {
		sess = newer
	}
	if m.forceCompactMode {
		m.isCompact = true
	}
	// Plan mode is scoped to the session it was enabled in: switching
	// to another session falls back to code mode and drops any pending
	// plan handoff. (Loading the session that was just created for the
	// first plan-mode prompt is not a switch; the IDs match then.)
	if m.session == nil || m.session.ID != sess.ID {
		m.agentBusyCache.set(false)
		m.chat.SetAgentBusy(false)
		if cmd := m.resetPlanModeState(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	m.setState(uiChat, m.focus)
	m.session = sess
	m.sidebarOffset = 0
	m.sessionFiles = msg.files
	// Session switch: the memoized busy state and queued prompts
	// belong to the previous session. Drop them and re-fetch
	// off-thread so the queue pill and esc behavior track the new
	// session instead of a stale one.
	m.invalidateBusyCaches()
	m.invalidatePromptQueue()
	m.promptQueue = 0
	m.promptQueueItems = nil
	m.promptQueueCheckedAt = time.Time{}
	if cmd := m.dispatchBusyRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.startLSPs(msg.lspFilePaths()))
	prepared := msg.prepared
	if prepared == nil {
		prepared = m.prepareTranscriptNow(msg.messages)
	}
	if cmd := m.setPreparedMessages(msg.messages, prepared); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.applyLateCancels(msg.messages, prepared.canceled)...)
	cmds = append(cmds, m.replaySessionLoadEvents(load.events, msg.messages)...)
	if cmd := m.restoreModelFromSession(msg.messages); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.autoExpandPillsIfReasonable(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// If a bang command was issued before the session finished
	// loading, start it now that the chat list is stable.
	if m.pendingBangCommand != "" {
		cmds = append(cmds, m.runShellCommandInternal(m.pendingBangCommand, true))
		m.pendingBangCommand = ""
	}
	if hasInProgressTodo(m.session.Todos) {
		// only start spinner if there is an in-progress todo
		if m.isAgentBusy() {
			m.todoIsSpinning = true
			cmds = append(cmds, m.todoSpinner.Tick)
		}
	}
	// Reload prompt history for the new session.
	m.historyReset()
	if cmd := m.restoreRecalledDrafts(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.loadPromptHistory())
	m.updateLayoutAndSize()
	// Render the rest of the history into the cache a slice per frame, so
	// the first scroll (scrollbar size and position need every row's
	// height) does not render a long session at once.
	cmds = append(cmds, m.chat.BeginWarm())
	cmds = append(cmds, m.runDeferredSends(load.deferred)...)
	return cmds
}

// preparedTranscript is a session's chat items built off the UI thread.
type preparedTranscript struct {
	items []chat.MessageItem
	// lastUserTime is what lastUserMessageTime reads after the transcript:
	// the last user message, or the first message without one.
	lastUserTime int64
	canceled     map[string]struct{}
}

// prepareTranscript builds the chat items for msgs, including the nested
// tools of agent calls, without touching UI state. canceled is a snapshot of
// the interrupted messages, cfg the config the info rows read.
func prepareTranscript(ctx context.Context, ws workspace.Workspace, sty *styles.Styles, cfg *config.Config, workingDir string, msgs []message.Message, canceled map[string]struct{}) *preparedTranscript {
	ptrs := make([]*message.Message, len(msgs))
	for i := range msgs {
		msg := withImmediateCancel(canceled, msgs[i])
		ptrs[i] = &msg
	}
	toolResults := chat.BuildToolResultMap(ptrs)
	p := &preparedTranscript{
		items:    make([]chat.MessageItem, 0, len(msgs)*2),
		canceled: canceled,
	}
	if len(ptrs) > 0 {
		p.lastUserTime = ptrs[0].CreatedAt
	}
	for _, msg := range ptrs {
		if msg.Role == message.User {
			p.lastUserTime = msg.CreatedAt
		}
		p.items = append(p.items, chat.ExtractMessageItems(sty, msg, toolResults, workingDir)...)
		if msg.Role == message.Assistant && chat.ShouldShowAssistantInfo(msg) {
			p.items = append(p.items, chat.NewAssistantInfoItem(sty, msg, cfg, time.Unix(p.lastUserTime, 0)))
		}
	}
	loadNestedTools(ctx, ws, sty, workingDir, p.items)
	return p
}

// prepareTranscriptNow prepares msgs on the UI thread, for callers that
// have no prepared transcript (tests, synchronous reloads).
func (m *UI) prepareTranscriptNow(msgs []message.Message) *preparedTranscript {
	return prepareTranscript(context.Background(), m.com.Workspace, m.com.Styles, m.com.Config(),
		m.com.Workspace.WorkingDir(), msgs, maps.Clone(m.canceledMessages))
}

// loadNestedTools fills agent tool calls with the tools of their child
// sessions, one level at a time. Each child transcript is a workspace read
// (an HTTP round trip in client/server mode), so a level is fetched in
// parallel.
func loadNestedTools(ctx context.Context, ws workspace.Workspace, sty *styles.Styles, workingDir string, items []chat.MessageItem) {
	type nestedFetch struct {
		container chat.NestedToolContainer
		sessionID string
		msgs      []message.Message
		tools     []chat.ToolMessageItem
	}
	var fetches []*nestedFetch
	for _, item := range items {
		container, ok := item.(chat.NestedToolContainer)
		if !ok {
			continue
		}
		toolItem, ok := item.(chat.ToolMessageItem)
		if !ok {
			continue
		}
		fetches = append(fetches, &nestedFetch{
			container: container,
			sessionID: ws.CreateAgentToolSessionID(toolItem.MessageID(), toolItem.ToolCall().ID),
		})
	}
	if len(fetches) == 0 {
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, nestedFetchConcurrency)
	for _, f := range fetches {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			msgs, err := ws.ListMessages(ctx, f.sessionID)
			if err == nil {
				f.msgs = msgs
			}
		})
	}
	wg.Wait()

	var next []chat.MessageItem
	for _, f := range fetches {
		if len(f.msgs) == 0 {
			continue
		}
		ptrs := make([]*message.Message, len(f.msgs))
		for i := range f.msgs {
			ptrs[i] = &f.msgs[i]
		}
		toolResults := chat.BuildToolResultMap(ptrs)
		for _, msg := range ptrs {
			for _, item := range chat.ExtractMessageItems(sty, msg, toolResults, workingDir) {
				toolItem, ok := item.(chat.ToolMessageItem)
				if !ok {
					continue
				}
				// Nested tools render compact.
				if compactable, ok := toolItem.(chat.Compactable); ok {
					compactable.SetCompact(true)
				}
				f.tools = append(f.tools, toolItem)
				next = append(next, toolItem)
			}
		}
	}
	// Agents within agents.
	loadNestedTools(ctx, ws, sty, workingDir, next)
	for _, f := range fetches {
		if len(f.msgs) > 0 {
			f.container.SetNestedTools(f.tools)
		}
	}
}

// setPreparedMessages shows a prepared transcript for msgs, keeping prompts
// that are still being submitted at the end.
func (m *UI) setPreparedMessages(msgs []message.Message, p *preparedTranscript) tea.Cmd {
	for i := range msgs {
		if msgs[i].Role == message.User {
			m.confirmPendingPrompt(msgs[i].Content().SubmissionID)
		}
	}
	if len(msgs) > 0 {
		m.lastUserMessageTime = p.lastUserTime
	}
	items := p.items[:len(p.items):len(p.items)]
	for i := range m.pendingPrompts {
		pending := m.pendingPrompts[i]
		if pending.SessionID == m.currentSessionID() && !m.heldPrompts[pending.ID] {
			items = append(items, chat.ExtractMessageItems(m.com.Styles, &pending, nil, m.com.Workspace.WorkingDir())...)
		}
	}
	m.setMessagePlanFlags(items)

	// If the user switches between sessions while the agent is working we
	// want to make sure the animations are shown. Gate on the agent actually
	// being busy: a session that was killed mid-generation can persist an
	// assistant message with no Finish part, which still reports Spinning()
	// even though nothing is running. Allowing the clock for it here would
	// leave a ghost "working" spinner (and a second one alongside any tool
	// spinner) after the session is reloaded. Messages arriving for the
	// session re-enable the clock.
	m.chat.SetAnimationsAllowed(m.isAgentBusy())

	cmd := m.chat.SetMessages(items...)
	m.chat.SelectLast()
	return cmd
}

// applyLateCancels marks messages interrupted after the transcript was
// prepared, through the same path a live update takes.
func (m *UI) applyLateCancels(msgs []message.Message, prepared map[string]struct{}) []tea.Cmd {
	var cmds []tea.Cmd
	for i := range msgs {
		id := msgs[i].ID
		if _, now := m.canceledMessages[id]; !now {
			continue
		}
		if _, before := prepared[id]; before || msgs[i].IsFinished() {
			continue
		}
		cmds = append(cmds, m.updateSessionMessage(msgs[i]))
	}
	return cmds
}

// replaySessionLoadEvents applies events held during the load on top of
// the snapshot. An event older than the snapshot's copy of its message is
// skipped so a buffered chunk never rolls a finished message back.
func (m *UI) replaySessionLoadEvents(events []pubsub.Event[message.Message], snapshot []message.Message) []tea.Cmd {
	if len(events) == 0 {
		return nil
	}
	byID := make(map[string]*message.Message, len(snapshot))
	for i := range snapshot {
		byID[snapshot[i].ID] = &snapshot[i]
	}
	var cmds []tea.Cmd
	for _, ev := range events {
		p := ev.Payload
		if p.SessionID != m.currentSessionID() {
			cmds = append(cmds, m.handleChildSessionMessage(ev))
			continue
		}
		if snap := byID[p.ID]; snap != nil && ev.Type != pubsub.DeletedEvent && snapshotIsNewer(snap, &p) {
			continue
		}
		switch ev.Type {
		case pubsub.CreatedEvent:
			if m.chat.MessageItem(p.ID) == nil {
				cmds = append(cmds, m.appendSessionMessage(p))
			} else if p.Role == message.Assistant {
				cmds = append(cmds, m.updateSessionMessage(p))
			}
		case pubsub.UpdatedEvent:
			cmds = append(cmds, m.updateSessionMessage(p))
		case pubsub.DeletedEvent:
			m.chat.RemoveMessage(p.ID)
		}
	}
	return cmds
}

func snapshotIsNewer(snap, ev *message.Message) bool {
	if snap.IsFinished() && !ev.IsFinished() {
		return true
	}
	return snap.UpdatedAt != 0 && ev.UpdatedAt != 0 && snap.UpdatedAt > ev.UpdatedAt
}

// drawSessionLoading fills area with the loading line shown while another
// session opens.
func (m *UI) drawSessionLoading(scr uv.Screen, area uv.Rectangle) {
	uv.NewStyledString(m.sessionLoadingView(area.Dx())).Draw(scr, area)
}

func (m *UI) sessionLoadingView(width int) string {
	t := m.com.Styles
	frames := spinner.MiniDot.Frames
	icon := t.Pills.TodoSpinner.Render(frames[m.sessionLoad.frame%len(frames)])
	label := "Opening conversation…"
	if title := strings.Join(strings.Fields(m.sessionLoad.title), " "); title != "" {
		label = "Opening " + title + "…"
	}
	elapsed := ""
	if d := time.Since(m.sessionLoad.started); d >= time.Second {
		elapsed = fmt.Sprintf(" %ds", int(d.Seconds()))
	}
	avail := max(0, width-3-lipgloss.Width(elapsed))
	return " " + icon + " " + t.Pills.HelpKey.Render(ansi.Truncate(label, avail, "…")) + t.Pills.HelpText.Render(elapsed)
}

func (m *UI) discardEmptySession(id string) tea.Cmd {
	cleaner, ok := m.com.Workspace.(session.EmptySessionWorkspace)
	if !ok || id == "" {
		return nil
	}
	return func() tea.Msg {
		if err := cleaner.DiscardEmptySession(context.Background(), id); err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}
