package common

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	xstrings "github.com/charmbracelet/x/exp/strings"

	"github.com/charmbracelet/crush/internal/ui/notification"
)

// Capabilities define different terminal capabilities supported.
type Capabilities struct {
	// Profile is the terminal color profile used to determine how colors are
	// rendered.
	Profile colorprofile.Profile
	// Columns is the number of character columns in the terminal.
	Columns int
	// Rows is the number of character rows in the terminal.
	Rows int
	// PixelX is the width of the terminal in pixels.
	PixelX int
	// PixelY is the height of the terminal in pixels.
	PixelY int
	// KittyGraphics indicates whether the terminal supports the Kitty graphics
	// protocol.
	KittyGraphics bool
	// SixelGraphics indicates whether the terminal supports Sixel graphics.
	SixelGraphics bool
	// Env is the terminal environment variables.
	Env uv.Environ
	// TerminalVersion is the terminal version string.
	TerminalVersion string
	// ReportFocusEvents indicates whether the terminal supports focus events.
	ReportFocusEvents bool
	// OSC99Notifications indicates whether the terminal supports OSC 99 notifications.
	OSC99Notifications bool
	// UnicodeCore indicates the terminal reported grapheme width support
	// (mode 2027), so the renderer already measures by grapheme cluster.
	UnicodeCore bool
}

// The width probe prints an emoji-presentation sequence (a symbol plus
// U+FE0F) at the start of a row and reads the cursor back. Per-codepoint
// (wcwidth) measuring counts it as one cell; terminals such as Konsole draw it
// as two without answering the mode 2027 query, and that mismatch shifts the
// rest of the row, sidebar included. The probe row is 2 because a cursor
// report for row 1 reads the same as a modified F3 key.
const (
	widthProbeRow  = 2
	widthProbeText = "\U0001F5E3\uFE0F"
)

// WidthProbe returns the sequence that measures how wide the terminal draws
// [widthProbeText]. The cursor is saved and restored around it; the caller
// must repaint the screen afterwards, since the probe overwrites cells.
func WidthProbe() string {
	return ansi.SaveCursor + ansi.CursorPosition(1, widthProbeRow) + widthProbeText +
		ansi.RequestCursorPositionReport + ansi.RestoreCursor
}

// WidthProbeResult reports whether msg answers [WidthProbe] and, if so,
// whether the terminal drew the probe two cells wide.
func WidthProbeResult(msg tea.CursorPositionMsg) (wide, ok bool) {
	if msg.Y != widthProbeRow-1 || msg.X < 1 || msg.X > 2 {
		return false, false
	}
	return msg.X == 2, true
}

// Update updates the capabilities based on the given message.
func (c *Capabilities) Update(msg any) {
	switch m := msg.(type) {
	case tea.EnvMsg:
		c.Env = uv.Environ(m)
	case tea.ColorProfileMsg:
		c.Profile = m.Profile
	case tea.WindowSizeMsg:
		c.Columns = m.Width
		c.Rows = m.Height
	case uv.PixelSizeEvent:
		c.PixelX = m.Width
		c.PixelY = m.Height
	case uv.KittyGraphicsEvent:
		c.KittyGraphics = true
	case uv.PrimaryDeviceAttributesEvent:
		if slices.Contains(m, 4) {
			c.SixelGraphics = true
		}
	case tea.TerminalVersionMsg:
		c.TerminalVersion = m.Name
	case tea.ModeReportMsg:
		switch m.Mode {
		case ansi.ModeFocusEvent:
			c.ReportFocusEvents = modeSupported(m.Value)
		case ansi.ModeUnicodeCore:
			c.UnicodeCore = c.UnicodeCore || m.Value.IsSet() || m.Value.IsReset() || m.Value.IsPermanentlySet()
		}
	case uv.UnknownOscEvent:
		if notification.DetectOSC99Support(string(m)) {
			c.OSC99Notifications = true
		}
	}
}

// QueryCmd returns a [tea.Cmd] that queries the terminal for different
// capabilities.
func QueryCmd(env uv.Environ) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(ansi.RequestPrimaryDeviceAttributes)
	sb.WriteString(ansi.QueryModifyOtherKeys)
	sb.WriteString(ansi.RequestModeFocusEvent)
	sb.WriteString(notification.OSC99QuerySequence())

	// Queries that should only be sent to "smart" normal terminals.
	shouldQueryFor := shouldQueryCapabilities(env)
	if shouldQueryFor {
		sb.WriteString(ansi.RequestNameVersion)
		sb.WriteString(ansi.WindowOp(14)) // Window size in pixels
		kittyReq := ansi.KittyGraphics([]byte("AAAA"), "i=31", "s=1", "v=1", "a=q", "t=d", "f=24")
		if _, isTmux := env.LookupEnv("TMUX"); isTmux {
			kittyReq = ansi.TmuxPassthrough(kittyReq)
		}
		sb.WriteString(kittyReq)
	}

	return tea.Raw(sb.String())
}

// WidthMethod is how the terminal measures text: by grapheme cluster when
// it reported mode 2027 (or the width probe showed it draws that way), else
// per codepoint. Frames must be drawn with the renderer's method, or a row
// holding a cluster the two disagree on shifts by a cell.
func (c Capabilities) WidthMethod() ansi.Method {
	if c.UnicodeCore {
		return ansi.GraphemeWidth
	}
	return ansi.WcWidth
}

// SupportsTrueColor returns true if the terminal supports true color.
func (c Capabilities) SupportsTrueColor() bool {
	return c.Profile == colorprofile.TrueColor
}

// SupportsKittyGraphics returns true if the terminal supports Kitty graphics.
func (c Capabilities) SupportsKittyGraphics() bool {
	return c.KittyGraphics
}

// SupportsSixelGraphics returns true if the terminal supports Sixel graphics.
func (c Capabilities) SupportsSixelGraphics() bool {
	return c.SixelGraphics
}

// CellSize returns the size of a single terminal cell in pixels.
func (c Capabilities) CellSize() (width, height int) {
	if c.Columns == 0 || c.Rows == 0 {
		return 0, 0
	}
	return c.PixelX / c.Columns, c.PixelY / c.Rows
}

func modeSupported(v ansi.ModeSetting) bool {
	return v.IsSet() || v.IsReset()
}

// kittyTerminals defines terminals supporting querying capabilities.
var kittyTerminals = []string{"alacritty", "ghostty", "kitty", "rio", "wezterm"}

func shouldQueryCapabilities(env uv.Environ) bool {
	const osVendorTypeApple = "Apple"
	termType := env.Getenv("TERM")
	termProg, okTermProg := env.LookupEnv("TERM_PROGRAM")
	_, okSSHTTY := env.LookupEnv("SSH_TTY")
	if okTermProg && strings.Contains(termProg, osVendorTypeApple) {
		return false
	}
	return (!okTermProg && !okSSHTTY) ||
		(!strings.Contains(termProg, osVendorTypeApple) && !okSSHTTY) ||
		// Terminals that do support XTVERSION.
		xstrings.ContainsAnyOf(termType, kittyTerminals...)
}
