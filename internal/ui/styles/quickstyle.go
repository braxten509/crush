package styles

import (
	"image/color"
	"math"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/diffview"
	uv "github.com/charmbracelet/ultraviolet"
)

// quickStyleOpts is the palette of colors used by quickStyle to simplify the
// process of building a theme.
type quickStyleOpts struct {
	// Brand.
	primary   color.Color
	secondary color.Color
	accent    color.Color
	keyword   color.Color

	// Default foreground and background colors.
	fgBase color.Color
	bgBase color.Color

	// Low-contrast dividers, separators, and rule lines.
	separator color.Color

	fgSubtle     color.Color
	fgMoreSubtle color.Color
	fgMostSubtle color.Color

	// Contrast pairings: foregrounds designed to sit on top of a
	// matching background role.
	onPrimary color.Color // foreground on primary backgrounds.

	bgMostVisible  color.Color
	bgLessVisible  color.Color
	bgLeastVisible color.Color

	// Statuses.
	destructive       color.Color
	error             color.Color
	warning           color.Color
	warningSubtle     color.Color
	attention         color.Color
	busy              color.Color
	info              color.Color
	infoMoreSubtle    color.Color
	infoMostSubtle    color.Color
	success           color.Color
	successMoreSubtle color.Color
	successMostSubtle color.Color
	yolo              color.Color
	plan              color.Color
	planMoreSubtle    color.Color

	// Diff view. Inserted (positive) and deleted (negative) lines. Each
	// line paints two background bands: the gutter (line-number column)
	// uses the darker gutter background, and the code band, which the
	// sign column sits inside, uses the lighter one. The foreground is
	// ink for everything outside the code itself, that is the signs and
	// the line numbers.
	//
	// All six are optional: unset foregrounds default to the success and
	// destructive hues, and unset backgrounds blend the foreground over
	// bgBase (see deriveDiffColors).
	diffInsertFg       color.Color // diff insert signs and line-number ink.
	diffInsertCodeBg   color.Color // diff insert code and sign band background.
	diffInsertGutterBg color.Color // diff insert line-number band background.
	diffDeleteFg       color.Color // diff delete signs and line-number ink.
	diffDeleteCodeBg   color.Color // diff delete code and sign band background.
	diffDeleteGutterBg color.Color // diff delete line-number band background.

	// Buttons. Backgrounds for each button state; the foregrounds are
	// derived from onPrimary / fgBase.
	button         color.Color // focused/primary button background.
	buttonSubtle   color.Color // blurred button background.
	buttonInactive color.Color // inactive (unfocused pane) button background.
	buttonHovered  color.Color // hovered button background.

	// ANSI 16-color palette. These remap the basic terminal colors that
	// programs emit (e.g. bang-mode shell output) onto legible, on-brand
	// colors instead of leaving them to the user's terminal defaults.
	// Normal intensity.
	ansiBlack   color.Color
	ansiRed     color.Color
	ansiGreen   color.Color
	ansiYellow  color.Color
	ansiBlue    color.Color
	ansiMagenta color.Color
	ansiCyan    color.Color
	ansiWhite   color.Color

	// Bright intensity.
	ansiBrightBlack   color.Color
	ansiBrightRed     color.Color
	ansiBrightGreen   color.Color
	ansiBrightYellow  color.Color
	ansiBrightBlue    color.Color
	ansiBrightMagenta color.Color
	ansiBrightCyan    color.Color
	ansiBrightWhite   color.Color
}

// Diff tint blend ratios: the fraction of the insert/delete foreground
// hue blended over bgBase for the code area and the line-number gutter.
// The gutter stays subtler than the code area, and the delete tints are
// more muted than the insert ones. These approximate the tints the
// built-in themes used when they were picked by hand; no single set of
// CIELAB ratios can reproduce those per-theme RGB choices exactly, so
// these are fitted to land closest to both built-in themes.
const (
	diffInsertCodeBlend   = 0.20
	diffInsertGutterBlend = 0.13
	diffDeleteCodeBlend   = 0.15
	diffDeleteGutterBlend = 0.08
)

