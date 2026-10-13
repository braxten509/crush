package styles

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// ButtonStyles groups the Button styles of [Styles].
type ButtonStyles struct {
	Focused lipgloss.Style
	Blurred lipgloss.Style
	// Inactive styles buttons of a prompt that is not in the
	// active pane: slightly lighter than Blurred so the choices
	// stay legible while the chat has focus.
	Inactive lipgloss.Style
	Hovered  lipgloss.Style
	Negative lipgloss.Style // Selected negative/destructive action.
}

// EditorStyles groups the Editor styles of [Styles].
type EditorStyles struct {
	Textarea textarea.Styles

	// Normal mode prompt ("›" on the first line, blank after).
	PromptNormalIconFocused lipgloss.Style
	PromptNormalIconBlurred lipgloss.Style
	PromptNormalFocused     lipgloss.Style
	PromptNormalBlurred     lipgloss.Style

	// Plan mode prompt.
	PromptPlanIconFocused lipgloss.Style
	PromptPlanIconBlurred lipgloss.Style
	PromptPlanDotsFocused lipgloss.Style
	PromptPlanDotsBlurred lipgloss.Style

	// YOLO mode prompt.
	PromptYoloIconFocused lipgloss.Style
	PromptYoloIconBlurred lipgloss.Style
	PromptYoloDotsFocused lipgloss.Style
	PromptYoloDotsBlurred lipgloss.Style

	// Bang mode prompt ("!" on the composer band).
	PromptBangIconFocused lipgloss.Style
	PromptBangIconBlurred lipgloss.Style
	PromptBangDotsFocused lipgloss.Style
	PromptBangDotsBlurred lipgloss.Style

	// Question mode prompt (" ? " badge in question forms).
	PromptQuestionIconFocused lipgloss.Style
	PromptQuestionIconBlurred lipgloss.Style

	// Question choice styling.
	QuestionSelected   lipgloss.Style // Active choice text (Dolly).
	QuestionUnselected lipgloss.Style // Inactive header text (Sash).
	QuestionBody       lipgloss.Style // Description/body text.
	QuestionConfirm    lipgloss.Style // Confirm tab title (primary).
	QuestionNote       lipgloss.Style // Saved note text (dimmer than body).
	QuestionCursorBar  lipgloss.Style // Active cursor indicator bar.
	QuestionRadioOn    lipgloss.Style // Selected single-choice radio.
	QuestionRadioOff   lipgloss.Style // Unselected single-choice radio.
	QuestionCheckOn    lipgloss.Style // Checked multi-choice indicator.
	QuestionCheckOff   lipgloss.Style // Unchecked multi-choice indicator.
}

// RadioStyles groups the Radio styles of [Styles].
type RadioStyles struct {
	On    lipgloss.Style
	Off   lipgloss.Style
	Label lipgloss.Style // Text next to a radio button
}

// TabStyles groups the Tab styles of [Styles].
type TabStyles struct {
	ActiveBorder          uv.Border
	InactiveBorder        uv.Border
	ActiveBorderBlurred   uv.Border
	InactiveBorderBlurred uv.Border
	ActiveStyle           uv.Style
	InactiveStyle         uv.Style
}

// CompletionsStyles groups the Completions styles of [Styles].
type CompletionsStyles struct {
	Normal  lipgloss.Style
	Focused lipgloss.Style
	Match   lipgloss.Style
}
