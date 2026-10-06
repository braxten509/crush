package dialog

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ChangesID is the identifier for the changes overlay.
const ChangesID = "changes"

const (
	// changesMaxWidth and changesMargin size the sheet: up to
	// changesMaxWidth columns, leaving changesMargin of the chat showing.
	changesMaxWidth = 148
	changesMargin   = 6
	// changesFoldOver is how many diff lines make a file start folded.
	changesFoldOver = 600
	changesTabWidth = 4
)

// Changes is a sheet over the right side of the window that reviews the
// file changes of one tool group: a list of the changed files, then each
// file's diff with line numbers. Laid out after the Forecaster bug bench's
// diff view; driven with the mouse (wheel, click a file to jump to it,
// click a file's header to fold it, drag the scrollbar) or the keyboard.
type Changes struct {
	com    *common.Common
	title  string
	files  []diffreview.File
	folded []bool
	pal    changesPalette

	offset int
	rows   []changesRow
	// headRow is the row of each file's header in rows.
	headRow []int
	built   int // body width rows were built for

	// Layout of the last Draw, for mouse hits.
	panel, body image.Rectangle
	closeX      int
	dragging    bool

	keyMap struct {
		Close, Up, Down, PageUp, PageDown, Top, Bottom, Next, Prev key.Binding
	}
}

type changesRow struct {
	text string
	// file is the file whose box the row belongs to, or -1; nav is the
	// file a row of the file list jumps to, or -1; head marks a file's
	// header row.
	file, nav int
	head      bool
}

var _ Dialog = (*Changes)(nil)

// NewChanges creates the changes overlay for files; title says what made
// them ("3 actions").
func NewChanges(com *common.Common, title string, files []diffreview.File) *Changes {
	c := &Changes{com: com, title: title, files: files, folded: make([]bool, len(files))}
	for i, f := range files {
		c.folded[i] = len(f.Lines) > changesFoldOver
	}
	c.pal = benchPalette(isDark(com.Styles.Background))
	c.keyMap.Close = key.NewBinding(key.WithKeys("esc", "alt+esc", "q"), key.WithHelp("esc", "close"))
	c.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"))
	c.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"))
	c.keyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "b", "shift+space"))
	c.keyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "space", "f"))
	c.keyMap.Top = key.NewBinding(key.WithKeys("home", "g"))
	c.keyMap.Bottom = key.NewBinding(key.WithKeys("end", "G"))
	c.keyMap.Next = key.NewBinding(key.WithKeys("n", "]", "tab"))
	c.keyMap.Prev = key.NewBinding(key.WithKeys("p", "[", "shift+tab"))
	return c
}

// ID implements Dialog.
func (c *Changes) ID() string { return ChangesID }

// HandleMsg implements Dialog.
func (c *Changes) HandleMsg(msg tea.Msg) Action {
	page := max(1, c.body.Dy()-1)
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, c.keyMap.Up):
			c.scroll(-1)
		case key.Matches(msg, c.keyMap.Down):
			c.scroll(1)
		case key.Matches(msg, c.keyMap.PageUp):
			c.scroll(-page)
		case key.Matches(msg, c.keyMap.PageDown):
			c.scroll(page)
		case key.Matches(msg, c.keyMap.Top):
			c.offset = 0
		case key.Matches(msg, c.keyMap.Bottom):
			c.offset = c.maxOffset()
		case key.Matches(msg, c.keyMap.Next):
			c.jumpFile(1)
		case key.Matches(msg, c.keyMap.Prev):
			c.jumpFile(-1)
		}
	case common.CoalescedWheelMsg:
		c.scroll(int(msg.DeltaY))
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		return c.click(msg.X, msg.Y)
	case tea.MouseMotionMsg:
		if c.dragging {
			c.dragTo(msg.Y)
		}
	case tea.MouseReleaseMsg:
		c.dragging = false
	}
	return nil
}

