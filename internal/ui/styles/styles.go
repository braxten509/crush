// Package styles define styling and theming for the project.
package styles

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log/slog"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/crush/internal/ui/diffview"
)

const (
	CheckIcon       string = "✓"
	SpinnerIcon     string = "⋯"
	LoadingIcon     string = "⟳"
	ModelIcon       string = "◇"
	HypercreditIcon string = "◆"

	ArrowRightIcon string = "→"

	ToolPending string = "●"
	ToolSuccess string = "✓"
	ToolError   string = "×"

	RadioOn  string = "◉"
	RadioOff string = "○"

	BorderThin  string = "│"
	BorderThick string = "▌"

	SectionSeparator string = "─"

	TodoCompletedIcon  string = "✓"
	TodoPendingIcon    string = "•"
	TodoInProgressIcon string = "→"

	ImageIcon  string = "■"
	TextIcon   string = "≡"
	SkillIcon  string = "▲"
	RemoveIcon string = "✕"

	ColorSwatchIcon string = "■"

	ScrollbarThumb string = "┃"
	ScrollbarTrack string = "│"

	LSPErrorIcon   string = "E"
	LSPWarningIcon string = "W"
	LSPInfoIcon    string = "I"
	LSPHintIcon    string = "H"
)

const (
	defaultMargin     = 2
	defaultListIndent = 2
)

type Styles struct {
	// ANSI holds the 16 standard ANSI colors (0-7 normal, 8-15 bright)
	// used to remap legible colors onto raw terminal output, such as the
	// output of bang-mode shell commands. Terminal programs emit the
	// basic 16-color SGR codes (red, green, blue, …) and leave the actual
	// colors up to the terminal; without this palette they fall through
	// to the user's terminal defaults, which are often illegible on
	// Crush's background. Defining them here keeps output readable and
	// on-brand regardless of terminal configuration.
	ANSI [16]color.Color

	// Header
	Header HeaderStyles

	CompactDetails CompactDetailsStyles

	// Tool calls
	ToolCallSuccess lipgloss.Style

	// Text selection
	TextSelection lipgloss.Style

	// Markdown & Chroma
	Markdown      ansi.StyleConfig
	QuietMarkdown ansi.StyleConfig
	PlanMarkdown  ansi.StyleConfig
	// QuestionMarkdown renders question descriptions, dimmed.
	QuestionMarkdown ansi.StyleConfig

	// Inputs
	TextInput textinput.Styles

	// Help
	Help help.Styles

	// Diff
	Diff diffview.Style

	// FilePicker
	FilePicker filepicker.Styles

	// Buttons
	Button ButtonStyles

	// Editor
	Editor EditorStyles

	// Radio
	Radio RadioStyles

	// Tabs for batch question forms. Uses uv types for direct
	// screen rendering without lipgloss.
	Tab TabStyles

	// Background
	Background color.Color

	// Logo
	Logo LogoStyles

	// Working indicator gradient (spinners/shimmers on assistant "thinking",
	// tool-call pending, CLI generating, startup).
	WorkingGradFromColor color.Color
	WorkingGradToColor   color.Color
	WorkingLabelColor    color.Color // Label text color next to the indicator
	WorkingTimerColor    color.Color // Elapsed timer suffix color

	// Section Title
	Section SectionStyles

	// Initialize
	Initialize InitializeStyles

	// LSP
	LSP LSPStyles

	// Sidebar
	Sidebar SidebarStyles

	// ComposerFooter is the compact model, context, quota, and return row.
	ComposerFooter ComposerFooterStyles

	// ModelInfo (model name, provider, reasoning, token/cost summary).
	ModelInfo ModelInfoStyles

	// Resource styles the LSP/MCP/skills sidebar lists: their heading,
	// each row's status icon, name, status text, and truncation hints.
	Resource ResourceStyles

	// Files
	Files FilesStyles

	// Chat
	// Messages - chat message item styles
	Messages MessagesStyles

	// Tool - styles for tool call rendering
	Tool ToolStyles

	// Dialog styles
	Dialog DialogStyles

	// Status bar and help
	Status StatusStyles

	// Completions popup styles
	Completions CompletionsStyles

	// Attachments styles
	Attachments AttachmentStyles

	// Pills styles for todo/queue pills
	Pills PillsStyles
}