// deriveDiffColors fills unset diff tokens: foregrounds default to the
// success and destructive hues, and backgrounds blend the foreground
// over bgBase.
func (o *quickStyleOpts) deriveDiffColors() {
	if o.diffInsertFg == nil {
		o.diffInsertFg = o.success
	}
	if o.diffDeleteFg == nil {
		o.diffDeleteFg = o.destructive
	}
	if o.diffInsertCodeBg == nil && o.diffInsertFg != nil && o.bgBase != nil {
		o.diffInsertCodeBg = blendTint(o.diffInsertFg, o.bgBase, diffInsertCodeBlend)
	}
	if o.diffInsertGutterBg == nil && o.diffInsertFg != nil && o.bgBase != nil {
		o.diffInsertGutterBg = blendTint(o.diffInsertFg, o.bgBase, diffInsertGutterBlend)
	}
	if o.diffDeleteCodeBg == nil && o.diffDeleteFg != nil && o.bgBase != nil {
		o.diffDeleteCodeBg = blendTint(o.diffDeleteFg, o.bgBase, diffDeleteCodeBlend)
	}
	if o.diffDeleteGutterBg == nil && o.diffDeleteFg != nil && o.bgBase != nil {
		o.diffDeleteGutterBg = blendTint(o.diffDeleteFg, o.bgBase, diffDeleteGutterBlend)
	}
}

// blendTint blends fg over bg in CIELAB (via lipgloss.Blend1D) with the
// given fraction of fg: 0 returns bg, 1 returns fg.
func blendTint(fg, bg color.Color, fraction float64) color.Color {
	const steps = 101
	gradient := lipgloss.Blend1D(steps, fg, bg)
	// Blend1D's first stop is pure fg, so the gradient index counts fg
	// share down from 1.
	i := min(int(math.Round((1-fraction)*(steps-1))), steps-1)
	return gradient[i]
}

// neutralGray returns the gray with c's average brightness.
func neutralGray(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	v := uint8((r + g + b) / 3 >> 8)
	return color.RGBA{R: v, G: v, B: v, A: 0xff}
}

// quickStyle builds the default Styles (that is, the default theme, Charmtone
// Pantera) from a palette of semi-semanticly-named colors.
//
// The idea here is that you can do most of the work on a theme with quickStyle,
// then add overrides as needed.
func quickStyle(o quickStyleOpts) Styles {
	o.deriveDiffColors()

	var (
		base   = lipgloss.NewStyle().Foreground(o.fgBase)
		muted  = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
		subtle = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
		s      Styles
	)

	quickStyleInputs(&s, o, base)
	quickStyleMarkdown(&s, o)
	quickStyleMarkdownVariants(&s, o, base)
	quickStyleQuietMarkdown(&s, o, muted, subtle)
	quickStyleWidgets(&s, o, base)
	quickStyleHeader(&s, o, base, muted, subtle)
	quickStyleTools(&s, o, base, muted, subtle)
	quickStyleEditor(&s, o)
	quickStyleSidebar(&s, o, base, muted, subtle)
	quickStyleChat(&s, o, base, muted, subtle)
	quickStyleDialogs(&s, o, base)
	quickStyleCompletions(&s, o, base)

	return s
}

// quickStyleInputs sets the background and working colors, the text input and
// the composer textarea.
func quickStyleInputs(s *Styles, o quickStyleOpts, base lipgloss.Style) {
	s.Background = o.bgBase

	// Populate color fields
	s.WorkingGradFromColor = o.primary
	s.WorkingGradToColor = o.secondary
	s.WorkingLabelColor = o.fgMostSubtle
	s.WorkingTimerColor = o.fgMostSubtle

	s.TextInput = textinput.Styles{
		Focused: textinput.StyleState{
			Text:        base,
			Placeholder: base.Foreground(o.fgMostSubtle),
			Prompt:      base.Foreground(o.accent),
			Suggestion:  base.Foreground(o.fgMostSubtle),
		},
		Blurred: textinput.StyleState{
			Text:        base.Foreground(o.fgMoreSubtle),
			Placeholder: base.Foreground(o.fgMostSubtle),
			Prompt:      base.Foreground(o.fgMoreSubtle),
			Suggestion:  base.Foreground(o.fgMostSubtle),
		},
		Cursor: textinput.CursorStyle{
			Color: o.secondary,
			Shape: tea.CursorBlock,
			Blink: true,
		},
	}

	// The composer sits on a band, like the user's sent messages.
	band := base.Background(o.bgLeastVisible)
	s.Editor.Textarea = textarea.Styles{
		Focused: textarea.StyleState{
			Base:             band,
			Text:             band,
			LineNumber:       band.Foreground(o.fgMostSubtle),
			CursorLine:       band,
			CursorLineNumber: band.Foreground(o.fgMostSubtle),
			Placeholder:      band.Foreground(o.fgMostSubtle),
			Prompt:           band.Foreground(o.accent),
			Selection:        base.Foreground(o.onPrimary).Background(o.secondary),
		},
		Blurred: textarea.StyleState{
			Base:             band,
			Text:             band.Foreground(o.fgMoreSubtle),
			LineNumber:       band.Foreground(o.fgMoreSubtle),
			CursorLine:       band,
			CursorLineNumber: band.Foreground(o.fgMoreSubtle),
			Placeholder:      band.Foreground(o.fgMostSubtle),
			Prompt:           band.Foreground(o.fgMoreSubtle),
			Selection:        base.Foreground(o.onPrimary).Background(o.secondary),
		},
		Cursor: textarea.CursorStyle{
			Color: o.secondary,
			Shape: tea.CursorBlock,
			Blink: true,
		},
	}
}

