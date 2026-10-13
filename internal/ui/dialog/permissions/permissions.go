// Package permissions holds the dialog that asks the user to allow or deny
// a tool call.
package permissions

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

// Permissions dialog sizing constants.
const (
	// diffMaxWidth is the maximum width for diff views.
	diffMaxWidth = 180
	// diffSizeRatio is the size ratio for diff views relative to window.
	diffSizeRatio = 0.8
	// simpleMaxWidth is the maximum width for simple content dialogs.
	simpleMaxWidth = 100
	// simpleSizeRatio is the size ratio for simple content dialogs.
	simpleSizeRatio = 0.6
	// simpleHeightRatio is the height ratio for simple content dialogs.
	simpleHeightRatio = 0.5
	// splitModeMinWidth is the minimum width to enable split diff mode.
	splitModeMinWidth = 140
	// layoutSpacingLines is the number of empty lines used for layout spacing.
	layoutSpacingLines = 4
	// minWindowWidth is the minimum window width before forcing fullscreen.
	minWindowWidth = 77
	// minWindowHeight is the minimum window height before forcing fullscreen.
	minWindowHeight = 20
)

// Permissions represents a dialog for permission requests.
type Permissions struct {
	com          *common.Common
	windowWidth  int // Terminal window dimensions.
	windowHeight int
	fullscreen   bool // true when dialog is fullscreen

	permission     permission.PermissionRequest
	selectedOption int // 0: Allow, 1: Allow for session, 2: Deny

	viewport      viewport.Model
	viewportDirty bool // true when viewport content needs to be re-rendered
	viewportWidth int

	// Diff view state.
	diffSplitMode        *bool // nil means use default based on width
	defaultDiffSplitMode bool  // default split mode based on width
	diffXOffset          int   // horizontal scroll offset for diff view
	unifiedDiffContent   string
	splitDiffContent     string

	help   help.Model
	keyMap permissionsKeyMap
}

type permissionsKeyMap struct {
	Left             key.Binding
	Right            key.Binding
	Tab              key.Binding
	Select           key.Binding
	Allow            key.Binding
	AllowSession     key.Binding
	Deny             key.Binding
	Close            key.Binding
	ToggleDiffMode   key.Binding
	ToggleFullscreen key.Binding
	ScrollUp         key.Binding
	ScrollDown       key.Binding
	ScrollLeft       key.Binding
	ScrollRight      key.Binding
	Choose           key.Binding
	Scroll           key.Binding
}

func defaultPermissionsKeyMap() permissionsKeyMap {
	return permissionsKeyMap{
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←", "previous"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→", "next"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next option"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter", "ctrl+y"),
			key.WithHelp("enter", "confirm"),
		),
		Allow: key.NewBinding(
			key.WithKeys("a", "A", "ctrl+a"),
			key.WithHelp("a", "allow"),
		),
		AllowSession: key.NewBinding(
			key.WithKeys("s", "S", "ctrl+s"),
			key.WithHelp("s", "allow session"),
		),
		Deny: key.NewBinding(
			key.WithKeys("d", "D"),
			key.WithHelp("d", "deny"),
		),
		Close: dialog.CloseKey,
		ToggleDiffMode: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "toggle diff view"),
		),
		ToggleFullscreen: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle fullscreen"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("shift+↑", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("shift+↓", "scroll down"),
		),
		ScrollLeft: key.NewBinding(
			key.WithKeys("shift+left", "H"),
			key.WithHelp("shift+←", "scroll left"),
		),
		ScrollRight: key.NewBinding(
			key.WithKeys("shift+right", "L"),
			key.WithHelp("shift+→", "scroll right"),
		),
		Choose: key.NewBinding(
			key.WithKeys("left", "right"),
			key.WithHelp("←/→", "choose"),
		),
		Scroll: key.NewBinding(
			key.WithKeys("shift+left", "shift+down", "shift+up", "shift+right"),
			key.WithHelp("shift+←↓↑→", "scroll"),
		),
	}
}

