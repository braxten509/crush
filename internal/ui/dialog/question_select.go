package dialog

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

// OpenLinkMsg asks the UI to open a link the user clicked in an inline
// editor. URL is a Markdown link destination, a web address or a local path.
type OpenLinkMsg struct{ URL string }

// formSelection lets the user drag across a question form to select and
// copy its text, and click links in it. It works on the cells the form drew
// last frame, so it covers every question type and wrapped text alike.
//
// A press inside the form is held until release: a release without moving
// opens the link under the pointer or runs the normal click, and a drag
// selects text instead, so selecting a choice's words never picks it.
type formSelection struct {
	area    image.Rectangle // where the form drew last frame
	cells   [][]uv.Cell     // the drawn cells, before any highlight
	pressed bool
	dragged bool
	anchor  image.Point // screen position of the press
	head    image.Point // screen position the drag reached
	shown   bool        // keep the highlight after the drag ends
}

// clear drops any selection highlight.
func (s *formSelection) clear() {
	s.pressed = false
	s.dragged = false
	s.shown = false
}

// active reports whether a highlight should be painted.
func (s *formSelection) active() bool {
	return (s.pressed && s.dragged) || s.shown
}

// capture remembers the cells drawn in area and paints the selection
// highlight over them.
func (s *formSelection) capture(scr uv.Screen, area image.Rectangle, highlight lipgloss.Style) {
	area = area.Intersect(scr.Bounds())
	if area != s.area {
		// The form moved or resized; old positions no longer match.
		s.clear()
	}
	s.area = area
	s.cells = s.cells[:0]
	for y := area.Min.Y; y < area.Max.Y; y++ {
		row := make([]uv.Cell, area.Dx())
		for x := area.Min.X; x < area.Max.X; x++ {
			if c := scr.CellAt(x, y); c != nil {
				row[x-area.Min.X] = *c
			} else {
				row[x-area.Min.X] = uv.EmptyCell
			}
		}
		s.cells = append(s.cells, row)
	}
	if !s.active() {
		return
	}
	start, end := s.ordered()
	style := list.ToStyle(highlight)
	for y := start.Y; y <= end.Y; y++ {
		x0, x1 := s.rowSpan(y, start, end)
		for x := x0; x <= x1; x++ {
			c := s.cellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			lit := *c
			lit.Style = style
			scr.SetCell(x, y, &lit)
		}
	}
}

// cellAt returns the captured cell at a screen position, or nil.
func (s *formSelection) cellAt(x, y int) *uv.Cell {
	if !image.Pt(x, y).In(s.area) {
		return nil
	}
	return &s.cells[y-s.area.Min.Y][x-s.area.Min.X]
}

// ordered returns the selection ends in reading order.
func (s *formSelection) ordered() (image.Point, image.Point) {
	a, b := s.anchor, s.head
	if b.Y < a.Y || (b.Y == a.Y && b.X < a.X) {
		a, b = b, a
	}
	return a, b
}

// rowSpan returns the inclusive columns selected on row y, like a terminal:
// the first row from the start, the last row up to the end, full rows between.
func (s *formSelection) rowSpan(y int, start, end image.Point) (int, int) {
	x0, x1 := s.area.Min.X, s.area.Max.X-1
	if y == start.Y {
		x0 = start.X
	}
	if y == end.Y {
		x1 = end.X
	}
	return x0, x1
}

// clamp keeps a pointer position inside the drawn area so a drag past the
// edge selects to the edge.
func (s *formSelection) clamp(p image.Point) image.Point {
	p.X = min(max(p.X, s.area.Min.X), s.area.Max.X-1)
	p.Y = min(max(p.Y, s.area.Min.Y), s.area.Max.Y-1)
	return p
}