// quickStyleWidgets sets the help, diff view and file picker styles.
func quickStyleWidgets(s *Styles, o quickStyleOpts, base lipgloss.Style) {
	s.Help = help.Styles{
		ShortKey:       base.Foreground(o.fgMoreSubtle),
		ShortDesc:      base.Foreground(o.fgMostSubtle),
		ShortSeparator: base.Foreground(o.separator),
		Ellipsis:       base.Foreground(o.separator),
		FullKey:        base.Foreground(o.fgMoreSubtle),
		FullDesc:       base.Foreground(o.fgMostSubtle),
		FullSeparator:  base.Foreground(o.separator),
	}

	s.Diff = diffview.Style{
		DividerLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(o.fgSubtle).
				Background(o.bgLeastVisible),
			Code: lipgloss.NewStyle().
				Foreground(o.fgSubtle).
				Background(o.bgLeastVisible),
		},
		MissingLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(o.bgLeastVisible),
			Code: lipgloss.NewStyle().
				Background(o.bgLeastVisible),
		},
		EqualLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(o.fgMoreSubtle).
				Background(o.bgBase),
			Code: lipgloss.NewStyle().
				Foreground(o.fgMoreSubtle).
				Background(o.bgBase),
		},
		InsertLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(o.diffInsertFg).
				Background(o.diffInsertGutterBg),
			Symbol: lipgloss.NewStyle().
				Foreground(o.diffInsertFg).
				Background(o.diffInsertCodeBg),
			Code: lipgloss.NewStyle().
				Background(o.diffInsertCodeBg),
		},
		DeleteLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(o.diffDeleteFg).
				Background(o.diffDeleteGutterBg),
			Symbol: lipgloss.NewStyle().
				Foreground(o.diffDeleteFg).
				Background(o.diffDeleteCodeBg),
			Code: lipgloss.NewStyle().
				Background(o.diffDeleteCodeBg),
		},
		Filename: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(o.fgSubtle).
				Background(o.bgLeastVisible),
			Code: lipgloss.NewStyle().
				Foreground(o.fgSubtle).
				Background(o.bgLeastVisible),
		},
	}

	s.FilePicker = filepicker.Styles{
		DisabledCursor:   base.Foreground(o.fgMoreSubtle),
		Cursor:           base.Foreground(o.fgBase),
		Symlink:          base.Foreground(o.fgMostSubtle),
		Directory:        base.Foreground(o.primary),
		File:             base.Foreground(o.fgBase),
		DisabledFile:     base.Foreground(o.fgMoreSubtle),
		DisabledSelected: base.Background(o.bgMostVisible).Foreground(o.fgMoreSubtle),
		Permission:       base.Foreground(o.fgMoreSubtle),
		Selected:         base.Background(o.primary).Foreground(o.fgBase),
		FileSize:         base.Foreground(o.fgMoreSubtle),
		EmptyDirectory:   base.Foreground(o.fgMoreSubtle).PaddingLeft(2).SetString("Empty directory"),
	}
}

// quickStyleHeader sets the tool success mark, the header and the compact
// details.
func quickStyleHeader(s *Styles, o quickStyleOpts, base, muted, subtle lipgloss.Style) {
	// borders
	s.ToolCallSuccess = lipgloss.NewStyle().Foreground(o.success).SetString(ToolSuccess)

	s.Header.Charm = base.Foreground(o.secondary)
	s.Header.Diagonals = base.Foreground(o.primary)
	s.Header.Percentage = muted
	s.Header.HypercreditIcon = base.Foreground(o.secondary)
	s.Header.Keystroke = muted
	s.Header.KeystrokeTip = subtle
	s.Header.WorkingDir = muted
	s.Header.GitBranch = base.Foreground(o.secondary)
	s.Header.Separator = subtle
	s.Header.Wrapper = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Header.LogoGradCanvas = lipgloss.NewStyle()
	s.Header.LogoGradFromColor = o.secondary
	s.Header.LogoGradToColor = o.primary

	s.CompactDetails.Title = base
	s.CompactDetails.View = base.Padding(0, 1, 1, 1).Border(lipgloss.RoundedBorder()).BorderForeground(o.primary)
	s.CompactDetails.Version = lipgloss.NewStyle().Foreground(o.separator)
}