// ChromaTheme converts the current markdown chroma styles to a chroma
// StyleEntries map.
func (s *Styles) ChromaTheme() chroma.StyleEntries {
	rules := s.Markdown.CodeBlock

	return chroma.StyleEntries{
		chroma.Text:                chromaStyle(rules.Chroma.Text),
		chroma.Error:               chromaStyle(rules.Chroma.Error),
		chroma.Comment:             chromaStyle(rules.Chroma.Comment),
		chroma.CommentPreproc:      chromaStyle(rules.Chroma.CommentPreproc),
		chroma.Keyword:             chromaStyle(rules.Chroma.Keyword),
		chroma.KeywordReserved:     chromaStyle(rules.Chroma.KeywordReserved),
		chroma.KeywordNamespace:    chromaStyle(rules.Chroma.KeywordNamespace),
		chroma.KeywordType:         chromaStyle(rules.Chroma.KeywordType),
		chroma.Operator:            chromaStyle(rules.Chroma.Operator),
		chroma.Punctuation:         chromaStyle(rules.Chroma.Punctuation),
		chroma.Name:                chromaStyle(rules.Chroma.Name),
		chroma.NameBuiltin:         chromaStyle(rules.Chroma.NameBuiltin),
		chroma.NameTag:             chromaStyle(rules.Chroma.NameTag),
		chroma.NameAttribute:       chromaStyle(rules.Chroma.NameAttribute),
		chroma.NameClass:           chromaStyle(rules.Chroma.NameClass),
		chroma.NameConstant:        chromaStyle(rules.Chroma.NameConstant),
		chroma.NameDecorator:       chromaStyle(rules.Chroma.NameDecorator),
		chroma.NameException:       chromaStyle(rules.Chroma.NameException),
		chroma.NameFunction:        chromaStyle(rules.Chroma.NameFunction),
		chroma.NameOther:           chromaStyle(rules.Chroma.NameOther),
		chroma.Literal:             chromaStyle(rules.Chroma.Literal),
		chroma.LiteralNumber:       chromaStyle(rules.Chroma.LiteralNumber),
		chroma.LiteralDate:         chromaStyle(rules.Chroma.LiteralDate),
		chroma.LiteralString:       chromaStyle(rules.Chroma.LiteralString),
		chroma.LiteralStringEscape: chromaStyle(rules.Chroma.LiteralStringEscape),
		chroma.GenericDeleted:      chromaStyle(rules.Chroma.GenericDeleted),
		chroma.GenericEmph:         chromaStyle(rules.Chroma.GenericEmph),
		chroma.GenericInserted:     chromaStyle(rules.Chroma.GenericInserted),
		chroma.GenericStrong:       chromaStyle(rules.Chroma.GenericStrong),
		chroma.GenericSubheading:   chromaStyle(rules.Chroma.GenericSubheading),
		chroma.Background:          chromaStyle(rules.Chroma.Background),
	}
}

// AttachmentStyles are the styles for one attachment chip row. They are
// passed to the attachments renderer as a unit so adding a style does not
// mean touching every construction site.
type AttachmentStyles struct {
	Normal lipgloss.Style
	Image  lipgloss.Style
	Text   lipgloss.Style
	Skill  lipgloss.Style
	// KeptIcon and KeptName draw a skill kept on for the whole chat. They
	// use the plan colors so they stand apart from one-off attachments.
	KeptIcon lipgloss.Style
	KeptName lipgloss.Style
	// KeptDot and KeptText mark a skill that is on in the "Skills for
	// this chat" list.
	KeptDot  lipgloss.Style
	KeptText lipgloss.Style
	Remove   lipgloss.Style
	Deleting lipgloss.Style
	More     lipgloss.Style // "N more…" hint for chips that didn't fit
}

// DialogHelpStyles returns the styles for dialog help.
func (s *Styles) DialogHelpStyles() help.Styles {
	return help.Styles(s.Dialog.Help)
}

// Clone returns a deep copy of the Styles struct, ensuring pointer fields
// (particularly within ansi.StyleConfig) are not aliased. This is used to
// safely snapshot styles before a theme preview.
func (s *Styles) Clone() Styles {
	clone := *s
	clone.Markdown = cloneStyleConfig(s.Markdown)
	clone.QuietMarkdown = cloneStyleConfig(s.QuietMarkdown)
	clone.QuestionMarkdown = cloneStyleConfig(s.QuestionMarkdown)
	return clone
}

// cloneStyleConfig deep-copies an ansi.StyleConfig by JSON round-tripping.
//
// NOTE: This assumes ansi.StyleConfig remains fully JSON-round-trippable.
// If the glamour library adds non-serializable fields, this approach will
// need to be replaced with a reflect-based deep copy.
func cloneStyleConfig(src ansi.StyleConfig) ansi.StyleConfig {
	data, err := json.Marshal(src)
	if err != nil {
		slog.Error("Failed to marshal markdown styles for theme preview", "error", err)
		return src
	}
	var dst ansi.StyleConfig
	if err := json.Unmarshal(data, &dst); err != nil {
		slog.Error("Failed to unmarshal markdown styles for theme preview", "error", err)
		return src
	}
	return dst
}

// hex returns a pointer to the "#rrggbb" representation of c. It's used to
// satisfy glamour's string-pointer API when configuring markdown colors
// from the theme palette.
func hex(c color.Color) *string {
	r, g, b, _ := c.RGBA()
	s := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	return &s
}

func chromaStyle(style ansi.StylePrimitive) string {
	var s strings.Builder

	if style.Color != nil {
		s.WriteString(*style.Color)
	}
	if style.BackgroundColor != nil {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("bg:")
		s.WriteString(*style.BackgroundColor)
	}
	if style.Italic != nil && *style.Italic {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("italic")
	}
	if style.Bold != nil && *style.Bold {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("bold")
	}
	if style.Underline != nil && *style.Underline {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("underline")
	}

	return s.String()
}
