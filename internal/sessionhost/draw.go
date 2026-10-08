package sessionhost

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/home"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// The list's rows: a header, "+ New session", a blank row, then one block
// per session (four lines and a gap) and the key hint on the last row. The
// strip has the same first three rows, then two lines and a gap per
// session.
const (
	firstSessionRow  = 3
	listBlockHeight  = 5
	stripBlockHeight = 3
)

// sideRow is what a spot in the list or strip does when clicked.
type sideRow struct {
	toggle     bool
	newSession bool
	close      bool
	session    int // -1 when not on a session
}

type palette struct {
	bg, text, muted, subtle, accent, working, waiting, selected, border color.Color
}

func (h *Host) palette() palette {
	s := h.styles
	return palette{
		bg:      s.Background,
		text:    s.Header.Wrapper.GetForeground(),
		muted:   s.Sidebar.WorkingDir.GetForeground(),
		subtle:  s.ModelInfo.Reasoning.GetForeground(),
		accent:  s.Header.Diagonals.GetForeground(),
		working: s.Resource.BusyIcon.GetForeground(),
		waiting: s.Resource.NeedsAuthIcon.GetForeground(),
		// The shown session sits on the same band as the user's messages.
		selected: s.Messages.UserBackground,
		border:   s.Header.Separator.GetForeground(),
	}
}

// visibleRange returns the sessions that fit, keeping the shown one in
// view.
func (h *Host) visibleRange(block int) (first, count int) {
	count = max((h.height-firstSessionRow-1)/block, 1)
	if h.active >= count {
		first = h.active - count + 1
	}
	return first, min(count, len(h.sessions)-first)
}

func (h *Host) rowAt(x, y int) sideRow {
	r := sideRow{session: -1}
	open := h.listOpen()
	block := stripBlockHeight
	if open {
		block = listBlockHeight
	}
	switch {
	case y == 0:
		r.toggle = !open || x >= listWidth-4
	case y == 1:
		r.newSession = true
	case y >= firstSessionRow:
		first, count := h.visibleRange(block)
		k, line := (y-firstSessionRow)/block, (y-firstSessionRow)%block
		if k < count && line < block-1 {
			r.session = first + k
			r.close = open && line == 0 && x >= listWidth-5 && x <= listWidth-2
		}
	}
	return r
}

func (h *Host) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true
	p := h.palette()
	v.BackgroundColor = p.bg
	if h.width <= 0 || h.height <= 0 {
		return v
	}
	canvas := uv.NewScreenBuffer(h.width, h.height)
	side := h.sideWidth()
	fill := uv.EmptyCell
	fill.Style.Bg = p.bg
	canvas.FillArea(&fill, image.Rect(0, 0, side, h.height))
	if h.listOpen() {
		h.drawList(canvas, p)
	} else {
		h.drawStrip(canvas, p)
	}
	divider := lipgloss.NewStyle().Foreground(p.border).Background(p.bg).Render("│")
	for y := range h.height {
		draw(canvas, side-1, y, 1, divider)
	}

	area := h.sessionArea()
	s := h.current()
	if s != nil {
		s.emu.Draw(canvas, area)
		v.WindowTitle = "crush · " + sessionTitle(s.snapshot())
	}
	if h.confirm != nil {
		h.drawConfirm(canvas, area, p)
	} else if s != nil {
		if visible, shape, blink := s.cursorState(); visible {
			pos := s.emu.CursorPosition()
			c := tea.NewCursor(pos.X+side, pos.Y)
			c.Shape, c.Blink = cursorShape(shape), blink
			v.Cursor = c
		}
	}
	v.Content = strings.ReplaceAll(canvas.Render(), "\r\n", "\n")
	return v
}

func cursorShape(s vt.CursorStyle) tea.CursorShape {
	switch s {
	case vt.CursorUnderline:
		return tea.CursorUnderline
	case vt.CursorBar:
		return tea.CursorBar
	}
	return tea.CursorBlock
}

// draw puts already styled text at x, y, cut to width cells.
func draw(scr uv.Screen, x, y, width int, text string) {
	uv.NewStyledString(text).Draw(scr, image.Rect(x, y, x+width, y+1))
}

