package model

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
)

// Chat represents the chat UI model that handles chat interactions and
// messages.
type Chat struct {
	com      *common.Common
	list     *list.List
	idInxMap map[string]int // Map of message IDs to their indices in the list

	// flat holds every message item in order. The list shows it with
	// each run of tool calls folded into a status group; groupOf and
	// folded remember the grouping so groups keep their state.
	flat    []chat.MessageItem
	groupOf map[string]*chat.ToolGroupItem
	folded  map[chat.MessageItem]bool
	// hiddenInfo holds info rows left out because they would label
	// nothing, so callers still find them by ID.
	hiddenInfo map[string]chat.MessageItem
	// agentBusy is passed to groups so the live one keeps a status
	// between steps.
	agentBusy bool
	// waitingItem is a display-only Working row until an assistant message
	// arrives. It never enters flat, history, or the persisted transcript.
	waitingItem    chat.MessageItem
	waitingFor     string
	awaitingReply  bool
	inputConfirmed bool
	// canBackground is passed to groups: whether Ctrl+B can move the
	// agent's running command to the background.
	canBackground bool

	// animRunning is true while the shared animation clock has a tick
	// outstanding. The clock stops itself when no visible item is spinning
	// and is re-armed by EnsureAnimating once one is. animGen identifies
	// the current clock: a tick carrying an older generation belongs to a
	// clock that was superseded and is ignored, so a delayed tick can
	// never run alongside a replacement. animArmedAt lets EnsureAnimating
	// recover if a tick is ever lost.
	animRunning bool
	animGen     uint64
	animArmedAt time.Time
	// animNow is an injectable clock for tests (nil == real time).
	animNow func() time.Time

	// animAllowed gates the shared clock. setSessionMessages clears it when
	// reloading a session whose agent is not busy so ghost spinners (an
	// assistant message that never got a Finish part) stay still; the
	// message handlers re-enable it when new work arrives.
	animAllowed bool

	// Mouse state
	mouseDown     bool
	mouseDownItem int // Item index where mouse was pressed
	mouseDownX    int // X position in item content (character offset)
	mouseDownY    int // Y position in item (line offset)
	mouseDragItem int // Current item index being dragged over
	mouseDragX    int // Current X in item content
	mouseDragY    int // Current Y in item

	// Click tracking for double/triple clicks
	lastClickTime time.Time
	lastClickX    int
	lastClickY    int
	clickCount    int

	// Pending single click action (delayed to detect double-click)
	pendingClickID int // Incremented on each click to invalidate old pending clicks
	openLink       string
	openImage      *message.Attachment
	heldLinkClick  *DelayedClickMsg
	linkDragged    bool

	// follow is a flag to indicate whether the view should auto-scroll to
	// bottom on new messages.
	follow bool

	// drawCache memoizes the decoded form of the last list.Render output so
	// repeat frames with byte-identical content skip the per-cell ANSI
	// reparse that uv.StyledString.Draw performs every call. See F9
	// (docs/notes/2026-05-12-chat-rendering-perf.md §4.8). Bounded to one
	// entry; invalidated implicitly by string inequality on the next Draw.
	drawCache *chatDrawCache

	// Scrollbar visibility state
	scrollbarVisible bool
	scrollbarHideSeq int // current sequence number for hide timer
	// scrollbarEdge is the chat area's last column, where the scrollbar
	// shows (-1 before the first draw). scrollbarGrab is where on the
	// thumb a drag holds it, or -1 when not dragging.
	scrollbarEdge int
	scrollbarGrab int
	scrollbarMode string // "default", "always", or "never"

	// resizing suppresses the O(N) total-height scan while a resize is in
	// flight (and during the incremental warm afterward), so a drag only
	// reflows the visible items. resizeSettleSeq guards stale settle/warm
	// timers; warmNext tracks warming progress through the message list.
	resizing        bool
	resizeSettleSeq int
	warmNext        int
}

// NewChat creates a new instance of [Chat] that handles chat interactions and
// messages.
func NewChat(com *common.Common, scrollbarMode string) *Chat {
	c := &Chat{
		com:           com,
		idInxMap:      make(map[string]int),
		scrollbarMode: scrollbarMode,
		scrollbarEdge: -1,
		scrollbarGrab: -1,
		animAllowed:   true,
	}
	l := list.NewList()
	l.SetGap(1)
	l.RegisterRenderCallback(c.applyHighlightRange)
	l.RegisterRenderCallback(list.FocusedRenderCallback(l))
	c.list = l
	c.mouseDownItem = -1
	c.mouseDragItem = -1
	return c
}