// quickStyleTools sets the tool call styles.
func quickStyleTools(s *Styles, o quickStyleOpts, base, muted, subtle lipgloss.Style) {
	// Tool rendering styles
	s.Tool.IconPending = base.Foreground(o.successMostSubtle).SetString(ToolPending)
	s.Tool.IconSuccess = base.Foreground(o.success).SetString(ToolSuccess)
	s.Tool.IconError = base.Foreground(o.error).SetString(ToolError)
	s.Tool.IconCancelled = muted.SetString(ToolPending)

	s.Tool.NameNormal = base.Foreground(o.info)
	s.Tool.NameNested = base.Foreground(o.info)
	s.Tool.ChangesAdd = base.Foreground(o.diffInsertFg)
	s.Tool.ChangesDel = base.Foreground(o.diffDeleteFg)

	s.Tool.ParamMain = subtle
	s.Tool.ParamKey = subtle

	// Content rendering - prepared styles that accept width parameter
	s.Tool.ContentLine = muted.Background(o.bgLeastVisible)
	s.Tool.ContentTruncation = muted.Background(o.bgLeastVisible)
	s.Tool.ContentCodeLine = base.Background(o.bgBase).PaddingLeft(2)
	s.Tool.ContentCodeTruncation = muted.Background(o.bgBase).PaddingLeft(2)
	s.Tool.ContentCodeBg = o.bgBase
	s.Tool.Body = base.PaddingLeft(2)

	// Deprecated - kept for backward compatibility
	s.Tool.ContentBg = muted.Background(o.bgLeastVisible)
	s.Tool.ContentText = muted
	s.Tool.ContentLineNumber = base.Foreground(o.fgMoreSubtle).Background(o.bgBase).PaddingRight(1).PaddingLeft(1)

	s.Tool.StateWaiting = base.Foreground(o.fgMostSubtle)
	s.Tool.StateCancelled = base.Foreground(o.fgMostSubtle)

	s.Tool.ErrorTag = base.Padding(0, 1).Background(o.destructive).Foreground(o.onPrimary)
	s.Tool.ErrorMessage = base.Foreground(o.fgSubtle)

	s.Tool.WarnTag = base.Padding(0, 1).Background(o.attention).Foreground(o.bgBase).Bold(true)
	s.Tool.WarnMessage = base.Foreground(o.fgSubtle)

	// Diff and multi-edit styles
	s.Tool.DiffTruncation = muted.Background(o.bgLeastVisible).PaddingLeft(2)
	s.Tool.NoteTag = base.Padding(0, 1).Background(o.info).Foreground(o.onPrimary)
	s.Tool.NoteMessage = base.Foreground(o.fgSubtle)

	// Job header styles
	s.Tool.JobIconPending = base.Foreground(o.successMostSubtle)
	s.Tool.JobIconError = base.Foreground(o.error)
	s.Tool.JobIconSuccess = base.Foreground(o.success)
	s.Tool.JobToolName = base.Foreground(o.info)
	s.Tool.JobAction = base.Foreground(o.infoMostSubtle)
	s.Tool.JobPID = muted
	s.Tool.JobDescription = subtle

	// Agent task styles
	s.Tool.AgentTaskTag = base.Bold(true).Padding(0, 1).MarginLeft(2).Background(o.infoMoreSubtle).Foreground(o.onPrimary)
	s.Tool.AgentPrompt = muted

	// Agentic fetch styles
	s.Tool.AgenticFetchPromptTag = base.Bold(true).Padding(0, 1).MarginLeft(2).Background(o.success).Foreground(o.separator)

	// Todo styles
	s.Tool.TodoRatio = base.Foreground(o.infoMostSubtle)
	s.Tool.TodoCompletedIcon = base.Foreground(o.success)
	s.Tool.TodoInProgressIcon = base.Foreground(o.successMostSubtle)
	s.Tool.TodoPendingIcon = base.Foreground(o.fgMoreSubtle)
	s.Tool.TodoStatusNote = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Tool.TodoItem = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Tool.TodoJustStarted = lipgloss.NewStyle().Foreground(o.fgBase)

	// MCP styles
	s.Tool.MCPName = base.Foreground(o.info)
	s.Tool.MCPToolName = base.Foreground(o.infoMostSubtle)
	s.Tool.MCPArrow = base.Foreground(o.info).SetString(ArrowRightIcon)

	// Loading indicators for images, skills
	s.Tool.ResourceLoadedText = base.Foreground(o.success)
	s.Tool.ResourceLoadedIndicator = base.Foreground(o.successMostSubtle)
	s.Tool.ResourceName = base
	s.Tool.MediaType = base
	s.Tool.ResourceSize = base.Foreground(o.fgMoreSubtle)

	// Hook styles
	s.Tool.HookLabel = base.Foreground(o.successMoreSubtle)
	s.Tool.HookName = base
	s.Tool.HookMatcher = base.Foreground(o.fgMoreSubtle)
	s.Tool.HookArrow = base.Foreground(o.successMoreSubtle)
	s.Tool.HookDetail = base.Foreground(o.fgMoreSubtle)
	s.Tool.HookOK = base.Foreground(o.successMostSubtle)
	s.Tool.HookDenied = base.Foreground(o.error)
	s.Tool.HookDeniedLabel = base.Foreground(o.destructive)
	s.Tool.HookDeniedReason = base.Foreground(o.bgMostVisible)
	s.Tool.HookRewrote = base.Foreground(o.bgMostVisible)

	// Tool-call action verbs and result-list styling.
	s.Tool.ActionCreate = lipgloss.NewStyle().Foreground(o.successMoreSubtle)
	s.Tool.ActionDestroy = lipgloss.NewStyle().Foreground(o.destructive)
	s.Tool.ResultEmpty = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Tool.ResultTruncation = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Tool.ResultItemName = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Tool.ResultItemDesc = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
}