// spread lays left and right out across width cells, cutting left first.
// The gap between them takes fill's background, so a row's color reaches
// across it.
func spread(fill lipgloss.Style, left, right string, width int) string {
	room := width - lipgloss.Width(right)
	if room < 1 {
		return ansi.Truncate(right, width, "")
	}
	left = ansi.Truncate(left, room-1, "…")
	return left + fill.Render(strings.Repeat(" ", room-lipgloss.Width(left))) + right
}

func sessionTitle(st Status) string {
	if st.Title == "" {
		return "New session"
	}
	return st.Title
}

// sessionState returns the words and color that say what s is doing.
func (h *Host) sessionState(s *session, p palette) (label string, mark string, c color.Color) {
	s.mu.Lock()
	stopping := s.stopped
	s.mu.Unlock()
	st := s.snapshot()
	switch {
	case stopping:
		return "Closing…", "×", p.muted
	case st.State == StateWaiting:
		return "! Needs you", "!", p.waiting
	case st.State == StateWorking:
		return "● Working", "●", p.working
	case s.unread:
		return "✓ Finished", "✓", p.accent
	case st.State == StateReady:
		return "Ready", "○", p.muted
	}
	return "Starting…", "·", p.muted
}

func (h *Host) drawList(scr uv.Screen, p palette) {
	inner := listWidth - 1
	base := lipgloss.NewStyle().Background(p.bg)
	text := base.Foreground(p.text)
	muted := base.Foreground(p.muted)
	accent := base.Foreground(p.accent)

	header := spread(base, text.Render(" Sessions")+muted.Render("  "+strconv.Itoa(len(h.sessions))), accent.Render("‹ "), inner)
	draw(scr, 0, 0, inner, header)
	draw(scr, 0, 1, inner, accent.Render(" + New session"))

	first, count := h.visibleRange(listBlockHeight)
	for k := range count {
		i := first + k
		h.drawListBlock(scr, i, firstSessionRow+k*listBlockHeight, inner, p)
	}

	hint := muted.Render(" alt+s hide · alt+w close")
	draw(scr, 0, h.height-1, inner, hint)
	if h.notice != "" {
		draw(scr, 0, h.height-2, inner, base.Foreground(p.waiting).Render(" "+h.notice))
	}
}

func (h *Host) drawListBlock(scr uv.Screen, i, y, width int, p palette) {
	s := h.sessions[i]
	st := s.snapshot()
	bg := p.bg
	shown := i == h.active
	if shown {
		bg = p.selected
	}
	base := lipgloss.NewStyle().Background(bg)
	bar := base.Render(" ")
	if shown {
		bar = base.Foreground(p.accent).Render("▌")
	}
	closeMark := ""
	if shown || i == h.hover {
		closeMark = base.Foreground(p.text).Render("× ")
	}
	label, _, labelColor := h.sessionState(s, p)
	right := ""
	switch {
	case shown:
		right = "Viewing"
	case s.unread:
		right = "Unread"
	}
	dir := st.Dir
	if dir != "" {
		dir = home.Short(dir)
	}
	lines := []string{
		spread(base, base.Foreground(p.text).Bold(true).Render(sessionTitle(st)), closeMark, width-2),
		spread(base, base.Foreground(p.muted).Render(dir), "", width-2),
		spread(base, base.Foreground(p.subtle).Render(st.Model), "", width-2),
		spread(base, base.Foreground(labelColor).Render(label), base.Foreground(p.muted).Render(right+" "), width-2),
	}
	for n, line := range lines {
		draw(scr, 0, y+n, width, bar+base.Render(" ")+line)
	}
}

