package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// While the wordmark is on screen its gradient drifts diagonally all the
// time and a band of light sweeps across it every few seconds. With no
// wordmark on screen only a slow check runs.
const (
	logoFlowEvery  = 6 * time.Second
	logoShineEvery = 3500 * time.Millisecond
	logoShineSweep = 1000 * time.Millisecond
	logoFrameEvery = 50 * time.Millisecond
	logoIdleCheck  = time.Second
)

// logoFrame is one frame of the wordmark's animation: flow loops from 0 to
// 1, and shine is the sweep's progress from 0 to 1 (0 between sweeps).
type logoFrame struct {
	flow  float64
	shine float64
}

type logoShineMsg struct{}

// logoFrameAt returns the frame at a time since the animation started.
func logoFrameAt(since time.Duration) logoFrame {
	frame := logoFrame{flow: float64(since%logoFlowEvery) / float64(logoFlowEvery)}
	if phase := since % logoShineEvery; phase < logoShineSweep {
		frame.shine = float64(phase) / float64(logoShineSweep)
	}
	return frame
}

// logoShineTick waits for the next frame, or for the next check while no
// wordmark is on screen.
func (m *UI) logoShineTick() tea.Cmd {
	wait := logoFrameEvery
	if !m.logoVisible() {
		wait = logoIdleCheck
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return logoShineMsg{} })
}

// logoVisible reports whether a large wordmark is on screen: the header
// before a chat, or the sidebar beside one.
func (m *UI) logoVisible() bool {
	if m.state == uiChat {
		return !m.isCompact && m.sidebarDrawLogo == m.sidebarLogo
	}
	return true
}

func (m *UI) handleLogoShine() tea.Cmd {
	var frame logoFrame
	if m.logoVisible() {
		frame = logoFrameAt(time.Since(m.logoShineStart))
	}
	if frame != m.logoFrame {
		m.logoFrame = frame
		if m.state == uiChat && !m.isCompact {
			// A short sidebar draws the small logo instead; keep it.
			big := m.sidebarDrawLogo == m.sidebarLogo
			m.cacheSidebarLogo(m.layout.sidebar.Dx())
			if big {
				m.sidebarDrawLogo = m.sidebarLogo
			}
		}
	}
	return m.logoShineTick()
}