var _ dialog.Dialog = (*Permissions)(nil)

// PermissionsOption configures the permissions dialog.
type PermissionsOption func(*Permissions)

// WithDiffMode sets the initial diff mode (split or unified).
func WithDiffMode(split bool) PermissionsOption {
	return func(p *Permissions) {
		p.diffSplitMode = &split
	}
}

// NewPermissions creates a new permissions dialog.
func NewPermissions(com *common.Common, perm permission.PermissionRequest, opts ...PermissionsOption) *Permissions {
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	km := defaultPermissionsKeyMap()

	// Configure viewport with matching keybindings.
	vp := viewport.New()
	vp.KeyMap = viewport.KeyMap{
		Up:    km.ScrollUp,
		Down:  km.ScrollDown,
		Left:  km.ScrollLeft,
		Right: km.ScrollRight,
		// Disable other viewport keys to avoid conflicts with dialog shortcuts.
		PageUp:       key.NewBinding(key.WithDisabled()),
		PageDown:     key.NewBinding(key.WithDisabled()),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
	}

	p := &Permissions{
		com:            com,
		permission:     perm,
		selectedOption: 0,
		viewport:       vp,
		help:           h,
		keyMap:         km,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Calculate usable content width (dialog border + horizontal padding).
func (p *Permissions) calculateContentWidth(width int) int {
	t := p.com.Styles
	const dialogHorizontalPadding = 2
	return width - t.Dialog.View.GetHorizontalFrameSize() - dialogHorizontalPadding
}

// ID implements [Dialog].
func (*Permissions) ID() string {
	return dialog.PermissionsID
}

// ToolCallID returns the tool call ID associated with this dialog's
// permission request.
func (p *Permissions) ToolCallID() string {
	return p.permission.ToolCallID
}

// HandleMsg implements [Dialog].
func (p *Permissions) HandleMsg(msg tea.Msg) dialog.Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			// Escape denies the permission request.
			return p.respond(dialog.PermissionDeny)
		case key.Matches(msg, p.keyMap.Right), key.Matches(msg, p.keyMap.Tab):
			p.selectedOption = (p.selectedOption + 1) % 3
		case key.Matches(msg, p.keyMap.Left):
			// Add 2 instead of subtracting 1 to avoid negative modulo.
			p.selectedOption = (p.selectedOption + 2) % 3
		case key.Matches(msg, p.keyMap.Select):
			return p.selectCurrentOption()
		case key.Matches(msg, p.keyMap.Allow):
			return p.respond(dialog.PermissionAllow)
		case key.Matches(msg, p.keyMap.AllowSession):
			return p.respond(dialog.PermissionAllowForSession)
		case key.Matches(msg, p.keyMap.Deny):
			return p.respond(dialog.PermissionDeny)
		case key.Matches(msg, p.keyMap.ToggleDiffMode):
			if p.hasDiffView() {
				newMode := !p.isSplitMode()
				p.diffSplitMode = &newMode
				p.viewportDirty = true
			}
		case key.Matches(msg, p.keyMap.ToggleFullscreen):
			if p.hasDiffView() {
				p.fullscreen = !p.fullscreen
			}
		case key.Matches(msg, p.keyMap.ScrollDown):
			p.viewport, _ = p.viewport.Update(msg)
		case key.Matches(msg, p.keyMap.ScrollUp):
			p.viewport, _ = p.viewport.Update(msg)
		case key.Matches(msg, p.keyMap.ScrollLeft):
			if p.hasDiffView() {
				p.scrollLeft()
			} else {
				p.viewport, _ = p.viewport.Update(msg)
			}
		case key.Matches(msg, p.keyMap.ScrollRight):
			if p.hasDiffView() {
				p.scrollRight()
			} else {
				p.viewport, _ = p.viewport.Update(msg)
			}
		}
	case common.CoalescedWheelMsg:
		if p.hasDiffView() {
			if msg.DeltaX < 0 {
				p.scrollLeft()
			} else if msg.DeltaX > 0 {
				p.scrollRight()
			} else {
				p.viewport, _ = p.viewport.Update(tea.MouseWheelMsg(msg.Mouse))
			}
		} else {
			p.viewport, _ = p.viewport.Update(tea.MouseWheelMsg(msg.Mouse))
		}
	default:
		// Pass unhandled keys to viewport for non-diff content scrolling.
		if !p.hasDiffView() {
			p.viewport, _ = p.viewport.Update(msg)
			p.viewportDirty = true
		}
	}

	return nil
}