func (c *Changes) click(x, y int) Action {
	pt := image.Pt(x, y)
	if !pt.In(c.panel) {
		return ActionClose{}
	}
	if y == c.panel.Min.Y+1 && x >= c.closeX-1 && x <= c.closeX+1 {
		return ActionClose{}
	}
	if !pt.In(c.body) {
		if x >= c.body.Max.X && y >= c.body.Min.Y && y < c.body.Max.Y {
			c.dragging = true
			c.dragTo(y)
		}
		return nil
	}
	row, sticky := c.rowAt(y - c.body.Min.Y)
	if row < 0 {
		return nil
	}
	r := c.rows[row]
	switch {
	case r.nav >= 0:
		c.showFile(r.nav)
	case r.head:
		c.folded[r.file] = !c.folded[r.file]
		c.built = 0
		c.layout(c.body.Dx())
		if sticky {
			c.showFile(r.file)
		}
	}
	return nil
}

// rowAt returns the row drawn at body line y, and whether it is the file
// header pinned to the top.
func (c *Changes) rowAt(y int) (int, bool) {
	if y == 0 {
		if h := c.stickyHead(); h >= 0 {
			return h, true
		}
	}
	if i := c.offset + y; i < len(c.rows) {
		return i, false
	}
	return -1, false
}

// stickyHead returns the header row to pin at the top while scrolled
// into a file's diff, or -1.
func (c *Changes) stickyHead() int {
	if c.offset >= len(c.rows) {
		return -1
	}
	f := c.rows[c.offset].file
	if f < 0 || c.offset <= c.headRow[f] {
		return -1
	}
	return c.headRow[f]
}

func (c *Changes) dragTo(y int) {
	h := c.body.Dy()
	if h <= 1 {
		return
	}
	c.offset = (y - c.body.Min.Y) * c.maxOffset() / (h - 1)
	c.clamp()
}

func (c *Changes) scroll(n int) {
	c.offset += n
	c.clamp()
}

func (c *Changes) maxOffset() int {
	return max(0, len(c.rows)-c.body.Dy())
}

func (c *Changes) clamp() {
	c.offset = max(0, min(c.offset, c.maxOffset()))
}

// showFile scrolls a file's box to the top.
func (c *Changes) showFile(i int) {
	c.offset = c.headRow[i] - 1
	c.clamp()
}

// jumpFile scrolls to the next (dir 1) or previous (dir -1) file.
func (c *Changes) jumpFile(dir int) {
	top := c.offset + 1
	if dir > 0 {
		for i, h := range c.headRow {
			if h > top {
				c.showFile(i)
				return
			}
		}
		return
	}
	for i := len(c.headRow) - 1; i >= 0; i-- {
		if c.headRow[i] < top {
			c.showFile(i)
			return
		}
	}
	c.offset = 0
}

