package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/chat"
)

// markLiveStatus keeps a single live status line, at the end of the chat:
// steps with something newer after them show none of their own. If one of
// them is still compacting, the live item at the end says so instead of
// "Thinking".
func (m *Chat) markLiveStatus() {
	last := m.list.Len() - 1
	for last >= 0 {
		if _, info := m.list.ItemAt(last).(*chat.AssistantInfoItem); !info {
			break
		}
		last--
	}
	compacting := false
	for i := range m.list.Len() {
		if a, ok := m.list.ItemAt(i).(*chat.AssistantMessageItem); ok {
			a.SetSuperseded(i < last)
			compacting = compacting || (i < last && a.Compacting())
		}
	}
	// Steps inside groups take their status from the group.
	for _, it := range m.flat {
		if a, ok := it.(*chat.AssistantMessageItem); ok && m.groupOf[a.ID()] != nil {
			a.SetSuperseded(false)
		}
	}
	for i := range m.list.Len() {
		switch it := m.list.ItemAt(i).(type) {
		case *chat.AssistantMessageItem:
			it.SetCompactingEarlier(i == last && compacting)
		case *chat.ToolGroupItem:
			it.SetCompactingEarlier(i == last && compacting)
		}
	}
}

// animTickMsg is the shared animation clock. One tick advances every
// visible spinner by a frame; there is one live clock at a time regardless
// of how many items are animating. gen is the clock generation the tick
// was armed for. chat is the chat that armed it, so a second chat (the
// sub-agent view) keeps its own clock; nil means the main chat.
type animTickMsg struct {
	gen  uint64
	chat *Chat
}

// hasVisibleAnimation reports whether any item in the viewport is spinning.
func (m *Chat) hasVisibleAnimation() bool {
	if m.list.Len() == 0 {
		return false
	}
	startIdx, endIdx := m.list.VisibleItemIndices()
	for idx := startIdx; idx <= endIdx; idx++ {
		if animatable, ok := m.list.ItemAt(idx).(chat.Animatable); ok && animatable.Spinning() {
			return true
		}
	}
	return false
}

// animClockLostAfter is how long an armed clock may go without its tick
// arriving before EnsureAnimating assumes the command was lost and arms a
// new one. Generous enough that a slow frame never trips it.
const animClockLostAfter = 2 * time.Second

// SetAgentBusy tells status groups whether the agent is still working.
func (m *Chat) SetAgentBusy(busy bool) {
	changed := m.agentBusy != busy
	m.agentBusy = busy
	if changed {
		m.regroup()
	}
	for _, g := range m.groupOf {
		g.SetBusy(busy)
	}
}

// SetCanBackground tells status groups whether Ctrl+B can move the agent's
// running command to the background.
func (m *Chat) SetCanBackground(can bool) {
	m.canBackground = can
	for _, g := range m.groupOf {
		g.SetCanBackground(can)
	}
}

// BackgroundableGroup returns the group whose running command Ctrl+B would
// move to the background, or nil.
func (m *Chat) BackgroundableGroup() *chat.ToolGroupItem {
	for _, g := range m.groupOf {
		if g.CanBackground() {
			return g
		}
	}
	return nil
}

// SetAnimationsAllowed gates the shared animation clock. It is cleared
// when a session is reloaded whose agent is not busy so ghost spinners (an
// assistant message that never got a Finish part) stay still, and re-enabled
// whenever new messages arrive for the current session.
func (m *Chat) SetAnimationsAllowed(allowed bool) {
	m.animAllowed = allowed
}

// EnsureAnimating starts the shared animation clock if a visible item is
// spinning and no tick is outstanding. Update calls it in its tail on
// every message so any change that puts a spinner on screen (new message,
// tool update, scroll, session load) starts the clock without per-call-site
// wiring. It is the only place a tick is armed apart from Tick itself.
func (m *Chat) EnsureAnimating() tea.Cmd {
	if !m.animAllowed {
		m.animRunning = false
		return nil
	}
	if m.animRunning && m.now().Sub(m.animArmedAt) < animClockLostAfter {
		return nil
	}
	if !m.hasVisibleAnimation() {
		m.animRunning = false
		return nil
	}
	return m.armAnimClock()
}

func (m *Chat) armAnimClock() tea.Cmd {
	m.animRunning = true
	m.animArmedAt = m.now()
	m.animGen++
	gen := m.animGen
	return tea.Tick(anim.FrameInterval(), func(time.Time) tea.Msg {
		return animTickMsg{gen: gen, chat: m}
	})
}

func (m *Chat) now() time.Time {
	if m.animNow != nil {
		return m.animNow()
	}
	return time.Now()
}

// stopAnimating marks the clock as stopped so the next EnsureAnimating
// re-arms it. Called while consuming the current clock's tick; bumping
// the generation also retires that clock in case the tick was not the
// current one.
func (m *Chat) stopAnimating(msg animTickMsg) {
	if msg.gen != m.animGen {
		return
	}
	m.animRunning = false
}

// Tick advances every visible spinning item by one frame. It reports
// whether any rendered output changed and returns the next tick while a
// visible item is still spinning; when none is, the clock stops and no
// command is returned. Ticks from a superseded clock generation are
// ignored so they cannot re-arm a second clock.
func (m *Chat) Tick(msg animTickMsg) (changed bool, cmd tea.Cmd) {
	if msg.gen != m.animGen {
		return false, nil
	}
	m.animRunning = false
	if m.list.Len() == 0 {
		return false, nil
	}
	spinning := false
	startIdx, endIdx := m.list.VisibleItemIndices()
	for idx := startIdx; idx <= endIdx; idx++ {
		animatable, ok := m.list.ItemAt(idx).(chat.Animatable)
		if !ok || !animatable.Spinning() {
			continue
		}
		spinning = true
		if animatable.Advance() {
			changed = true
		}
	}
	if !spinning {
		return changed, nil
	}
	return changed, m.armAnimClock()
}