func (h *Host) drawStrip(scr uv.Screen, p palette) {
	inner := stripWidth - 1
	base := lipgloss.NewStyle().Background(p.bg)
	accent := base.Foreground(p.accent)
	draw(scr, 0, 0, inner, accent.Render(" › "))
	draw(scr, 0, 1, inner, accent.Render(" + "))
	first, count := h.visibleRange(stripBlockHeight)
	for k := range count {
		i := first + k
		s := h.sessions[i]
		y := firstSessionRow + k*stripBlockHeight
		bg := p.bg
		if i == h.active {
			bg = p.selected
		}
		row := lipgloss.NewStyle().Background(bg)
		bar := row.Render(" ")
		if i == h.active {
			bar = row.Foreground(p.accent).Render("▌")
		}
		_, mark, c := h.sessionState(s, p)
		number := strconv.Itoa(i + 1)
		draw(scr, 0, y, inner, bar+row.Foreground(c).Render(mark)+row.Render(" "))
		draw(scr, 0, y+1, inner, bar+row.Foreground(p.text).Render(ansi.Truncate(number, 2, ""))+row.Render(strings.Repeat(" ", max(2-len(number), 0))))
	}
}

// confirmDetail is the close box's second line: what closing would cut
// short, and what is kept.
func confirmDetail(st Status) string {
	detail := ""
	switch st.State {
	case StateWorking:
		detail = "It is still working. "
	case StateWaiting:
		detail = "It is waiting for your answer. "
	}
	return detail + "The chat stays saved and can be reopened later."
}

// confirmBox returns where the close box goes over area, and where its
// Close and Keep buttons are. Inside its border it holds the title, the
// detail, a blank line and the buttons.
func (h *Host) confirmBox(area image.Rectangle, detail string) (box, closeButton, keepButton image.Rectangle) {
	width := min(52, area.Dx()-2)
	detailLines := lipgloss.Height(lipgloss.NewStyle().Width(width - 4).Render(detail))
	height := 2 + 1 + detailLines + 1 + 1
	x := area.Min.X + (area.Dx()-width)/2
	y := area.Min.Y + (area.Dy()-height)/2
	box = image.Rect(x, y, x+width, y+height)
	const closeW, keepW, gap = 7, 6, 2
	bx := x + (width-closeW-gap-keepW)/2
	by := y + height - 2
	closeButton = image.Rect(bx, by, bx+closeW, by+1)
	keepButton = image.Rect(bx+closeW+gap, by, bx+closeW+gap+keepW, by+1)
	return box, closeButton, keepButton
}

func (h *Host) drawConfirm(scr uv.Screen, area image.Rectangle, p palette) {
	s := h.byID(h.confirm.id)
	if s == nil {
		return
	}
	st := s.snapshot()
	detail := confirmDetail(st)
	box, closeButton, keepButton := h.confirmBox(area, detail)
	inner := box.Dx() - 4
	base := lipgloss.NewStyle().Background(p.bg)
	body := lipgloss.JoinVertical(lipgloss.Left,
		base.Foreground(p.text).Bold(true).Width(inner).Render(ansi.Truncate("Close “"+sessionTitle(st)+"”?", inner, "…")),
		base.Foreground(p.muted).Width(inner).Render(detail),
	)
	frame := base.Border(lipgloss.RoundedBorder()).BorderForeground(p.accent).BorderBackground(p.bg).
		Padding(0, 1).Width(box.Dx()).Height(box.Dy())
	uv.NewStyledString(frame.Render(body)).Draw(scr, box)

	focused, blurred := h.styles.Button.Focused, h.styles.Button.Blurred
	closeStyle, keepStyle := focused, blurred
	if h.confirm.keep {
		closeStyle, keepStyle = blurred, focused
	}
	draw(scr, closeButton.Min.X, closeButton.Min.Y, closeButton.Dx(), closeStyle.Render(" Close "))
	draw(scr, keepButton.Min.X, keepButton.Min.Y, keepButton.Dx(), keepStyle.Render(" Keep "))
}

// sessionArea is where the shown session is drawn.
func (h *Host) sessionArea() image.Rectangle {
	return image.Rect(h.sideWidth(), 0, h.width, h.height)
}

func (h *Host) clickConfirm(x, y int) {
	s := h.byID(h.confirm.id)
	if s == nil {
		return
	}
	_, closeButton, keepButton := h.confirmBox(h.sessionArea(), confirmDetail(s.snapshot()))
	pt := image.Pt(x, y)
	switch {
	case pt.In(closeButton):
		h.answerClose(true)
	case pt.In(keepButton):
		h.answerClose(false)
	}
}