// Draw implements Dialog.
func (c *Changes) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	p := c.pal
	width := min(area.Dx(), max(min(changesMaxWidth, area.Dx()-changesMargin), 40))
	c.panel = image.Rect(area.Max.X-width, area.Min.Y, area.Max.X, area.Max.Y)
	// Left border, two columns of padding, then the body; one column of
	// padding and the scrollbar on the right.
	// Keep padding below the footer, matching the blank row above the
	// title. Terminal windows can clip the last row at fractional cell sizes.
	const head, foot = 4, 3
	c.body = image.Rect(c.panel.Min.X+3, c.panel.Min.Y+head, c.panel.Max.X-2, c.panel.Max.Y-foot)
	if c.body.Dy() < 1 || c.body.Dx() < 20 {
		return nil
	}
	c.layout(c.body.Dx())
	c.clamp()

	bg := lipgloss.NewStyle().Background(p.surface)
	border := bg.Foreground(p.lineStrong).Render("│")
	line := func(y int, content string) {
		pad := max(0, width-1-ansi.StringWidth(content))
		uv.NewStyledString(border+content+bg.Render(strings.Repeat(" ", pad))).
			Draw(scr, uv.Rect(c.panel.Min.X, y, width, 1))
	}

	// Header: title and close button, what changed, a rule.
	y := c.panel.Min.Y
	line(y, "")
	adds, dels := diffreview.Stats(c.files)
	title := bg.Foreground(p.text).Bold(true).Render("Changes")
	c.closeX = c.panel.Max.X - 3
	closeBtn := bg.Foreground(p.text2).Render("✕")
	gap := max(1, width-1-2-lipgloss.Width(title)-3)
	line(y+1, bg.Render("  ")+title+bg.Render(strings.Repeat(" ", gap))+closeBtn)
	sub := bg.Foreground(p.text3).Render(c.title + " · " + countNoun(len(c.files), "file"))
	if diffreview.AnyCounted(c.files) {
		sub += bg.Foreground(p.text3).Render(" · ") +
			bg.Foreground(p.addText).Render(fmt.Sprintf("+%d", adds)) + bg.Render(" ") +
			bg.Foreground(p.delText).Render(fmt.Sprintf("−%d", dels))
	}
	line(y+2, bg.Render("  ")+sub)
	line(y+3, bg.Foreground(p.line).Render(strings.Repeat("─", width-1)))

	// Body with the scrollbar.
	bodyH := c.body.Dy()
	thumb, thumbLen := scrollThumb(bodyH, len(c.rows), c.offset)
	sticky := c.stickyHead()
	for i := range bodyH {
		row := ""
		if i == 0 && sticky >= 0 {
			row = c.rows[sticky].text
		} else if j := c.offset + i; j < len(c.rows) {
			row = c.rows[j].text
		}
		row += bg.Render(strings.Repeat(" ", max(0, c.body.Dx()-ansi.StringWidth(row))))
		bar := bg.Render(" ")
		if thumbLen > 0 && i >= thumb && i < thumb+thumbLen {
			bar = bg.Foreground(p.lineStrong).Render("┃")
		}
		line(c.body.Min.Y+i, bg.Render("  ")+row+bg.Render(" ")+bar)
	}

	// Footer: the keys.
	fy := c.body.Max.Y
	line(fy, bg.Foreground(p.line).Render(strings.Repeat("─", width-1)))
	keys := "wheel/↑↓ scroll · click a file to jump · click its header to fold · n/p next/prev file · esc close"
	if ansi.StringWidth(keys) > width-4 {
		keys = "↑↓/wheel scroll · click file: jump · click header: fold · n/p file · esc close"
	}
	if ansi.StringWidth(keys) > width-4 {
		keys = "↑↓ scroll · n/p file · esc close"
	}
	line(fy+1, bg.Render("  ")+bg.Foreground(p.text3).Render(keys))
	line(fy+2, "")
	return nil
}

// scrollThumb places the scrollbar thumb, or returns length 0 when
// everything fits.
func scrollThumb(height, total, offset int) (start, length int) {
	if total <= height || height <= 0 {
		return 0, 0
	}
	length = max(1, height*height/total)
	maxOff := total - height
	start = (height - length) * offset / maxOff
	return start, length
}

// layout builds the body rows for width.
func (c *Changes) layout(width int) {
	if c.built == width && c.rows != nil {
		return
	}
	c.built = width
	p := c.pal
	bg := lipgloss.NewStyle().Background(p.surface)
	c.rows = c.rows[:0]
	c.headRow = make([]int, len(c.files))
	add := func(text string, file, nav int, head bool) {
		c.rows = append(c.rows, changesRow{text: text, file: file, nav: nav, head: head})
	}
	adds, dels := diffreview.Stats(c.files)

	// "2 files changed +12 −3"
	summary := bg.Foreground(p.text).Bold(true).Render(diffreview.Summary(c.files))
	if diffreview.AnyCounted(c.files) {
		summary += bg.Render("  ") + c.stat(bg, adds, dels)
	}
	add(summary, -1, -1, false)
	add("", -1, -1, false)

	// The file list.
	edge := bg.Foreground(p.line)
	inner := width - 2
	add(edge.Render("╭"+strings.Repeat("─", inner)+"╮"), -1, -1, false)
	for i, f := range c.files {
		dot := bg.Foreground(c.kindColor(f.Kind)).Render("■")
		stat := c.stat(bg, f.Adds, f.Dels)
		if !f.HasEdits() || !f.Counted() {
			stat = bg.Foreground(p.text3).Render(f.Kind.String())
		}
		room := inner - 2 - 2 - lipgloss.Width(stat) - 2
		path := bg.Foreground(p.text).Render(truncateLeft(c.shortPath(f.Path), room))
		gap := max(1, inner-2-lipgloss.Width(dot)-1-lipgloss.Width(path)-lipgloss.Width(stat))
		add(edge.Render("│")+bg.Render(" ")+dot+bg.Render(" ")+path+bg.Render(strings.Repeat(" ", gap))+stat+
			bg.Render(" ")+edge.Render("│"), -1, i, false)
	}
	add(edge.Render("╰"+strings.Repeat("─", inner)+"╯"), -1, -1, false)
	add("", -1, -1, false)

	// One box per file.
	for i, f := range c.files {
		add(edge.Render("╭"+strings.Repeat("─", inner)+"╮"), i, -1, false)
		c.headRow[i] = len(c.rows)
		add(c.fileHead(f, i, inner), i, -1, true)
		if !c.folded[i] {
			add(edge.Render("├"+strings.Repeat("─", inner)+"┤"), i, -1, false)
			for _, l := range c.diffRows(f, inner) {
				add(edge.Render("│")+l+edge.Render("│"), i, -1, false)
			}
		}
		add(edge.Render("╰"+strings.Repeat("─", inner)+"╯"), i, -1, false)
		add("", -1, -1, false)
	}
}