// quickStyleEditor sets the buttons, the composer prompts and the question
// form tabs.
func quickStyleEditor(s *Styles, o quickStyleOpts) {
	// Buttons
	s.Button.Focused = lipgloss.NewStyle().Foreground(o.onPrimary).Background(o.button)
	s.Button.Blurred = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.buttonSubtle)
	s.Button.Inactive = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.buttonInactive)
	s.Button.Hovered = lipgloss.NewStyle().Foreground(o.onPrimary).Background(o.buttonHovered)
	s.Button.Negative = lipgloss.NewStyle().Foreground(o.onPrimary).Background(o.error)

	// Editor
	// The prompt column is 2 cells on the composer band: a mark for the
	// mode, colored by it, on the first row; blank on the rest. Unfocused,
	// the mark turns bold near-white, not gray: a one-cell glyph on the
	// band was too faint to find.
	promptBand := lipgloss.NewStyle().Background(o.bgLeastVisible)
	promptMark := func(c color.Color, mark string) lipgloss.Style {
		return promptBand.Foreground(c).Bold(true).SetString(mark + " ")
	}
	promptRest := promptBand.SetString("  ")
	s.Editor.PromptNormalIconFocused = promptMark(o.accent, "›")
	s.Editor.PromptNormalIconBlurred = promptMark(o.fgBase, "›")
	s.Editor.PromptNormalFocused = promptRest
	s.Editor.PromptNormalBlurred = promptRest
	s.Editor.PromptPlanIconFocused = promptMark(o.plan, "⏸")
	s.Editor.PromptPlanIconBlurred = promptMark(o.fgBase, "⏸")
	s.Editor.PromptPlanDotsFocused = promptRest
	s.Editor.PromptPlanDotsBlurred = promptRest
	// YOLO keeps the normal mark in its own color; the status line names it.
	s.Editor.PromptYoloIconFocused = promptMark(o.busy, "›")
	s.Editor.PromptYoloIconBlurred = promptMark(o.fgBase, "›")
	s.Editor.PromptYoloDotsFocused = promptRest
	s.Editor.PromptYoloDotsBlurred = promptRest
	s.Editor.PromptBangIconFocused = promptMark(o.primary, "!")
	s.Editor.PromptBangIconBlurred = promptMark(o.fgBase, "!")
	s.Editor.PromptBangDotsFocused = promptRest
	s.Editor.PromptBangDotsBlurred = promptRest
	// Question forms replace the composer. Their "?" sits on a colored
	// tag, as in the original Crush, so an open question stands out from
	// the chat around it.
	s.Editor.PromptQuestionIconFocused = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.primary).Bold(true).SetString(" ? ")
	s.Editor.PromptQuestionIconBlurred = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.bgLessVisible).Bold(true).SetString(" ? ")
	s.Editor.QuestionSelected = lipgloss.NewStyle().Foreground(o.secondary).Bold(true)
	s.Editor.QuestionUnselected = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Editor.QuestionBody = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Editor.QuestionConfirm = lipgloss.NewStyle().Foreground(o.primary).Bold(true)
	s.Editor.QuestionNote = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Editor.QuestionCursorBar = lipgloss.NewStyle().Foreground(o.secondary)
	s.Editor.QuestionRadioOn = lipgloss.NewStyle().Foreground(o.secondary).SetString(RadioOn)
	s.Editor.QuestionRadioOff = lipgloss.NewStyle().Foreground(o.fgSubtle).SetString(RadioOff)
	s.Editor.QuestionCheckOn = lipgloss.NewStyle().Foreground(o.secondary).SetString(RadioOn)
	s.Editor.QuestionCheckOff = lipgloss.NewStyle().Foreground(o.fgSubtle).SetString(RadioOff)

	s.Radio.On = lipgloss.NewStyle().Foreground(o.fgSubtle).SetString(RadioOn)
	s.Radio.Off = lipgloss.NewStyle().Foreground(o.fgSubtle).SetString(RadioOff)
	s.Radio.Label = lipgloss.NewStyle().Foreground(o.fgSubtle)

	// Tabs for batch question forms. All borders use charple
	// (primary). Active tab has an open bottom that merges with
	// the content area; inactive tabs have a closed bottom. First
	// tab gets a right-angle bottom-left corner at draw time.
	borderColor := uv.Style{Fg: o.primary}
	inactiveBorder := uv.RoundedBorder().Style(borderColor)
	inactiveBorder.BottomLeft = uv.Side{Content: "┴", Style: borderColor}
	inactiveBorder.BottomRight = uv.Side{Content: "┴", Style: borderColor}
	activeBorder := uv.RoundedBorder().Style(borderColor)
	activeBorder.Bottom = uv.Side{Content: " ", Style: borderColor}
	activeBorder.BottomLeft = uv.Side{Content: "┘", Style: borderColor}
	activeBorder.BottomRight = uv.Side{Content: "└", Style: borderColor}

	s.Tab.ActiveBorder = activeBorder
	s.Tab.InactiveBorder = inactiveBorder

	blurredBorderColor := uv.Style{Fg: o.fgMoreSubtle}
	inactiveBorderBlurred := uv.RoundedBorder().Style(blurredBorderColor)
	inactiveBorderBlurred.BottomLeft = uv.Side{Content: "┴", Style: blurredBorderColor}
	inactiveBorderBlurred.BottomRight = uv.Side{Content: "┴", Style: blurredBorderColor}
	activeBorderBlurred := uv.RoundedBorder().Style(blurredBorderColor)
	activeBorderBlurred.Bottom = uv.Side{Content: " ", Style: blurredBorderColor}
	activeBorderBlurred.BottomLeft = uv.Side{Content: "┘", Style: blurredBorderColor}
	activeBorderBlurred.BottomRight = uv.Side{Content: "└", Style: blurredBorderColor}
	s.Tab.ActiveBorderBlurred = activeBorderBlurred
	s.Tab.InactiveBorderBlurred = inactiveBorderBlurred

	s.Tab.ActiveStyle = uv.Style{Fg: o.fgBase}
	s.Tab.InactiveStyle = uv.Style{Fg: o.fgMoreSubtle}
}

