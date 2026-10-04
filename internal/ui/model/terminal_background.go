package model

import (
	"context"
	"image/color"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Crush sets the terminal's background color, and the renderer only sends
// it again when it changes. Terminals can drop it on their own: Konsole
// reapplies a tab's profile (default background included) whenever that
// profile is saved. Crush asks the terminal for its background many times a
// second and sets its own again as soon as it is lost, before the change is
// noticeable. Replies that match are dropped by [Filter] before they reach
// the update loop, so checking never redraws the screen.

// backgroundCheckEvery is how often Crush asks for the terminal background.
const backgroundCheckEvery = 50 * time.Millisecond

// maxBackgroundResends stops resending to a terminal that reports another
// background but never takes Crush's (it can't set one).
const maxBackgroundResends = 3

// backgroundQuery is the message type of [tea.RequestBackgroundColor]
// (unexported in Bubble Tea).
var backgroundQuery = reflect.TypeOf(tea.RequestBackgroundColor())

// isBackgroundQuery reports whether msg is a background check on its way to
// the terminal. It changes nothing on screen.
func isBackgroundQuery(msg tea.Msg) bool {
	return reflect.TypeOf(msg) == backgroundQuery
}

// WatchTerminalBackground asks the terminal for its background until ctx
// ends. send is the program's Send.
func WatchTerminalBackground(ctx context.Context, m *UI, send func(tea.Msg)) {
	ticker := time.NewTicker(backgroundCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.watchBackground.Load() {
				send(tea.RequestBackgroundColor())
			}
		}
	}
}

// keepBackgroundReply reports whether the terminal's reported background
// needs the model's attention: only when Crush's color was lost.
func (m *UI) keepBackgroundReply(msg tea.BackgroundColorMsg) bool {
	want := m.com.Styles.Background
	if m.isTransparent || want == nil || msg.Color == nil {
		return false
	}
	if sameRGB(msg.Color, want) {
		m.backgroundResends = 0
		return false
	}
	return m.backgroundResends < maxBackgroundResends
}

// handleTerminalBackground puts Crush's background back when the terminal
// reports another one.
func (m *UI) handleTerminalBackground(msg tea.BackgroundColorMsg) {
	if !m.keepBackgroundReply(msg) {
		return
	}
	m.backgroundResends++
	// A value of another type compares unequal, so the renderer sends the
	// (same) color again.
	m.resendBackground = !m.resendBackground
}

// viewBackground is the color the view asks the terminal to use.
func (m *UI) viewBackground() color.Color {
	bg := m.com.Styles.Background
	if !m.resendBackground || bg == nil {
		return bg
	}
	r, g, b, _ := bg.RGBA()
	return color.NRGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: 0xffff}
}

func sameRGB(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar>>8 == br>>8 && ag>>8 == bg>>8 && ab>>8 == bb>>8
}