func (c *Changes) stat(bg lipgloss.Style, adds, dels int) string {
	return bg.Foreground(c.pal.addText).Bold(true).Render(fmt.Sprintf("+%d", adds)) + bg.Render(" ") +
		bg.Foreground(c.pal.delText).Bold(true).Render(fmt.Sprintf("−%d", dels))
}

// fileHead is a file's header row: fold chevron, kind badge, path, counts.
func (c *Changes) fileHead(f diffreview.File, i, inner int) string {
	p := c.pal
	edge := lipgloss.NewStyle().Background(p.surface).Foreground(p.line)
	hb := lipgloss.NewStyle().Background(p.surface2)
	chev := "▾"
	if c.folded[i] {
		chev = "▸"
	}
	badgeBg, badgeFg := p.surface3, p.text2
	switch f.Kind {
	case diffreview.Added:
		badgeBg, badgeFg = p.addBg, p.addText
	case diffreview.Deleted:
		badgeBg, badgeFg = p.delBg, p.delText
	}
	left := hb.Foreground(p.text3).Render(" "+chev+" ") +
		lipgloss.NewStyle().Background(badgeBg).Foreground(badgeFg).Bold(true).Render(" "+f.Kind.String()+" ") +
		hb.Render(" ")
	// The badge already says what happened to a file without counts.
	stat := hb.Render(" ")
	if f.Counted() {
		stat = c.stat(hb, f.Adds, f.Dels) + stat
	}
	room := inner - lipgloss.Width(left) - lipgloss.Width(stat) - 1
	path := hb.Foreground(p.text).Bold(true).Render(truncateLeft(c.shortPath(f.Path), room))
	gap := max(1, inner-lipgloss.Width(left)-lipgloss.Width(path)-lipgloss.Width(stat))
	return edge.Render("│") + left + path + hb.Render(strings.Repeat(" ", gap)) + stat + edge.Render("│")
}

// diffRows renders a file's diff lines inner columns wide: old and new
// line numbers, the sign, then the code, wrapped.
func (c *Changes) diffRows(f diffreview.File, inner int) []string {
	p := c.pal
	maxNo := 0
	for _, l := range f.Lines {
		maxNo = max(maxNo, l.Old, l.New)
	}
	digits := max(3, len(strconv.Itoa(maxNo)))
	lnW := digits + 2
	// The sign column is " + ", so code sits a space away from it.
	codeW := max(8, inner-2*lnW-3)
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", lnW)
		}
		return fmt.Sprintf(" %*d ", digits, n)
	}
	var out []string
	for _, l := range f.Lines {
		if l.Kind == diffreview.Hunk {
			hs := lipgloss.NewStyle().Background(p.hunkBg).Foreground(p.text3)
			text := ansi.Truncate(" "+l.Text, codeW+2, "…")
			out = append(out, hs.Render(strings.Repeat(" ", 2*lnW+1)+text+
				strings.Repeat(" ", max(0, codeW+2-ansi.StringWidth(text)))))
			continue
		}
		lnBg, lnFg, codeBg, signFg, sign := p.surface2, p.text3, p.surface, p.text3, " "
		switch l.Kind {
		case diffreview.Add:
			lnBg, lnFg, codeBg, signFg, sign = p.addLn, p.addText, p.addBg, p.addText, "+"
		case diffreview.Del:
			lnBg, lnFg, codeBg, signFg, sign = p.delLn, p.delText, p.delBg, p.delText, "−"
		}
		ln := lipgloss.NewStyle().Background(lnBg).Foreground(lnFg)
		cs := lipgloss.NewStyle().Background(codeBg)
		for j, part := range wrapCode(l.Text, codeW) {
			oldNo, newNo, sg := num(l.Old), num(l.New), sign
			if j > 0 {
				oldNo, newNo, sg = num(0), num(0), " "
			}
			out = append(out, ln.Render(oldNo)+ln.Render(newNo)+cs.Foreground(signFg).Render(" "+sg+" ")+
				cs.Foreground(p.text).Render(part+strings.Repeat(" ", max(0, codeW-ansi.StringWidth(part)))))
		}
	}
	return out
}