// quickStyleSidebar sets the logo, section, initialize, sidebar, footer and
// model info styles.
func quickStyleSidebar(s *Styles, o quickStyleOpts, base, muted, subtle lipgloss.Style) {
	// Logo
	s.Logo.FieldColor = o.primary
	s.Logo.TitleColorA = o.secondary
	s.Logo.TitleColorB = o.primary
	s.Logo.CharmColor = o.secondary
	s.Logo.VersionColor = o.primary
	s.Logo.SmallCharm = lipgloss.NewStyle().Foreground(o.secondary)
	s.Logo.SmallDiagonals = lipgloss.NewStyle().Foreground(o.primary)
	s.Logo.GradCanvas = lipgloss.NewStyle()
	s.Logo.SmallGradFromColor = o.secondary
	s.Logo.SmallGradToColor = o.primary
	s.Logo.ShineColor = o.ansiBrightWhite
	s.Logo.FlowColor = o.keyword

	// Section
	s.Section.Title = subtle
	s.Section.Line = base.Foreground(o.separator)

	// Initialize
	s.Initialize.Header = base
	s.Initialize.Content = muted
	s.Initialize.Accent = base.Foreground(o.successMostSubtle)

	// ResourceGroup (LSP/MCP/skills sidebar lists).
	s.Resource.Heading = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Resource.Name = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Resource.StatusText = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Resource.OfflineIcon = lipgloss.NewStyle().Foreground(o.bgMostVisible).SetString("●")
	s.Resource.BusyIcon = s.Resource.OfflineIcon.Foreground(o.busy)
	s.Resource.ErrorIcon = s.Resource.OfflineIcon.Foreground(o.destructive)
	s.Resource.OnlineIcon = s.Resource.OfflineIcon.Foreground(o.successMostSubtle)
	s.Resource.OnlineText = lipgloss.NewStyle().Foreground(o.successMostSubtle)
	s.Resource.NeedsAuthIcon = s.Resource.OfflineIcon.Foreground(o.attention)
	s.Resource.DisabledIcon = lipgloss.NewStyle().Foreground(o.fgMoreSubtle).SetString("●")
	s.Resource.AdditionalText = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Resource.CapabilityCount = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Resource.RowTitleBase = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Resource.RowDescBase = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Resource.DefaultTitleFg = o.fgMoreSubtle
	s.Resource.DefaultDescFg = o.fgMostSubtle

	// LSP
	s.LSP.ErrorDiagnostic = base.Foreground(o.error)
	s.LSP.WarningDiagnostic = base.Foreground(o.warningSubtle)
	s.LSP.HintDiagnostic = base.Foreground(o.fgSubtle)
	s.LSP.InfoDiagnostic = base.Foreground(o.info)

	// Files
	s.Files.Path = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Files.Additions = lipgloss.NewStyle().Foreground(o.successMostSubtle)
	s.Files.Deletions = lipgloss.NewStyle().Foreground(o.error)
	s.Files.SectionTitle = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Files.EmptyMessage = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Files.TruncationHint = lipgloss.NewStyle().Foreground(o.fgMostSubtle)

	// Sidebar
	s.Sidebar.SessionTitle = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Sidebar.WorkingDir = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)

	// Composer footer.
	s.ComposerFooter.Model = lipgloss.NewStyle().Foreground(o.fgBase).Bold(true)
	s.ComposerFooter.Text = lipgloss.NewStyle().Foreground(o.fgSubtle)
	s.ComposerFooter.Accent = lipgloss.NewStyle().Foreground(o.secondary)
	s.ComposerFooter.Tokens = lipgloss.NewStyle().Foreground(o.info)
	s.ComposerFooter.Context = lipgloss.NewStyle().Foreground(o.warningSubtle)
	s.ComposerFooter.Limit5h = lipgloss.NewStyle().Foreground(o.keyword)
	s.ComposerFooter.LimitWeekly = lipgloss.NewStyle().Foreground(o.destructive)
	s.ComposerFooter.LimitOther = lipgloss.NewStyle().Foreground(o.infoMoreSubtle)
	s.ComposerFooter.Resets = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.ComposerFooter.Separator = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.ComposerFooter.Skill = lipgloss.NewStyle().Foreground(o.plan).Bold(true)
	s.ComposerFooter.SkillRemove = lipgloss.NewStyle().Foreground(o.planMoreSubtle)

	// ModelInfo
	s.ModelInfo.Icon = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.ModelInfo.Name = lipgloss.NewStyle().Foreground(o.fgBase)
	s.ModelInfo.Provider = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.ModelInfo.ProviderFallback = lipgloss.NewStyle().Foreground(o.fgMoreSubtle).PaddingLeft(2)
	s.ModelInfo.Reasoning = lipgloss.NewStyle().Foreground(o.fgMostSubtle).PaddingLeft(2)
	s.ModelInfo.TokenCount = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.ModelInfo.TokenPercentage = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.ModelInfo.EstimatedUsagePrefix = s.ModelInfo.TokenPercentage
	s.ModelInfo.Cost = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.ModelInfo.HypercreditIcon = lipgloss.NewStyle().Foreground(o.secondary)
	s.ModelInfo.HypercreditText = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)

	// ResourceGroup
	s.Resource.DefaultTitleFg = o.fgMoreSubtle
	s.Resource.DefaultDescFg = o.fgMostSubtle
}