// Height returns the height of the chat view port.
func (m *Chat) Height() int {
	return m.list.Height()
}

// SetSize sets the size of the chat view port.
func (m *Chat) SetSize(width, height int) {
	// Reserve a column for the scrollbar when content overflows, decided
	// with a cheap bounded overflow check rather than the O(N) total height.
	// The final width is applied in a single SetSize so that an unchanged
	// width is a no-op — critical after warming, where re-setting the same
	// width would otherwise drop the freshly warmed cache and reintroduce
	// the blocking full render.
	// Capture whether we should stay pinned to the bottom *before* the size
	// change. A width change rewraps every item, so the list's line offsets
	// (offsetIdx/offsetLine) become stale and AtBottom() can no longer be
	// trusted afterward. follow short-circuits the AtBottom() walk in the
	// common streaming case.
	wasFollowing := m.follow || m.AtBottom()
	listWidth := width
	if m.list.Overflows(height) {
		listWidth = max(0, width-1)
	}
	m.list.SetSize(listWidth, height)
	// Re-anchor to bottom if we were pinned there before the resize.
	if wasFollowing {
		m.ScrollToBottom()
	}
}

// ListWidth returns the width of the chat's rows, which leaves out the
// scrollbar's column when the content overflows.
func (m *Chat) ListWidth() int {
	return m.list.Width()
}

// Len returns the number of items in the chat list.
func (m *Chat) Len() int {
	return m.list.Len()
}

// InvalidateRenderCaches drops cached rendered output on every message
// item so the next draw re-renders with the current styles.
func (m *Chat) InvalidateRenderCaches() {
	items := make([]chat.MessageItem, 0, m.list.Len())
	for i := range m.list.Len() {
		if item, ok := m.list.ItemAt(i).(chat.MessageItem); ok {
			items = append(items, item)
		}
	}
	chat.ClearItemCaches(items)
}

// InvalidateVisibleRenderCaches drops cached rendered output on the
// message items currently visible in the viewport, so the next draw
// re-renders them with the current styles. Items outside the viewport
// keep their cached output, which keeps theme previews fast in large
// sessions; they are re-rendered by a later full invalidation.
func (m *Chat) InvalidateVisibleRenderCaches() {
	start, end := m.list.VisibleItemIndices()
	for i := start; i <= end; i++ {
		if item, ok := m.list.ItemAt(i).(chat.MessageItem); ok {
			chat.ClearItemCaches([]chat.MessageItem{item})
		}
	}
}

// SetMessages sets the chat messages to the provided list of message items.
func (m *Chat) SetMessages(msgs ...chat.MessageItem) tea.Cmd {
	m.scrollbarVisible = false // Reset scrollbar visibility on new session load
	m.flat = slices.Clone(msgs)
	if m.awaitingReply && !slices.ContainsFunc(m.flat, func(item chat.MessageItem) bool {
		user, ok := item.(*chat.UserMessageItem)
		return ok && submissionKey(user) == m.waitingFor
	}) {
		m.awaitingReply = false
		m.inputConfirmed = false
		m.waitingItem = nil
		m.waitingFor = ""
	}
	m.groupOf = nil
	m.regroup()
	m.ScrollToBottom()
	return nil
}

// AppendMessages appends a new message item to the chat list.
func (m *Chat) AppendMessages(msgs ...chat.MessageItem) {
	if len(msgs) == 0 {
		return
	}
	m.flat = append(m.flat, msgs...)
	m.regroup()
}