// wrapCode expands tabs, drops control characters, and wraps text to width
// columns, at spaces where it can.
func wrapCode(text string, width int) []string {
	var sb strings.Builder
	col := 0
	for _, r := range text {
		switch {
		case r == '\t':
			n := changesTabWidth - col%changesTabWidth
			sb.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f:
		default:
			sb.WriteRune(r)
			col++
		}
	}
	s := sb.String()
	if ansi.StringWidth(s) <= width {
		return []string{s}
	}
	return strings.Split(ansi.Wrap(s, width, ""), "\n")
}

// shortPath shows a path relative to the working directory when it is
// inside it.
func (c *Changes) shortPath(path string) string {
	if c.com.Workspace != nil {
		if rel, err := filepath.Rel(c.com.Workspace.WorkingDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return fsext.PrettyPath(path)
}

func (c *Changes) kindColor(k diffreview.Kind) color.Color {
	switch k {
	case diffreview.Added:
		return c.pal.addText
	case diffreview.Deleted:
		return c.pal.delText
	}
	return c.pal.text3
}

// truncateLeft keeps the end of s (the file name) when it is too wide.
func truncateLeft(s string, width int) string {
	if width <= 1 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-width+1, "")
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// changesPalette holds the bench's colors.
type changesPalette struct {
	surface, surface2, surface3, line, lineStrong color.Color
	text, text2, text3                            color.Color
	addBg, addLn, addText, delBg, delLn, delText  color.Color
	hunkBg                                        color.Color
}

// benchPalette is the Forecaster bug bench's diff palette (its oklch
// colors in sRGB), dark or light to suit the theme.
func benchPalette(dark bool) changesPalette {
	c := lipgloss.Color
	if dark {
		return changesPalette{
			surface: c("#14171d"), surface2: c("#1b1e24"), surface3: c("#23272d"),
			line: c("#2a2e35"), lineStrong: c("#3b4048"),
			text: c("#e5e8ed"), text2: c("#b0b4bc"), text3: c("#8e929b"),
			addBg: c("#142d1b"), addLn: c("#163620"), addText: c("#79cd91"),
			delBg: c("#3a1d1c"), delLn: c("#4a2322"), delText: c("#fb9890"),
			hunkBg: c("#1b222e"),
		}
	}
	return changesPalette{
		surface: c("#ffffff"), surface2: c("#f0f2f7"), surface3: c("#e7eaef"),
		line: c("#dde0e5"), lineStrong: c("#c6cbd3"),
		text: c("#1c222b"), text2: c("#4d535d"), text3: c("#676c75"),
		addBg: c("#e1f9e6"), addLn: c("#cdefd5"), addText: c("#006731"),
		delBg: c("#ffecea"), delLn: c("#ffdcda"), delText: c("#b02a2d"),
		hunkBg: c("#eaf0fd"),
	}
}

// isDark reports whether a theme background is dark (unknown counts as
// dark, Crush's default).
func isDark(bg color.Color) bool {
	if bg == nil {
		return true
	}
	r, g, b, _ := bg.RGBA()
	return 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) < 0.5*0xffff
}
