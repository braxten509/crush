package sessionhost

import (
	"image/color"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	activityInterval = 100 * time.Millisecond
	activityFrames   = 16
)

type activityTickMsg struct{ time.Time }

// tickActivity keeps all glowing dots on the same wall-clock rhythm,
// including dots that start later or belong to another Crush window.
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

// activityColor gives both working colors the same brightness curve.
// The filled circles never disappear or shift the titles.
func (h *Host) activityColor(foreground, background color.Color) color.Color {
	if h.activityFrame == 0 {
		return foreground
	}
	amount := 0.85 + 0.15*math.Cos(2*math.Pi*float64(h.activityFrame)/activityFrames)
	r, g, b, _ := foreground.RGBA()
	br, bg, bb, _ := background.RGBA()
	blend := func(fg, bg uint32) uint8 {
		return uint8(math.Round((float64(bg) + (float64(fg)-float64(bg))*amount) / 257))
	}
	return color.RGBA{R: blend(r, br), G: blend(g, bg), B: blend(b, bb), A: 255}
}