func (p *Permissions) selectCurrentOption() tea.Msg {
	switch p.selectedOption {
	case 0:
		return p.respond(dialog.PermissionAllow)
	case 1:
		return p.respond(dialog.PermissionAllowForSession)
	default:
		return p.respond(dialog.PermissionDeny)
	}
}

func (p *Permissions) respond(action dialog.PermissionAction) tea.Msg {
	return dialog.ActionPermissionResponse{
		Permission: p.permission,
		Action:     action,
	}
}

func (p *Permissions) hasDiffView() bool {
	switch p.permission.ToolName {
	case tools.EditToolName, tools.WriteToolName, tools.MultiEditToolName, tools.ReplaceSymbolToolName:
		return true
	}
	return false
}

func (p *Permissions) isSplitMode() bool {
	if p.diffSplitMode != nil {
		return *p.diffSplitMode
	}
	return p.defaultDiffSplitMode
}

const horizontalScrollStep = 5

func (p *Permissions) scrollLeft() {
	p.diffXOffset = max(0, p.diffXOffset-horizontalScrollStep)
	p.viewportDirty = true
}

func (p *Permissions) scrollRight() {
	p.diffXOffset += horizontalScrollStep
	p.viewportDirty = true
}

// Draw implements [Dialog].
// dialogSize returns the outer dialog width and maximum height for the
// given screen area, and whether the window is too small so the dialog
// must fill it. Diff views get more room than simple prompts.
func (p *Permissions) dialogSize(area uv.Rectangle) (width, maxHeight int, forceFullscreen bool) {
	forceFullscreen = area.Dx() <= minWindowWidth || area.Dy() <= minWindowHeight
	switch {
	case forceFullscreen || (p.fullscreen && p.hasDiffView()):
		width, maxHeight = area.Dx(), area.Dy()
	case p.hasDiffView():
		// Wide for side-by-side diffs, capped for readability.
		width = min(int(float64(area.Dx())*diffSizeRatio), diffMaxWidth)
		maxHeight = int(float64(area.Dy()) * diffSizeRatio)
	default:
		// Narrower for simple content like commands and URLs.
		width = min(int(float64(area.Dx())*simpleSizeRatio), simpleMaxWidth)
		maxHeight = int(float64(area.Dy()) * simpleHeightRatio)
	}
	return width, maxHeight, forceFullscreen
}

// contentViewportHeight returns the height for the scrollable content
// viewport given the height taken by the fixed chrome (fixedHeight) and
// the content's natural height. Simple prompts shrink to fit their
// content; diff and fullscreen views always take all remaining height.
func (p *Permissions) contentViewportHeight(forceFullscreen bool, maxHeight, fixedHeight, contentHeight int) int {
	if p.hasDiffView() || forceFullscreen {
		return maxHeight - fixedHeight
	}
	if fixedHeight+contentHeight < maxHeight {
		return max(contentHeight, 3)
	}
	return max(maxHeight-fixedHeight, 3)
}

