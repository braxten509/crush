package model

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// leadingSpaces counts the spaces at the start of a stripped line.
func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// drawStatusLines draws the status bar into a screen buffer and returns the
// visible content of each row.
func drawStatusLines(t *testing.T, st *Status, w, h int) []string {
	t.Helper()
	scr := uv.NewScreenBuffer(w, h)
	st.Draw(scr, uv.Rect(0, 0, w, h))
	lines := strings.Split(ansi.Strip(scr.Render()), "\n")
	return lines
}

func TestStatusDrawExpandedHelpRowsAlignWithBadge(t *testing.T) {
	t.Parallel()

	u := newPrismTestUI()
	u.attachments = attachments.New(nil, attachments.Keymap{})
	st := u.status
	st.helpKm = u
	st.SetWidth(100)
	st.ToggleHelp()
	st.SetMode(uiInputModePlan, false)

	lines := drawStatusLines(t, st, 100, 6)
	require.True(t, strings.HasPrefix(lines[0], strings.Repeat(" ", badgeLeftInset)+" "+"PLAN MODE"),
		"the badge row must start with the badge inset and its padding: %q", lines[0])

	// Every subsequent help row must start at the same column as the hints
	// on the badge row: badge inset + badge width + separator + help padding.
	badge := st.modeBadge()
	wantHintsCol := badgeLeftInset + lipgloss.Width(badge) + 1 + u.com.Styles.Status.Help.GetPaddingLeft()
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		require.Equal(t, wantHintsCol, leadingSpaces(line),
			"expanded help row %d must align with the badge row hints", i+1)
	}
}

func TestStatusDrawExpandedHelpRowsAlignWithoutBadge(t *testing.T) {
	t.Parallel()

	u := newPrismTestUI()
	u.attachments = attachments.New(nil, attachments.Keymap{})
	st := u.status
	st.helpKm = u
	st.SetWidth(100)
	st.ToggleHelp()
	st.SetMode(uiInputModeCode, false)

	lines := drawStatusLines(t, st, 100, 6)
	wantCol := u.com.Styles.Status.Help.GetPaddingLeft()
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			break
		}
		require.Equal(t, wantCol, leadingSpaces(line),
			"expanded help row %d must align with the first row", i)
	}
}

// A notice spans the composer's band: its band starts where the composer's
// does, its label lines up with the composer's text, and it ends with the
// composer's band.
func TestStatusNoticeSpansComposerBand(t *testing.T) {
	t.Parallel()

	u := newPrismTestUI()
	st := u.status
	st.SetHideHelp(true)
	st.SetBand(0, 90)
	st.SetInfoMsg(util.InfoMsg{Type: util.InfoTypeUpdate, Msg: "Crush update available"})
	scr := uv.NewScreenBuffer(100, 1)
	st.Draw(scr, uv.Rect(0, 0, 100, 1))
	line := ansi.Strip(scr.Render())
	require.Equal(t, promptWidth, strings.Index(line, "HEY!"), "the label lines up with the composer's text")
	bg := u.com.Styles.Status.UpdateMessage.GetBackground()
	sameColor := func(c color.Color) bool {
		if c == nil {
			return false
		}
		r1, g1, b1, _ := c.RGBA()
		r2, g2, b2, _ := bg.RGBA()
		return r1 == r2 && g1 == g2 && b1 == b2
	}
	require.True(t, sameColor(scr.CellAt(0, 0).Style.Bg), "the band starts at the composer's left edge")
	require.True(t, sameColor(scr.CellAt(89, 0).Style.Bg), "and runs to its right edge")
	require.False(t, sameColor(scr.CellAt(90, 0).Style.Bg), "but no further")
}
