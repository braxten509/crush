package sessionhost

import (
	"image"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/home"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// The list's rows: a header with ‹, a new-session button, a blank row, then one block per
// session (two lines and a gap) and the key hint on the last row. The strip
// has ›, +, a blank row, then two lines and a gap per session.
const (
	listFirstRow     = 3
	stripFirstRow    = 3
	listBlockHeight  = 3
	stripBlockHeight = 3
)

// The new-session button and header's ‹ in the open list.
const (
	newSessionColumn = 1
	newSessionRow    = 1
	toggleColumn     = listWidth - 3
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

// firstRow is the row the first session starts on.
func (h *Host) firstRow() int {
	if h.listOpen() {
		return listFirstRow
	}
	return stripFirstRow
}

// visibleRange returns the sessions that fit, keeping the shown one in
// view.
func (h *Host) visibleRange(block int) (first, count int) {
	count = max((h.height-h.firstRow()-1)/block, 1)
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
	top := h.firstRow()
	switch {
	case open && y == 0:
		r.toggle = x >= toggleColumn-1
	case open && y == newSessionRow:
		r.newSession = x >= newSessionColumn && x < listWidth-2
	case y == 0:
		r.toggle = true
	case !open && y == 1:
		r.newSession = true
	case y >= top:
		first, count := h.visibleRange(block)
		k, line := (y-top)/block, (y-top)%block
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
	switch {
	case h.confirm != nil:
		h.drawConfirm(canvas, area, p)
	case h.picker != nil:
		v.Cursor = h.drawPicker(canvas, area, p)
	case s != nil:
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

// sessionState returns the mark and color that say what s is doing.
func (h *Host) sessionState(s *session, p palette) (mark string, c color.Color) {
	s.mu.Lock()
	stopping := s.stopped
	s.mu.Unlock()
	st := s.snapshot()
	switch {
	case stopping:
		return "×", p.muted
	case st.State == StateWaiting:
		return "!", p.waiting
	case st.State == StateWorking:
		return "●", p.working
	case s.unread:
		return "✓", p.accent
	case st.State == StateReady:
		return "○", p.muted
	}
	return "·", p.muted
}

func (h *Host) drawList(scr uv.Screen, p palette) {
	inner := listWidth - 1
	base := lipgloss.NewStyle().Background(p.bg)
	text := base.Foreground(p.text)
	muted := base.Foreground(p.muted)
	accent := base.Foreground(p.accent)

	header := spread(base, text.Bold(true).Render(" Sessions")+muted.Render(" ["+strconv.Itoa(len(h.sessions))+"]"),
		accent.Render("‹")+base.Render(" "), inner)
	draw(scr, 0, 0, inner, header)
	buttonWidth := inner - 2
	button := h.styles.Button.Blurred.Background(p.bg).Foreground(p.accent).Width(buttonWidth).Render(" + New session")
	draw(scr, newSessionColumn, newSessionRow, buttonWidth, button)

	first, count := h.visibleRange(listBlockHeight)
	for k := range count {
		i := first + k
		h.drawListBlock(scr, i, listFirstRow+k*listBlockHeight, inner, p)
	}

	draw(scr, 0, h.height-1, inner, muted.Render(" alt+n new · alt+s hide"))
	if h.notice != "" {
		draw(scr, 0, h.height-2, inner, base.Foreground(p.waiting).Render(" "+h.notice))
	}
}

// drawListBlock draws session i as two lines: its mark and title, then its
// folder's name.
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
	mark, markColor := h.sessionState(s, p)
	folder := ""
	if st.Dir != "" {
		folder = filepath.Base(home.Short(filepath.Clean(st.Dir)))
	}
	lines := []string{
		spread(base, base.Foreground(markColor).Render(mark)+base.Render(" ")+base.Foreground(p.text).Bold(true).Render(sessionTitle(st)), closeMark, width-1),
		spread(base, base.Render("  ")+base.Foreground(p.muted).Render(folder), "", width-1),
	}
	for n, line := range lines {
		draw(scr, 0, y+n, width, bar+line)
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
		y := stripFirstRow + k*stripBlockHeight
		bg := p.bg
		if i == h.active {
			bg = p.selected
		}
		row := lipgloss.NewStyle().Background(bg)
		bar := row.Render(" ")
		if i == h.active {
			bar = row.Foreground(p.accent).Render("▌")
		}
		mark, c := h.sessionState(s, p)
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

// The new-session box's lines inside its border: the title, the typed path,
// the matches, "Recent", the recent folders, a line for problems and the
// key hint. Its size stays the same while typing.
const (
	pickerInputLine  = 1
	pickerMatchLine  = 2
	pickerRecentHead = pickerMatchLine + pickerRows
	pickerRecentLine = pickerRecentHead + 1
	pickerErrLine    = pickerRecentLine + pickerRows
	pickerHintLine   = pickerErrLine + 1
	pickerLines      = pickerHintLine + 1
)

// pickerBox returns where the new-session box goes over area, and the part
// inside its border and padding.
func pickerBox(area image.Rectangle) (box, inner image.Rectangle) {
	width := min(72, area.Dx()-2)
	height := pickerLines + 2
	x := area.Min.X + (area.Dx()-width)/2
	y := area.Min.Y + max(min(3, area.Dy()-height), 0)
	box = image.Rect(x, y, x+width, y+height)
	return box, image.Rect(x+2, y+1, x+width-2, y+1+pickerLines)
}

// pickerRowAt returns the row of the box's rows at screen line y, or -1.
func (h *Host) pickerRowAt(inner image.Rectangle, x, y int) int {
	if x < inner.Min.X || x >= inner.Max.X {
		return -1
	}
	line := y - inner.Min.Y
	pk := h.picker
	switch {
	case line >= pickerMatchLine && line < pickerMatchLine+len(pk.matches):
		return line - pickerMatchLine
	case line >= pickerRecentLine && line < pickerRecentLine+len(pk.recent):
		return len(pk.matches) + line - pickerRecentLine
	}
	return -1
}

// drawPicker draws the new-session box and returns the cursor for its
// typed path.
func (h *Host) drawPicker(scr uv.Screen, area image.Rectangle, p palette) *tea.Cursor {
	pk := h.picker
	box, inner := pickerBox(area)
	base := lipgloss.NewStyle().Background(p.bg)
	frame := base.Border(lipgloss.RoundedBorder()).BorderForeground(p.accent).BorderBackground(p.bg).
		Width(box.Dx()).Height(box.Dy())
	uv.NewStyledString(frame.Render("")).Draw(scr, box)

	w := inner.Dx()
	line := func(n int, text string) {
		draw(scr, inner.Min.X, inner.Min.Y+n, w, text)
	}
	line(0, base.Foreground(p.text).Bold(true).Render("New session in…"))

	// The typed path, scrolled so the cursor stays in view.
	field := base.Background(p.selected).Foreground(p.text)
	room := max(w-2, 1)
	start := max(pk.cursor-room+1, 0)
	shown := string(pk.input[start:min(len(pk.input), start+room)])
	line(pickerInputLine, field.Render(" "+shown+strings.Repeat(" ", max(room-ansi.StringWidth(shown), 0))+" "))
	cursorX := inner.Min.X + 1 + ansi.StringWidth(string(pk.input[start:pk.cursor]))

	picked := lipgloss.NewStyle().
		Background(h.styles.Button.Focused.GetBackground()).
		Foreground(h.styles.Button.Focused.GetForeground())
	row := func(n, i int, dir string) {
		style := base.Foreground(p.text)
		if i == pk.sel {
			style = picked
		}
		text := " ▸ " + keepEnd(home.Short(dir), w-3)
		line(n, style.Render(text+strings.Repeat(" ", max(w-ansi.StringWidth(text), 0))))
	}
	for i, dir := range pk.matches {
		row(pickerMatchLine+i, i, dir)
	}
	if len(pk.recent) > 0 {
		line(pickerRecentHead, base.Foreground(p.muted).Render("Recent"))
	}
	for i, dir := range pk.recent {
		row(pickerRecentLine+i, len(pk.matches)+i, dir)
	}
	if pk.err != "" {
		line(pickerErrLine, base.Foreground(p.waiting).Render(keepEnd(pk.err, w)))
	}
	line(pickerHintLine, base.Foreground(p.muted).Render("↑↓ pick · tab open folder · enter start · esc cancel"))

	c := tea.NewCursor(cursorX, inner.Min.Y+pickerInputLine)
	c.Shape = tea.CursorBar
	return c
}

// keepEnd cuts text to width cells from the front, since a path's last
// folders say the most.
func keepEnd(text string, width int) string {
	over := ansi.StringWidth(text) - width
	if over <= 0 {
		return text
	}
	return "…" + ansi.TruncateLeft(text, over+1, "")
}

// clickPicker picks the clicked row, starts in it when it was already
// picked, and closes the box on a click outside it.
func (h *Host) clickPicker(x, y int) {
	box, inner := pickerBox(h.sessionArea())
	if !image.Pt(x, y).In(box) {
		h.picker = nil
		return
	}
	i := h.pickerRowAt(inner, x, y)
	if i < 0 {
		return
	}
	if i == h.picker.sel {
		h.startPicked()
		return
	}
	h.picker.sel = i
	h.picker.err = ""
}
