//go:build !windows

package sessionhost

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

// snapshotDirEnv names a folder for HTML pictures of the end-to-end
// screens, for checking colors and alignment by eye.
const snapshotDirEnv = "CRUSH_HOST_E2E_SNAPSHOTS"

func TestActivitySnapshots(t *testing.T) {
	if os.Getenv(snapshotDirEnv) == "" {
		t.Skip("set CRUSH_HOST_E2E_SNAPSHOTS to save rendered activity frames")
	}
	for _, width := range []int{120, 60} {
		h := newTestHost(t, width, 21,
			Status{Title: "Working on the app", Dir: "/projects/app", State: StateWorking},
			Status{Title: "Waiting for a review", Dir: "/projects/crush", State: StateBackground},
			Status{Title: "Finished chat", Dir: "/projects/notes", State: StateReady},
			Status{Title: "Needs your answer", State: StateWaiting},
			Status{Title: "Unread answer", State: StateReady},
		)
		h.sessions[4].unread = true
		h.applyTheme("graphite")
		for frame := range activityFrames {
			h.activityFrame = frame
			emu := vt.NewSafeEmulator(width, h.height)
			emu.SetDefaultForegroundColor(h.palette().text)
			emu.SetDefaultBackgroundColor(h.palette().bg)
			_, err := emu.Write([]byte(strings.ReplaceAll(h.View().Content, "\n", "\r\n")))
			require.NoError(t, err)
			s := screen{t: t, emu: emu}
			s.snapshot(fmt.Sprintf("activity-%d-%02d", width, frame))
		}
	}
}

func (s *screen) snapshot(name string) {
	dir := os.Getenv(snapshotDirEnv)
	if dir == "" {
		return
	}
	hex := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	var sb strings.Builder
	sb.WriteString(`<!doctype html><meta charset="utf-8"><style>body{margin:0;background:#000}pre{margin:0;font:15px/18px "Noto Sans Mono",monospace}span{display:inline-block;width:9px;height:18px;overflow:visible;white-space:pre}</style><pre>`)
	defFg, defBg := s.emu.ForegroundColor(), s.emu.BackgroundColor()
	for y := range s.emu.Height() {
		for x := 0; x < s.emu.Width(); {
			cell := s.emu.CellAt(x, y)
			if cell == nil {
				cell = &uv.EmptyCell
			}
			fg, bg := cell.Style.Fg, cell.Style.Bg
			if fg == nil {
				fg = defFg
			}
			if bg == nil {
				bg = defBg
			}
			if cell.Style.Attrs&uv.AttrReverse != 0 {
				fg, bg = bg, fg
			}
			w := max(cell.Width, 1)
			weight := ""
			if cell.Style.Attrs&uv.AttrBold != 0 {
				weight = "font-weight:bold;"
			}
			content := cell.Content
			if content == "" {
				content = " "
			}
			fmt.Fprintf(&sb, `<span style="color:%s;background:%s;width:%dpx;%s">%s</span>`, hex(fg), hex(bg), 9*w, weight, html.EscapeString(content))
			x += w
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</pre>")
	_ = os.WriteFile(filepath.Join(dir, name+".html"), []byte(sb.String()), 0o600)
}