func (p *Permissions) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	width, maxHeight, forceFullscreen := p.dialogSize(area)
	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	// The dialog fills the screen when forced small or when a diff is
	// expanded; center the buttons then instead of hugging the far edge.
	fullscreen := forceFullscreen || (p.fullscreen && p.hasDiffView())

	contentWidth := p.calculateContentWidth(width)
	header := p.renderHeader(contentWidth)
	buttons := p.renderButtons(contentWidth, fullscreen)
	// Pack the hints to the content width so they truncate cleanly instead
	// of overflowing. The dialog frame supplies the padding, so this renders
	// the hint line without the extra help view inset that kit.RenderDialogHelp
	// applies for RenderContext dialogs.
	helpView := kit.ShortHelpLine(&p.help, p.ShortHelp(), contentWidth)

	p.defaultDiffSplitMode = width >= splitModeMinWidth

	// Pre-render content to measure its actual height, then fit the
	// scrollable viewport into whatever height the fixed chrome leaves.
	renderedContent := p.renderContent(contentWidth)
	contentHeight := lipgloss.Height(renderedContent)
	fixedHeight := lipgloss.Height(header) + lipgloss.Height(buttons) +
		lipgloss.Height(helpView) + dialogStyle.GetVerticalFrameSize() + layoutSpacingLines
	availableHeight := p.contentViewportHeight(forceFullscreen, maxHeight, fixedHeight, contentHeight)

	// Determine if scrollbar is needed.
	needsScrollbar := p.hasDiffView() || contentHeight > availableHeight
	viewportWidth := contentWidth
	if needsScrollbar {
		viewportWidth = contentWidth - 1 // Reserve space for scrollbar.
	}

	if p.viewport.Width() != viewportWidth {
		// Mark content as dirty if width has changed.
		p.viewportDirty = true
		renderedContent = p.renderContent(viewportWidth)
	}

	var content string
	p.viewport.SetWidth(viewportWidth)
	p.viewport.SetHeight(availableHeight)
	if p.viewportDirty {
		p.viewport.SetContent(renderedContent)
		p.viewportWidth = p.viewport.Width()
		p.viewportDirty = false
	}
	content = p.viewport.View()
	if needsScrollbar {
		content = kit.JoinScrollbar(t, content, availableHeight, p.viewport.TotalLineCount(), availableHeight, p.viewport.YOffset())
	}

	parts := []string{header}
	if content != "" {
		parts = append(parts, "", content)
	}
	parts = append(parts, "", buttons, "", helpView)

	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	dialog.DrawCenterCursor(scr, area, dialogStyle.Render(innerContent), nil)
	return nil
}

func (p *Permissions) renderHeader(contentWidth int) string {
	t := p.com.Styles

	title := common.DialogTitle(t, "Permission Required", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)
	title = t.Dialog.Title.Render(title)

	// Tool info.
	toolLine := p.renderToolName(contentWidth)

	lines := []string{title, "", toolLine}

	// Show generic Path only for tools that don't render their own file/path line.
	switch p.permission.ToolName {
	case tools.EditToolName, tools.WriteToolName, tools.MultiEditToolName,
		tools.ViewToolName, tools.ReplaceSymbolToolName,
		tools.DownloadToolName, tools.LSToolName:
		// These tools show their own File/Directory line below.
	default:
		lines = append(lines, p.renderKeyValue("Path", fsext.PrettyPath(p.permission.Path), contentWidth))
	}

	// Add tool-specific header info.
	switch p.permission.ToolName {
	case tools.BashToolName:
		if params, ok := p.permission.Params.(tools.BashPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("Desc", params.Description, contentWidth))
		}
	case tools.DownloadToolName:
		if params, ok := p.permission.Params.(tools.DownloadPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("URL", params.URL, contentWidth))
			lines = append(lines, p.renderKeyValue("File", fsext.PrettyPath(params.FilePath), contentWidth))
		}
	case tools.EditToolName, tools.WriteToolName, tools.MultiEditToolName, tools.ViewToolName, tools.ReplaceSymbolToolName:
		var filePath string
		switch params := p.permission.Params.(type) {
		case tools.EditPermissionsParams:
			filePath = params.FilePath
		case tools.WritePermissionsParams:
			filePath = params.FilePath
		case tools.MultiEditPermissionsParams:
			filePath = params.FilePath
		case tools.ViewPermissionsParams:
			filePath = params.FilePath
		case tools.ReplaceSymbolPermissionsParams:
			filePath = params.FilePath
		}
		if filePath != "" {
			lines = append(lines, p.renderKeyValue("File", fsext.PrettyPath(filePath), contentWidth))
		}
	case tools.LSToolName:
		if params, ok := p.permission.Params.(tools.LSPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("Directory", fsext.PrettyPath(params.Path), contentWidth))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p *Permissions) renderKeyValue(key, value string, width int) string {
	t := p.com.Styles
	keyStyle := t.Dialog.Permissions.KeyText
	valueStyle := t.Dialog.Permissions.ValueText

	keyStr := keyStyle.Render(key)
	valueStr := valueStyle.Width(width - lipgloss.Width(keyStr) - 1).Render(" " + value)

	return lipgloss.JoinHorizontal(lipgloss.Left, keyStr, valueStr)
}

