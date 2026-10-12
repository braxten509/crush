package sessionhost

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// spinnerFrames is the mark of a busy chat, one frame per tick.
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	activityInterval = 100 * time.Millisecond
	activityFrames   = len(spinnerFrames)
)

type activityTickMsg struct{ time.Time }

// tickActivity keeps all spinners on the same wall-clock rhythm,
// including ones that start later or belong to another Crush window.
func (h *Host) tickActivity() tea.Cmd {
	return h.tickActivityAt(time.Now())
}

func (h *Host) tickActivityAt(now time.Time) tea.Cmd {
	if h.activityTicking || !h.hasVisibleActivity() {
		return nil
	}
	h.activityFrame = int(now.UnixMilli()/activityInterval.Milliseconds()) % activityFrames
	h.activityTicking = true
	delay := activityInterval - now.Sub(now.Truncate(activityInterval))
	return tea.Tick(delay, func(at time.Time) tea.Msg { return activityTickMsg{at} })
}

func (h *Host) hasVisibleActivity() bool {
	if h.width <= 0 || h.height <= 0 {
		return false
	}
	block := stripBlockHeight
	if h.listOpen() {
		block = listBlockHeight
	}
	first, count := h.visibleRange(block)
	for _, s := range h.sessions[first : first+count] {
		s.mu.Lock()
		working := (s.status.State == StateWorking || s.status.State == StateBackground) && !s.stopped
		s.mu.Unlock()
		if working {
			return true
		}
	}
	return false
}

// activityMark is the spinner frame every busy chat shows now. It is one
// cell wide like the other marks, so titles never shift.
func (h *Host) activityMark() string {
	return spinnerFrames[h.activityFrame%activityFrames]
}
