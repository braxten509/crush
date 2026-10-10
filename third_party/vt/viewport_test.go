package vt

import (
	"fmt"
	"image/color"
	"sync"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func requireCellColors(t *testing.T, cell *uv.Cell, foreground, background color.Color) {
	t.Helper()
	if cell == nil {
		t.Fatal("missing cell")
	}
	for _, check := range []struct {
		name      string
		got, want color.Color
	}{
		{"foreground", cell.Style.Fg, foreground},
		{"background", cell.Style.Bg, background},
	} {
		if check.got == nil || check.want == nil {
			if check.got != check.want {
				t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
			}
			continue
		}
		gr, gg, gb, ga := check.got.RGBA()
		wr, wg, wb, wa := check.want.RGBA()
		if gr != wr || gg != wg || gb != wb || ga != wa {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
}

func TestDrawEffectiveDefaultColors(t *testing.T) {
	e := NewEmulator(3, 2)
	t.Cleanup(func() { _ = e.Close() })
	_, _ = e.WriteString("A")
	// Drawing must cover untouched rows as well as text and blank cells.
	e.scr.ClearTouched()
	foreground := color.RGBA{R: 210, G: 190, B: 170, A: 255}
	background := color.RGBA{R: 20, G: 30, B: 40, A: 255}
	for _, theme := range []struct{ foreground, background color.Color }{
		{color.White, color.Black}, {foreground, background},
	} {
		e.SetDefaultForegroundColor(theme.foreground)
		e.SetDefaultBackgroundColor(theme.background)
		destination := uv.NewScreenBuffer(5, 4)
		e.Draw(destination, uv.Rect(1, 1, 3, 2))
		for y := 1; y < 3; y++ {
			for x := 1; x < 4; x++ {
				requireCellColors(t, destination.CellAt(x, y), theme.foreground, theme.background)
			}
		}
		if got := destination.CellAt(1, 1).Content; got != "A" {
			t.Errorf("text = %q, want A", got)
		}
		requireCellColors(t, destination.CellAt(0, 0), nil, nil)
	}
	// Resolving colors while drawing must not bake the theme into stored cells.
	requireCellColors(t, e.CellAt(0, 0), nil, nil)
}

func TestDrawPreservesExplicitAndProgramColors(t *testing.T) {
	e := NewEmulator(4, 1)
	t.Cleanup(func() { _ = e.Close() })
	_, _ = e.WriteString("\x1b[38;2;1;2;3;48;2;4;5;6mX\x1b[0mY")
	explicit := e.CellAt(0, 0).Clone()
	programForeground := color.RGBA{R: 70, A: 255}
	programBackground := color.RGBA{B: 80, A: 255}
	e.SetForegroundColor(programForeground)
	e.SetBackgroundColor(programBackground)
	e.SetDefaultForegroundColor(color.RGBA{G: 90, A: 255})
	e.SetDefaultBackgroundColor(color.RGBA{R: 100, A: 255})
	destination := uv.NewScreenBuffer(4, 1)
	e.Draw(destination, destination.Bounds())
	if !destination.CellAt(0, 0).Equal(explicit) {
		t.Error("drawing changed an explicitly colored cell")
	}
	for x := 1; x < 4; x++ {
		requireCellColors(t, destination.CellAt(x, 0), programForeground, programBackground)
	}
	e.SetForegroundColor(nil)
	e.SetBackgroundColor(nil)
	e.SetDefaultForegroundColor(color.White)
	e.SetDefaultBackgroundColor(color.Black)
	e.Draw(destination, destination.Bounds())
	requireCellColors(t, destination.CellAt(1, 0), color.White, color.Black)
	if !destination.CellAt(0, 0).Equal(explicit) {
		t.Error("reset or theme change replaced explicit colors")
	}
}

func TestOSCColorOverrideResetAndDefaultUpdate(t *testing.T) {
	e := NewEmulator(3, 1)
	t.Cleanup(func() { _ = e.Close() })
	var foregroundEvents, backgroundEvents []color.Color
	e.SetCallbacks(Callbacks{
		ForegroundColor: func(c color.Color) { foregroundEvents = append(foregroundEvents, c) },
		BackgroundColor: func(c color.Color) { backgroundEvents = append(backgroundEvents, c) },
	})
	_, _ = e.WriteString("A\x1b]10;#112233\x07\x1b]11;#445566\x07")
	programForeground := color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 255}
	programBackground := color.RGBA{R: 0x44, G: 0x55, B: 0x66, A: 255}
	e.SetDefaultForegroundColor(color.Black)
	e.SetDefaultBackgroundColor(color.White)
	destination := uv.NewScreenBuffer(3, 1)
	e.Draw(destination, destination.Bounds())
	requireCellColors(t, destination.CellAt(0, 0), programForeground, programBackground)
	_, _ = e.WriteString("\x1b]110\x07\x1b]111\x07")
	e.Draw(destination, destination.Bounds())
	requireCellColors(t, destination.CellAt(0, 0), color.Black, color.White)
	if len(foregroundEvents) != 2 || foregroundEvents[0] == nil || foregroundEvents[1] != nil {
		t.Errorf("foreground callbacks = %v, want override then nil", foregroundEvents)
	}
	if len(backgroundEvents) != 2 || backgroundEvents[0] == nil || backgroundEvents[1] != nil {
		t.Errorf("background callbacks = %v, want override then nil", backgroundEvents)
	}
	e.SetDefaultForegroundColor(programBackground)
	e.SetDefaultBackgroundColor(programForeground)
	e.Draw(destination, destination.Bounds())
	for x := 0; x < 3; x++ {
		requireCellColors(t, destination.CellAt(x, 0), programBackground, programForeground)
	}
}

func TestDrawViewportOffsetsAndAlternateScreen(t *testing.T) {
	e := NewEmulator(4, 2)
	t.Cleanup(func() { _ = e.Close() })
	_, _ = e.WriteString("A\r\nB\r\nC\r\nD")
	if got := e.ScrollbackLen(); got != 2 {
		t.Fatalf("history length = %d, want 2", got)
	}
	for _, test := range []struct {
		offset int
		want   string
	}{
		{-1, "C\nD"}, {0, "C\nD"}, {1, "B\nC"}, {2, "A\nB"}, {99, "A\nB"},
	} {
		destination := uv.NewScreenBuffer(4, 2)
		e.DrawViewport(destination, destination.Bounds(), test.offset)
		if got := uv.TrimSpace(destination.String()); got != test.want {
			t.Errorf("offset %d = %q, want %q", test.offset, got, test.want)
		}
		// Trimmed history columns still need effective colors.
		requireCellColors(t, destination.CellAt(3, 0), color.White, color.Black)
	}
	_, _ = e.WriteString("\x1b[?1049hALT")
	destination := uv.NewScreenBuffer(4, 2)
	e.DrawViewport(destination, destination.Bounds(), 99)
	if got := uv.TrimSpace(destination.String()); got != "ALT\n" {
		t.Errorf("alternate screen = %q, want ALT and blank row", got)
	}
	_, _ = e.WriteString("\x1b[?1049l")
	e.DrawViewport(destination, destination.Bounds(), 1)
	if got := uv.TrimSpace(destination.String()); got != "B\nC" {
		t.Errorf("restored history = %q, want B and C", got)
	}
}

// This screen deliberately retains incoming pointers to test drawing ownership.
type retainingScreen struct {
	uv.ScreenBuffer
	cells map[uv.Position]*uv.Cell
}

func (s *retainingScreen) SetCell(x, y int, cell *uv.Cell) {
	s.cells[uv.Pos(x, y)] = cell
}

func TestDrawViewportPreservesWideStyledCellsAndClones(t *testing.T) {
	e := NewSafeEmulator(4, 2)
	t.Cleanup(func() { _ = e.Close() })
	_, _ = e.WriteString("\x1b[1;38;2;1;2;3;48;2;4;5;6m\x1b]8;id=7;https://example.com\x1b\\界\x1b]8;;\x1b\\\x1b[0m\r\nB\r\nC")
	history := e.ScrollbackCellAt(0, 0)
	if history == nil || history.Width != 2 {
		t.Fatal("expected wide history cell")
	}
	destination := uv.NewScreenBuffer(4, 2)
	e.DrawViewport(destination, destination.Bounds(), 1)
	if !destination.CellAt(0, 0).Equal(history) || !destination.CellAt(1, 0).IsZero() {
		t.Error("wide cell, style, link, or continuation changed")
	}
	// Cropping a wide glyph must keep its colors and stay inside the area.
	cropped := uv.NewScreenBuffer(4, 2)
	e.DrawViewport(cropped, uv.Rect(1, 0, 1, 2), 1)
	if cell := cropped.CellAt(1, 0); cell.Width != 1 || cell.Content != " " {
		t.Error("partial wide glyph was not drawn as a blank")
	}
	requireCellColors(t, cropped.CellAt(1, 0), history.Style.Fg, history.Style.Bg)
	requireCellColors(t, cropped.CellAt(2, 0), nil, nil)
	e.SetDefaultForegroundColor(color.Black)
	e.SetDefaultBackgroundColor(color.White)
	e.DrawViewport(destination, destination.Bounds(), 1)
	if !destination.CellAt(0, 0).Equal(history) {
		t.Error("theme change replaced explicit history colors")
	}
	requireCellColors(t, destination.CellAt(3, 0), color.Black, color.White)
	retained := &retainingScreen{ScreenBuffer: uv.NewScreenBuffer(4, 2), cells: make(map[uv.Position]*uv.Cell)}
	e.DrawViewport(retained, retained.Bounds(), 1)
	retained.cells[uv.Pos(0, 0)].Content = "changed"
	history.Content = "changed"
	e.Scrollback().CellAt(0, 0).Content = "changed"
	if got := e.ScrollbackCellAt(0, 0).Content; got != "界" {
		t.Errorf("caller changed history to %q", got)
	}
	retained.cells = make(map[uv.Position]*uv.Cell)
	e.DrawViewport(retained, retained.Bounds(), 0)
	retained.cells[uv.Pos(0, 0)].Content = "changed"
	e.CellAt(0, 0).Content = "changed"
	if got := e.CellAt(0, 0).Content; got != "B" {
		t.Errorf("caller changed live cell to %q", got)
	}
}

func TestSafeConcurrentOutputDrawingAndDefaults(t *testing.T) {
	e := NewSafeEmulator(12, 3)
	t.Cleanup(func() { _ = e.Close() })
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			_, _ = e.WriteString("\x1b[?1000h\x1b]10;#112233\x07X\r\n\x1b]110\x07\x1b[?1000l")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			e.SetDefaultForegroundColor(color.RGBA{R: uint8(i), A: 255})
			e.SetDefaultBackgroundColor(color.RGBA{B: uint8(i), A: 255})
		}
	}()
	go func() {
		defer workers.Done()
		destination := uv.NewScreenBuffer(12, 3)
		for i := 0; i < 200; i++ {
			e.DrawViewport(destination, destination.Bounds(), i)
			e.Draw(destination, destination.Bounds())
			_ = e.MouseReporting()
			if cell := e.CellAt(0, 0); cell != nil {
				cell.Content = "private copy"
			}
			if cell := e.ScrollbackCellAt(0, 0); cell != nil {
				cell.Content = "private copy"
			}
		}
	}()
	workers.Wait()
}

func TestMouseReportingModes(t *testing.T) {
	for _, mode := range []int{9, 1000, 1001, 1002, 1003} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := NewSafeEmulator(2, 1)
			t.Cleanup(func() { _ = e.Close() })
			if e.MouseReporting() {
				t.Fatal("mouse reporting enabled initially")
			}
			_, _ = e.WriteString("\x1b[?1006h")
			if e.MouseReporting() {
				t.Error("SGR encoding alone enabled reporting")
			}
			_, _ = e.WriteString(fmt.Sprintf("\x1b[?%dh", mode))
			if !e.MouseReporting() {
				t.Error("tracking mode did not enable reporting")
			}
			_, _ = e.WriteString(fmt.Sprintf("\x1b[?%dl", mode))
			if e.MouseReporting() {
				t.Error("reporting remained after mode reset")
			}
			_, _ = e.WriteString("\x1b[?9;1003h\x1b[?1003l")
			if !e.MouseReporting() {
				t.Error("resetting one mode hid another enabled mode")
			}
		})
	}
}