func (p *Permissions) renderToolName(width int) string {
	toolName := p.permission.ToolName

	// Check if this is an MCP tool (format: mcp_<mcpname>_<toolname>).
	if strings.HasPrefix(toolName, "mcp_") {
		parts := strings.SplitN(toolName, "_", 3)
		if len(parts) == 3 {
			mcpName := prettyName(parts[1])
			toolPart := prettyName(parts[2])
			toolName = fmt.Sprintf("%s %s %s", mcpName, styles.ArrowRightIcon, toolPart)
		}
	}

	return p.renderKeyValue("Tool", toolName, width)
}

// prettyName converts snake_case or kebab-case to Title Case.
func prettyName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return stringext.Capitalize(name)
}

func (p *Permissions) renderButtons(contentWidth int, fullscreen bool) string {
	buttons := []common.ButtonOpts{
		{Text: "Allow", UnderlineIndex: 0, Selected: p.selectedOption == 0},
		{Text: "Allow for Session", UnderlineIndex: 10, Selected: p.selectedOption == 1},
		{Text: "Deny", UnderlineIndex: 0, Selected: p.selectedOption == 2},
	}

	content := common.ButtonGroup(p.com.Styles, buttons, "  ")

	// Center when stacked or when the dialog fills the screen; otherwise
	// hug the right edge next to the content. Right-aligning across a
	// full-screen width leaves the buttons stranded in the corner.
	align := lipgloss.Right
	if fullscreen {
		align = lipgloss.Center
	}
	if lipgloss.Width(content) > contentWidth {
		content = common.ButtonGroup(p.com.Styles, buttons, "\n")
		align = lipgloss.Center
	}

	return lipgloss.NewStyle().
		Width(contentWidth).
		Align(align).
		Render(content)
}

func (p *Permissions) canScroll() bool {
	if p.hasDiffView() {
		// Diff views can always scroll.
		return true
	}
	// For non-diff content, check if viewport has scrollable content.
	return !p.viewport.AtTop() || !p.viewport.AtBottom()
}

// ShortHelp implements [help.KeyMap].
func (p *Permissions) ShortHelp() []key.Binding {
	bindings := []key.Binding{
		p.keyMap.Choose,
		p.keyMap.Select,
		p.keyMap.Close,
	}

	if p.canScroll() {
		bindings = append(bindings, p.keyMap.Scroll)
	}

	if p.hasDiffView() {
		bindings = append(
			bindings,
			p.keyMap.ToggleDiffMode,
			p.keyMap.ToggleFullscreen,
		)
	}

	return bindings
}

// FullHelp implements [help.KeyMap].
func (p *Permissions) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}