// text returns the selected text without the form's decorations: the gutter
// bar, the "?" tag, answer box padding and the scrollbar.
func (s *formSelection) text(sty *styles.Styles) string {
	if !s.active() {
		return ""
	}
	tagBgs := []color.Color{
		sty.Editor.PromptQuestionIconFocused.GetBackground(),
		sty.Editor.PromptQuestionIconBlurred.GetBackground(),
	}
	start, end := s.ordered()
	var lines []string
	for y := start.Y; y <= end.Y; y++ {
		x0, x1 := s.rowSpan(y, start, end)
		var b strings.Builder
		for x := x0; x <= x1; x++ {
			c := s.cellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			content := c.Content
			switch {
			case content == "":
				content = " "
			case x-s.area.Min.X < questionBarWidth && content == "┃",
				x == s.area.Max.X-1 && (content == styles.ScrollbarThumb || content == styles.ScrollbarTrack),
				content == "▄" || content == "▀":
				content = " "
			case sameColor(c.Style.Bg, tagBgs...):
				// The "?" tag is a label, not text.
				continue
			}
			b.WriteString(content)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	// Drop blank rows at either end, then the shared indentation of the
	// rest. The first row starts where the drag began, so it trims alone.
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	lines[0] = strings.TrimLeft(lines[0], " ")
	indent := -1
	for _, l := range lines[1:] {
		if l == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i := 1; i < len(lines); i++ {
		if len(lines[i]) >= indent && indent > 0 {
			lines[i] = lines[i][indent:]
		}
	}
	return strings.Join(lines, "\n")
}

// sameColor reports whether c matches any of the given colors.
func sameColor(c color.Color, others ...color.Color) bool {
	if c == nil {
		return false
	}
	r, g, b, a := c.RGBA()
	for _, o := range others {
		if o == nil {
			continue
		}
		or, og, ob, oa := o.RGBA()
		if r == or && g == og && b == ob && a == oa {
			return true
		}
	}
	return false
}

// linkTrim is what surrounds a bare address in prose: quotes, brackets,
// backticks and sentence punctuation.
const linkTrim = "\"'`()[]{}<>,.;:!?"

var linkLineSuffix = regexp.MustCompile(`:[0-9]+(?::[0-9]+)?$`)

// linkAt returns the link under a screen position: a real hyperlink (from
// Markdown), or a bare web address or existing file path written as text.
func (s *formSelection) linkAt(p image.Point) string {
	c := s.cellAt(p.X, p.Y)
	if c == nil {
		return ""
	}
	if c.Link.URL != "" {
		return c.Link.URL
	}
	if c.Content == "" || c.Content == " " {
		return ""
	}
	// Grow the word under the pointer across non-blank cells.
	row := s.cells[p.Y-s.area.Min.Y]
	i := p.X - s.area.Min.X
	lo, hi := i, i
	for lo > 0 && row[lo-1].Content != " " && row[lo-1].Content != "" {
		lo--
	}
	for hi < len(row)-1 && row[hi+1].Content != " " && row[hi+1].Content != "" {
		hi++
	}
	var b strings.Builder
	for _, cell := range row[lo : hi+1] {
		b.WriteString(cell.Content)
	}
	return bareLink(strings.Trim(b.String(), linkTrim))
}

// bareLink accepts web addresses and absolute or home paths that exist.
// Anything else is ordinary text, so a click on it stays a normal click.
func bareLink(word string) string {
	lower := strings.ToLower(word)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		return word
	case strings.HasPrefix(word, "~/"), strings.HasPrefix(word, "/") && len(word) > 1:
	default:
		return ""
	}
	path := word
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, path[2:])
	}
	if _, err := os.Stat(path); err != nil {
		if _, err := os.Stat(linkLineSuffix.ReplaceAllString(path, "")); err != nil {
			return ""
		}
	}
	return word
}

// HandleMouseDown holds a press inside the form until release. It does not
// claim presses outside, so the chat and composer keep theirs.
func (f *QuestionForm) HandleMouseDown(x, y int) bool {
	p := image.Pt(x, y)
	if !p.In(f.sel.area) {
		f.sel.clear()
		return false
	}
	f.sel.clear()
	f.sel.pressed = true
	f.sel.anchor = p
	f.sel.head = p
	return true
}

// HandleMouseDrag extends the selection while the button is held.
func (f *QuestionForm) HandleMouseDrag(x, y int) bool {
	if !f.sel.pressed {
		return false
	}
	p := f.sel.clamp(image.Pt(x, y))
	if p != f.sel.anchor {
		f.sel.dragged = true
	}
	f.sel.head = p
	return true
}

// HandleMouseRelease copies a dragged selection, or treats a press without
// movement as a click: open the link under it, otherwise the normal action.
func (f *QuestionForm) HandleMouseRelease(x, y int) (bool, tea.Cmd) {
	if !f.sel.pressed {
		return false, nil
	}
	f.HandleMouseDrag(x, y)
	f.sel.pressed = false
	if f.sel.dragged {
		f.sel.shown = true
		text := f.sel.text(f.Styles)
		if text == "" {
			return true, nil
		}
		return true, common.CopyToClipboard(text, "Selected text copied to clipboard")
	}
	at := f.sel.anchor
	if link := f.sel.linkAt(at); link != "" {
		return true, func() tea.Msg { return OpenLinkMsg{URL: link} }
	}
	done, _ := f.HandleMouseClick(at.X, at.Y)
	f.releaseDone = done
	return true, nil
}

// TakeReleaseDone reports, once, that a click run on release finished the
// form, so the UI closes it like a direct click.
func (f *QuestionForm) TakeReleaseDone() bool {
	done := f.releaseDone
	f.releaseDone = false
	return done
}