// quickStyleChat sets the chat message, ANSI palette, shell, plan, thinking
// and selection styles.
func quickStyleChat(s *Styles, o quickStyleOpts, base, muted, subtle lipgloss.Style) {
	// Chat
	messageFocussedBorder := lipgloss.Border{
		Left: "▌",
	}

	s.Messages.NoContent = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Messages.UserBlurred = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.bgLeastVisible)
	s.Messages.UserFocused = lipgloss.NewStyle().Foreground(o.primary).Background(o.bgLeastVisible).Bold(true)
	s.Messages.UserBackground = o.bgLeastVisible
	s.Messages.AssistantBlurred = s.Messages.NoContent.PaddingLeft(2)
	s.Messages.AssistantFocused = s.Messages.NoContent.PaddingLeft(1).BorderLeft(true).
		BorderForeground(o.successMostSubtle).BorderStyle(messageFocussedBorder)
	s.Messages.AssistantBullet = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Messages.Thinking = lipgloss.NewStyle().MaxHeight(10)
	s.Messages.ErrorTag = lipgloss.NewStyle().Padding(0, 1).
		Background(o.destructive).Foreground(o.onPrimary)
	s.Messages.ErrorTitle = lipgloss.NewStyle().Foreground(o.fgSubtle)
	s.Messages.ErrorDetails = lipgloss.NewStyle().Foreground(o.fgMostSubtle)

	// Message item styles
	s.Messages.ToolCallFocused = muted.PaddingLeft(1).
		BorderStyle(messageFocussedBorder).
		BorderLeft(true).
		BorderForeground(o.successMostSubtle)
	s.Messages.ToolCallBlurred = muted.PaddingLeft(2)
	// No padding or border for compact tool calls within messages
	s.Messages.ToolCallCompact = muted

	// ANSI 16-color palette (indices 0-7 normal, 8-15 bright). Used to
	// remap raw terminal color codes in command output onto legible
	// colors. See [Styles.ANSI].
	s.ANSI = [16]color.Color{
		o.ansiBlack, o.ansiRed, o.ansiGreen, o.ansiYellow,
		o.ansiBlue, o.ansiMagenta, o.ansiCyan, o.ansiWhite,
		o.ansiBrightBlack, o.ansiBrightRed, o.ansiBrightGreen, o.ansiBrightYellow,
		o.ansiBrightBlue, o.ansiBrightMagenta, o.ansiBrightCyan, o.ansiBrightWhite,
	}

	// Shell (bang mode) item styles.
	s.Messages.ShellBarFocused = lipgloss.NewStyle().PaddingLeft(1).
		BorderStyle(messageFocussedBorder).BorderLeft(true).
		BorderForeground(o.primary)
	s.Messages.ShellBarBlurred = lipgloss.NewStyle().PaddingLeft(1).BorderLeft(true).
		BorderForeground(o.bgMostVisible).BorderStyle(lipgloss.NormalBorder())
	s.Messages.ShellPrompt = base.Foreground(o.primary).Bold(true)
	s.Messages.ShellPromptBlurred = base.Foreground(o.fgMoreSubtle)
	s.Messages.ShellCommand = base.Foreground(o.fgBase)
	s.Messages.ShellOutput = lipgloss.NewStyle().Foreground(o.fgSubtle)
	s.Messages.ShellExitCode = lipgloss.NewStyle().Foreground(o.destructive)
	s.Messages.ShellTruncation = muted

	s.Messages.SectionHeader = base.PaddingLeft(2)
	s.Messages.AssistantInfoIcon = subtle
	s.Messages.AssistantInfoModel = muted
	s.Messages.AssistantInfoProvider = subtle
	s.Messages.AssistantInfoDuration = subtle
	s.Messages.SubduedHypercreditIcon = subtle
	s.Messages.AssistantCanceled = lipgloss.NewStyle().Foreground(o.fgSubtle).Italic(true)

	// Plan section styles. The plan card is a bordered box with no background
	// fill: the terminal background shows through, and only intentional chips
	// (inline code, H1 badge) keep a background of their own.
	s.Messages.PlanBox = lipgloss.NewStyle().
		Foreground(o.fgBase).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(o.plan).
		Padding(1, 2)

	// Thinking section styles
	s.Messages.ThinkingBox = subtle.Background(o.bgLeastVisible)
	s.Messages.ThinkingTruncationHint = muted
	s.Messages.ThinkingFooterTitle = muted
	s.Messages.ThinkingFooterDuration = subtle

	// Text selection.
	s.TextSelection = lipgloss.NewStyle().Foreground(o.onPrimary).Background(o.primary)
}
