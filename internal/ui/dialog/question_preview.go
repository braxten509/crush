package dialog

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	_ "image/jpeg" // sketches may be JPEG
	"image/png"
	"math"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
)

// previewRows is the height of the sketch area under a choice list
// whose choices carry pictures. It stays the same for every choice,
// so moving between choices never shifts the form.
const previewRows = 16

// PreviewReadyMsg asks for another look at the form's sketch: a block
// picture got ready, or the form may have been drawn since the last
// placement.
type PreviewReadyMsg struct{}

// previewer draws choice sketches with the terminal's best picture
// support. With Kitty graphics the picture is placed over a blank area
// of the form at its screen position (terminals like Konsole draw Kitty
// pictures but not the placeholder text that would anchor them in the
// text); otherwise it is drawn with colored blocks.
type previewer struct {
	kitty bool
	cell  fimage.CellSize
	tmux  bool

	mu      sync.Mutex
	pending map[string]bool
	sizes   map[string]image.Point
}

func newPreviewer(caps *common.Capabilities) *previewer {
	p := &previewer{pending: map[string]bool{}, sizes: map[string]image.Point{}}
	if caps != nil {
		p.kitty = caps.SupportsKittyGraphics()
		p.cell.Width, p.cell.Height = caps.CellSize()
		_, p.tmux = caps.Env.LookupEnv("TMUX")
	}
	return p
}

// cellSize falls back to a typical cell when the terminal didn't say.
func (p *previewer) cellSize() (int, int) {
	if p.cell.Width <= 0 || p.cell.Height <= 0 {
		return 8, 16
	}
	return p.cell.Width, p.cell.Height
}

// imageID ties a picture to the file's contents, so a sketch redrawn
// under the same name isn't shown stale.
func imageID(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return path
	}
	return fmt.Sprintf("%s@%d", path, info.ModTime().UnixNano())
}