// InsertRelatedMessages keeps late rows beside their original assistant/tool
// step, rather than placing old-turn updates beneath newer user submissions.
func (m *Chat) InsertRelatedMessages(messageID string, before bool, msgs ...chat.MessageItem) {
	if len(msgs) == 0 {
		return
	}
	first, last := -1, -1
	for i, item := range m.flat {
		related := item.ID() == messageID || item.ID() == chat.AssistantInfoID(messageID)
		if tool, ok := item.(chat.ToolMessageItem); ok && tool.MessageID() == messageID {
			related = true
		}
		if related {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		m.AppendMessages(msgs...)
		return
	}
	position := last + 1
	if before {
		position = first
	} else if _, tools := msgs[0].(chat.ToolMessageItem); tools {
		for i, item := range m.flat {
			if item.ID() == chat.AssistantInfoID(messageID) {
				position = i
				break
			}
		}
	}
	m.flat = slices.Insert(m.flat, position, msgs...)
	m.regroup()
}

func submissionKey(user *chat.UserMessageItem) string {
	if id := user.SubmissionID(); id != "" {
		return id
	}
	return user.ID()
}

func (m *Chat) BeginInput(id string) {
	if m.waitingFor != id {
		m.waitingItem = nil
	}
	m.waitingFor = id
	m.awaitingReply = true
	m.inputConfirmed = false
}

func (m *Chat) ConfirmInput(id string) {
	if m.awaitingReply && m.waitingFor == id {
		m.inputConfirmed = true
	}
}

// Adopt the displayed Working item into the real response so confirmation
// and handoff preserve its animation phase as well as its screen position.
func (m *Chat) AcceptResponse(msg *message.Message, items []chat.MessageItem) []chat.MessageItem {
	if !m.awaitingReply || !m.inputConfirmed {
		return items
	}
	for i, item := range items {
		if _, ok := item.(*chat.AssistantMessageItem); ok && m.waitingItem != nil {
			if waiting, ok := m.waitingItem.(*chat.AssistantMessageItem); ok {
				waiting.SetMessage(msg)
				items[i] = waiting
			}
		}
	}
	m.awaitingReply = false
	m.inputConfirmed = false
	m.waitingItem = nil
	m.waitingFor = ""
	return items
}

// Refold regroups the chat if item changed whether it folds into a status
// group, e.g. a thinking step that started to answer.
func (m *Chat) Refold(item chat.MessageItem) {
	if f, ok := m.folded[item]; ok && f != chat.Foldable(item) {
		m.regroup()
		return
	}
	// A step can start or stop compacting without changing its fold.
	m.markLiveStatus()
}

// regroup rebuilds the list from flat, folding each run of tool calls into
// a status group, and keeps the scroll position and selection on the same
// items.
func (m *Chat) regroup() {
	// ponytail: O(n) per structural change; fine for thousands of items.
	offIdx, offLine := m.list.ScrollPosition()
	anchor, selected := m.list.ItemAt(offIdx), m.list.ItemAt(m.list.Selected())

	items := make([]list.Item, 0, len(m.flat))
	m.idInxMap = make(map[string]int, len(m.flat))
	m.hiddenInfo = make(map[string]chat.MessageItem)
	groupOf := make(map[string]*chat.ToolGroupItem)
	m.folded = make(map[chat.MessageItem]bool, len(m.flat))
	owner := make(map[list.Item]int, len(m.flat))
	used := make(map[*chat.ToolGroupItem]bool)
	place := func(it chat.MessageItem, idx int) {
		m.idInxMap[it.ID()] = idx
		owner[it] = idx
		if container, ok := it.(chat.NestedToolContainer); ok {
			for _, nested := range container.NestedTools() {
				m.idInxMap[nested.ID()] = idx
			}
		}
	}
	for i := 0; i < len(m.flat); {
		j := i
		for j < len(m.flat) && chat.Foldable(m.flat[j]) {
			m.folded[m.flat[j]] = true
			j++
		}
		if run := m.flat[i:j]; chat.GroupsTools(run) {
			// Reuse the group that held any of these steps so it keeps
			// its expanded state.
			var g *chat.ToolGroupItem
			for _, it := range run {
				if old := m.groupOf[it.ID()]; old != nil && !used[old] {
					g = old
					break
				}
			}
			if g == nil {
				g = chat.NewToolGroupItem(m.com.Styles)
			}
			used[g] = true
			if !slices.Equal(g.Children(), run) {
				g.SetChildren(slices.Clone(run))
			}
			idx := len(items)
			items = append(items, g)
			owner[g] = idx
			m.idInxMap[g.ID()] = idx
			for _, it := range run {
				groupOf[it.ID()] = g
				place(it, idx)
			}
			i = j
			continue
		}
		if j == i {
			m.folded[m.flat[i]] = false
			j = i + 1
		}
		for _, it := range m.flat[i:j] {
			if _, info := it.(*chat.AssistantInfoItem); info {
				var prev list.Item
				if len(items) > 0 {
					prev = items[len(items)-1]
				}
				if chat.InfoLabelsNothing(prev) {
					m.hiddenInfo[it.ID()] = it
					continue
				}
			}
			place(it, len(items))
			items = append(items, it)
		}
		i = j
	}
	if m.agentBusy && len(m.flat) > 0 {
		user, _ := m.flat[len(m.flat)-1].(*chat.UserMessageItem)
		if m.awaitingReply {
			user = nil
			spinning := false
			if animatable, ok := m.flat[len(m.flat)-1].(chat.Animatable); ok {
				spinning = animatable.Spinning()
			}
			if !spinning {
				for i := len(m.flat) - 1; i >= 0; i-- {
					if candidate, ok := m.flat[i].(*chat.UserMessageItem); ok && submissionKey(candidate) == m.waitingFor {
						user = candidate
						break
					}
				}
			}
		}
		if user != nil {
			if m.waitingItem == nil || m.waitingFor != submissionKey(user) {
				m.waitingFor = submissionKey(user)
				m.waitingItem = chat.NewAssistantMessageItem(m.com.Styles, &message.Message{
					ID: "waiting:" + m.waitingFor, Role: message.Assistant,
				})
			}
			owner[m.waitingItem] = len(items)
			items = append(items, m.waitingItem)
		} else if !m.awaitingReply {
			m.waitingItem, m.waitingFor = nil, ""
		}
	} else {
		m.waitingItem = nil
		if !m.awaitingReply {
			m.waitingFor = ""
		}
	}
	m.groupOf = groupOf
	for g := range used {
		g.SetLive(owner[g] == len(items)-1)
		g.SetBusy(m.agentBusy)
		g.SetCanBackground(m.canBackground)
	}

	newOff, ok := owner[anchor]
	if !ok || items[newOff] != anchor {
		offLine = 0
	}
	if !ok {
		newOff = offIdx
	}
	newSel, ok := owner[selected]
	if !ok {
		newSel = m.list.Selected()
	}
	m.list.ReplaceItems(items, newOff, offLine, newSel)
	m.markLiveStatus()
}

// UpdateNestedToolIDs updates the ID map for nested tools within a container.
// Call this after modifying nested tools to ensure animations work correctly.
func (m *Chat) UpdateNestedToolIDs(containerID string) {
	idx, ok := m.idInxMap[containerID]
	if !ok {
		return
	}
	container, ok := m.MessageItem(containerID).(chat.NestedToolContainer)
	if !ok {
		return
	}

	// Register all nested tool IDs to point to the container's index.
	for _, nested := range container.NestedTools() {
		m.idInxMap[nested.ID()] = idx
	}
}

// Focus sets the focus state of the chat component.
func (m *Chat) Focus() {
	m.list.Focus()
}

// Blur removes the focus state from the chat component.
func (m *Chat) Blur() {
	m.list.Blur()
}

// ScrollPosition returns the list's first visible item index and the line
// offset into it.
func (m *Chat) ScrollPosition() (offsetIdx, offsetLine int) {
	return m.list.ScrollPosition()
}

// Offset returns the scroll offset in lines from the top of the list.
func (m *Chat) Offset() int {
	return m.list.Offset()
}

// Selected returns the index of the selected item.
func (m *Chat) Selected() int {
	return m.list.Selected()
}

// Focused returns whether the chat list is focused.
func (m *Chat) Focused() bool {
	return m.list.Focused()
}

// RenderState captures everything about the chat that affects its rendered
// output and is not carried by item versions. Anything added to Chat that
// Draw reads belongs here, so callers that memoize whole frames stay correct
// without knowing Chat's internals.
type RenderState struct {
	OffsetIdx        int
	OffsetLine       int
	Selected         int
	Focused          bool
	ScrollbarVisible bool
	// ItemsVersion changes when any message mutates its rendered output.
	ItemsVersion uint64
}

// RenderState returns the current render-affecting chat state. It renders
// nothing; the only non-constant part is the item version fold, which reads
// one field per message.
func (m *Chat) RenderState() RenderState {
	offsetIdx, offsetLine := m.ScrollPosition()
	return RenderState{
		OffsetIdx:        offsetIdx,
		OffsetLine:       offsetLine,
		Selected:         m.Selected(),
		Focused:          m.Focused(),
		ScrollbarVisible: m.scrollbarVisible,
		ItemsVersion:     m.list.ItemsVersion(),
	}
}

// ClearMessages removes all messages from the chat list.
func (m *Chat) ClearMessages() {
	m.idInxMap = make(map[string]int)
	m.flat, m.groupOf, m.folded = nil, nil, nil
	m.scrollbarVisible = false
	m.list.SetItems()
	m.ClearMouse()
	// Nothing is left to warm; end any warming so the scrollbar returns.
	m.resizing = false
	m.resizeSettleSeq++
}

// RemoveMessage removes a message from the chat list by its ID.
func (m *Chat) RemoveMessage(id string) {
	i := slices.IndexFunc(m.flat, func(it chat.MessageItem) bool { return it.ID() == id })
	if i < 0 {
		return
	}
	m.flat = slices.Delete(m.flat, i, i+1)
	m.regroup()
}

// ReplaceMessage confirms a pending item without moving it in the timeline.
func (m *Chat) ReplaceMessage(id string, items ...chat.MessageItem) bool {
	i := slices.IndexFunc(m.flat, func(it chat.MessageItem) bool { return it.ID() == id })
	if i < 0 {
		return false
	}
	m.flat = slices.Concat(m.flat[:i], items, m.flat[i+1:])
	m.regroup()
	return true
}

// CancelRunning gives immediate feedback without changing saved messages.
func (m *Chat) CancelRunning(continuing bool) []string {
	var ids []string
	for _, item := range m.flat {
		switch item := item.(type) {
		case chat.ToolMessageItem:
			// Finished on ToolCall means its arguments finished streaming.
			// Execution is only finished once the result has arrived.
			if result, ok := item.(interface{ Result() *message.ToolResult }); ok && result.Result() != nil {
				continue
			}
			if !item.Finished() && item.Status() != chat.ToolStatusCanceled {
				item.SetStatus(chat.ToolStatusCanceled)
				ids = append(ids, item.MessageID())
			}
		case *chat.AssistantMessageItem:
			if id := item.Cancel(); id != "" {
				ids = append(ids, id)
			}
		case *chat.ShellItem:
			item.Cancel()
		}
	}
	m.SetAgentBusy(continuing)
	m.regroup()
	return ids
}

// MessageItem returns the message item with the given ID, or nil if not
// found. Steps folded into a status group are returned themselves.
func (m *Chat) MessageItem(id string) chat.MessageItem {
	idx, ok := m.idInxMap[id]
	if !ok {
		if hidden, ok := m.hiddenInfo[id]; ok {
			return hidden
		}
		return nil
	}
	item, ok := m.list.ItemAt(idx).(chat.MessageItem)
	if !ok {
		return nil
	}
	if g, ok := item.(*chat.ToolGroupItem); ok && g.ID() != id {
		if child := g.Child(id); child != nil {
			return child
		}
	}
	return item
}

// ToggleExpandedSelectedItem expands the selected message item if it is expandable.
func (m *Chat) ToggleExpandedSelectedItem() {
	if expandable, ok := m.list.SelectedItem().(chat.Expandable); ok {
		wasFollowing := m.follow
		expandable.ToggleExpanded()
		if wasFollowing {
			m.ScrollToBottom()
		}
	}
}

// IsSelectedShellItem returns true if the currently selected item is a
// ShellItem (bang-mode result).
func (m *Chat) IsSelectedShellItem() bool {
	_, ok := m.list.SelectedItem().(*chat.ShellItem)
	return ok
}

// ScrollSelectedShellHorizontal scrolls the selected ShellItem horizontally
// by delta columns. No-op if the selected item is not a ShellItem.
func (m *Chat) ScrollSelectedShellHorizontal(delta int) {
	if shell, ok := m.list.SelectedItem().(*chat.ShellItem); ok {
		shell.ScrollHorizontal(delta)
	}
}

// HandleKeyMsg handles key events for the chat component.
func (m *Chat) HandleKeyMsg(key tea.KeyMsg) (bool, tea.Cmd) {
	if m.list.Focused() {
		if handler, ok := m.list.SelectedItem().(chat.KeyEventHandler); ok {
			return handler.HandleKeyEvent(key)
		}
	}
	return false, nil
}