// fit returns the cell size the picture at path takes within maxCols
// by previewRows, keeping its shape. ok is false when it can't be read.
func (p *previewer) fit(path string, maxCols int) (cols, rows int, ok bool) {
	p.mu.Lock()
	size, known := p.sizes[path]
	p.mu.Unlock()
	if !known {
		f, err := os.Open(path)
		if err != nil {
			return 0, 0, false
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width == 0 || cfg.Height == 0 {
			return 0, 0, false
		}
		size = image.Pt(cfg.Width, cfg.Height)
		p.mu.Lock()
		p.sizes[path] = size
		p.mu.Unlock()
	}
	cw, ch := p.cellSize()
	rows = previewRows
	cols = int(math.Round(float64(rows*ch*size.X) / float64(size.Y*cw)))
	if cols > maxCols {
		cols = maxCols
		rows = max(1, int(math.Round(float64(cols*cw*size.Y)/float64(size.X*ch))))
	}
	return max(1, cols), rows, true
}

// blocks returns the picture drawn in colored blocks, or nil until it
// has been loaded.
func (p *previewer) blocks(path string, cols, rows int) []string {
	out := fimage.EncodingBlocks.Render(imageID(path), cols, rows)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// loadBlocksCmd loads the picture for block drawing once per file and
// size, then asks for a redraw.
func (p *previewer) loadBlocksCmd(path string, cols, rows int) tea.Cmd {
	id := imageID(path)
	if fimage.HasTransmitted(id, cols, rows) {
		return nil
	}
	key := fmt.Sprintf("%s-%dx%d", id, cols, rows)
	p.mu.Lock()
	if p.pending[key] {
		p.mu.Unlock()
		return nil
	}
	p.pending[key] = true
	p.mu.Unlock()
	return func() tea.Msg {
		defer func() {
			p.mu.Lock()
			delete(p.pending, key)
			p.mu.Unlock()
		}()
		img, err := decodeFile(path)
		if err != nil {
			return nil
		}
		if load := fimage.EncodingBlocks.Transmit(id, img, p.cell, cols, rows, p.tmux); load != nil {
			load()
		}
		return PreviewReadyMsg{}
	}
}

func decodeFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// PreviewPlacement is a sketch placed over the form at a screen
// position, in cells.
type PreviewPlacement struct {
	ID         string
	Path       string
	X, Y       int
	Cols, Rows int
}

func kittyImageID(id string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return int(h.Sum32()&0x7fffffff) | 1
}

// PreviewPlacementCmd moves the on-screen sketch from old to placed:
// it removes the old picture and puts the new one in its cells. Either
// may be nil.
func PreviewPlacementCmd(old, placed *PreviewPlacement, tmux bool) tea.Cmd {
	return func() tea.Msg {
		wrap := func(seq string) string {
			if tmux {
				return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
			}
			return seq
		}
		var out strings.Builder
		if old != nil {
			out.WriteString(wrap(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", kittyImageID(old.ID))))
		}
		if placed != nil {
			data, err := os.ReadFile(placed.Path)
			if err == nil && !bytes.HasPrefix(data, []byte("\x89PNG")) {
				var img image.Image
				if img, err = decodeFile(placed.Path); err == nil {
					var buf bytes.Buffer
					err = png.Encode(&buf, img)
					data = buf.Bytes()
				}
			}
			if err == nil {
				b64 := base64.StdEncoding.EncodeToString(data)
				// Save the cursor, put the picture at its cells without moving
				// the cursor (C=1), and restore it, so the screen drawing
				// carries on where it was.
				out.WriteString(fmt.Sprintf("\x1b7\x1b[%d;%dH", placed.Y+1, placed.X+1))
				for i := 0; i < len(b64); i += 4096 {
					chunk := b64[i:min(i+4096, len(b64))]
					more := 0
					if i+4096 < len(b64) {
						more = 1
					}
					if i == 0 {
						out.WriteString(wrap(fmt.Sprintf("\x1b_Ga=T,f=100,t=d,i=%d,c=%d,r=%d,C=1,q=2,m=%d;%s\x1b\\",
							kittyImageID(placed.ID), placed.Cols, placed.Rows, more, chunk)))
					} else {
						out.WriteString(wrap(fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, chunk)))
					}
				}
				out.WriteString("\x1b8")
			}
		}
		if out.Len() == 0 {
			return nil
		}
		return tea.RawMsg{Msg: out.String()}
	}
}

// hasImages reports whether any choice carries a sketch.
func (c *choiceList) hasImages() bool {
	for _, ch := range c.Request.Choices {
		if ch.Image != "" {
			return true
		}
	}
	return false
}

// previewChoice is the choice whose sketch shows: the hovered one with
// the mouse, else the one under the cursor; -1 for the fill-in.
func (c *choiceList) previewChoice() int {
	i := c.cursorIdx
	if c.mouseActive && c.hoveredChoice >= 0 {
		i = c.hoveredChoice
	}
	if i < 0 || i >= len(c.Request.Choices) {
		return -1
	}
	return i
}

// previewMaxCols is the sketch width for a list drawn in areaWidth
// cells. It uses the narrower layout (with a scrollbar column) so the
// size stays the same whether or not the list overflows.
func previewMaxCols(areaWidth int) int {
	return max(8, min(areaWidth-5, choiceListMaxWidth)-4)
}

// previewIndent is where sketches start: past the gutter bar, flush with
// the choices' text.
const previewIndent = questionBarWidth

// previewLines is the sketch area: a blank row, then previewRows rows
// with the current choice's picture at their left edge. With Kitty
// graphics the rows stay blank and the picture is placed over them.
func (c *choiceList) previewLines() []contentLine {
	lines := []contentLine{newContentLine("")}
	var pic []string
	if i := c.previewChoice(); i >= 0 && c.Request.Choices[i].Image != "" {
		path := c.Request.Choices[i].Image
		if cols, rows, ok := c.preview.fit(path, c.previewCols); ok {
			if !c.preview.kitty {
				pic = c.preview.blocks(path, cols, rows)
				if pic == nil {
					pic = []string{c.Styles.Editor.QuestionBody.Render("Loading the picture…")}
				}
			}
		} else {
			pic = []string{c.Styles.Editor.QuestionBody.Render("This picture can't be opened.")}
		}
	} else {
		pic = []string{c.Styles.Editor.QuestionBody.Render("No picture for this choice.")}
	}
	lead := strings.Repeat(" ", previewIndent)
	for r := range previewRows {
		text := ""
		if r < len(pic) {
			text = lead + pic[r]
		}
		lines = append(lines, newContentLine(text))
	}
	return lines
}

// placePreview records where the current sketch sits on screen after a
// draw: only when its whole area is in view, since a picture can't be
// cut to the visible rows.
func (c *choiceList) placePreview(area image.Rectangle, top int) {
	c.placement = nil
	c.previewBounds = image.Rectangle{}
	c.previewPath = ""
	if top < 0 {
		return
	}
	i := c.previewChoice()
	if i < 0 || c.Request.Choices[i].Image == "" {
		return
	}
	path := c.Request.Choices[i].Image
	cols, rows, ok := c.preview.fit(path, c.previewCols)
	if !ok {
		return
	}
	first := top + 1 - c.scrollOffset // the area's blank row comes first
	// Block images can be clipped; Kitty images are shown only in full.
	if !c.preview.kitty || first >= 0 && first+rows <= area.Dy() {
		c.previewBounds = image.Rect(area.Min.X+previewIndent, area.Min.Y+first,
			area.Min.X+previewIndent+cols, area.Min.Y+first+rows).Intersect(area)
		c.previewPath = path
	}
	if !c.preview.kitty {
		return
	}
	if first < 0 || first+rows > area.Dy() {
		return
	}
	c.placement = &PreviewPlacement{
		ID:   imageID(path),
		Path: path,
		X:    area.Min.X + previewIndent,
		Y:    area.Min.Y + first,
		Cols: cols,
		Rows: rows,
	}
}

func (c *choiceList) setPreviewer(p *previewer) { c.preview = p }

func (c *choiceList) clearPlacement() { c.placement = nil }

// previewState is the sketch to place on screen (nil for none), and
// a command that loads a block picture when one is needed.
func (c *choiceList) previewState() (*PreviewPlacement, tea.Cmd) {
	if c.preview.kitty {
		return c.placement, nil
	}
	if c.previewCols == 0 {
		return nil, nil
	}
	i := c.previewChoice()
	if i < 0 || c.Request.Choices[i].Image == "" {
		return nil, nil
	}
	path := c.Request.Choices[i].Image
	cols, rows, ok := c.preview.fit(path, c.previewCols)
	if !ok {
		return nil, nil
	}
	return nil, c.preview.loadBlocksCmd(path, cols, rows)
}

// previewHolder is a question component that can show choice sketches.
type previewHolder interface {
	setPreviewer(*previewer)
	previewState() (*PreviewPlacement, tea.Cmd)
}

// SetImageCapabilities lets the form's choices show their sketches
// with the terminal's picture support.
func (f *QuestionForm) SetImageCapabilities(caps *common.Capabilities) {
	p := newPreviewer(caps)
	for _, q := range f.questions {
		if h, ok := q.(previewHolder); ok {
			h.setPreviewer(p)
		}
	}
}

// PreviewWaiting reports whether the question on screen has a sketch to
// place but hasn't been drawn yet, so its place isn't known.
func (f *QuestionForm) PreviewWaiting() bool {
	if f.activeIdx < 0 || f.activeIdx >= len(f.questions) {
		return false
	}
	h, ok := f.questions[f.activeIdx].(interface{ previewWaiting() bool })
	return ok && h.previewWaiting()
}

func (c *choiceList) previewWaiting() bool {
	i := c.previewChoice()
	return c.preview.kitty && c.lastWidth == 0 && i >= 0 && c.Request.Choices[i].Image != ""
}

// PreviewState returns the sketch the form wants placed on screen as
// of its last draw (nil for none), and a command that loads a block
// picture when one is needed. Cheap to call every update.
func (f *QuestionForm) PreviewState() (*PreviewPlacement, tea.Cmd) {
	if f.activeIdx < 0 || f.activeIdx >= len(f.questions) {
		return nil, nil
	}
	if h, ok := f.questions[f.activeIdx].(previewHolder); ok {
		return h.previewState()
	}
	return nil, nil
}
