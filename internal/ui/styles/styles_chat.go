package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// MessagesStyles groups the Messages styles of [Styles].
type MessagesStyles struct {
	// The user's messages sit on a full-width band; these style the
	// "›" marker on its first line.
	UserBlurred      lipgloss.Style
	UserFocused      lipgloss.Style
	UserBackground   color.Color // band behind the user's messages
	AssistantBlurred lipgloss.Style
	AssistantFocused lipgloss.Style
	NoContent        lipgloss.Style
	Thinking         lipgloss.Style
	ErrorTag         lipgloss.Style
	ErrorTitle       lipgloss.Style
	ErrorDetails     lipgloss.Style
	ToolCallFocused  lipgloss.Style
	ToolCallCompact  lipgloss.Style
	ToolCallBlurred  lipgloss.Style

	// AssistantBullet colors the "•" that opens each reply.
	AssistantBullet lipgloss.Style

	// Shell (bang mode) item styles.
	ShellBarFocused    lipgloss.Style // Left vertical bar when focused.
	ShellBarBlurred    lipgloss.Style // Left vertical bar when blurred.
	ShellPrompt        lipgloss.Style // "$" prompt symbol (focused).
	ShellPromptBlurred lipgloss.Style // "$" prompt symbol (blurred).
	ShellCommand       lipgloss.Style // Command text (syntax-highlighted).
	ShellOutput        lipgloss.Style // Plain output text.
	ShellExitCode      lipgloss.Style // Non-zero exit code indicator.
	ShellTruncation    lipgloss.Style // "N more lines" hint.
	SectionHeader      lipgloss.Style

	// Plan section styles
	PlanBox lipgloss.Style // Border+padding for the final plan message

	// Thinking section styles
	ThinkingBox            lipgloss.Style // Background for thinking content
	ThinkingTruncationHint lipgloss.Style // "… (N lines hidden)" hint
	ThinkingFooterTitle    lipgloss.Style // "Thought for" text
	ThinkingFooterDuration lipgloss.Style // Duration value
	AssistantInfoIcon      lipgloss.Style
	AssistantInfoModel     lipgloss.Style
	AssistantInfoProvider  lipgloss.Style
	AssistantInfoDuration  lipgloss.Style
	SubduedHypercreditIcon lipgloss.Style // Subdued ◆ for hypercredit figures within subdued text
	AssistantCanceled      lipgloss.Style // Italic "Canceled" footer
}

// ToolStyles groups the Tool styles of [Styles].
type ToolStyles struct {
	// Icon styles with tool status
	IconPending   lipgloss.Style
	IconSuccess   lipgloss.Style
	IconError     lipgloss.Style
	IconCancelled lipgloss.Style

	// Tool name styles
	NameNormal lipgloss.Style // Top-level tool name
	NameNested lipgloss.Style // Nested child tool name (inside Agent/Agentic Fetch)

	// Line counts of a tool group's file changes (+N −N)
	ChangesAdd lipgloss.Style
	ChangesDel lipgloss.Style

	// Parameter list styles
	ParamMain lipgloss.Style
	ParamKey  lipgloss.Style

	// Content rendering styles
	ContentLine           lipgloss.Style // Individual content line with background and width
	ContentTruncation     lipgloss.Style // Truncation message "… (N lines)"
	ContentCodeLine       lipgloss.Style // Code line with background and width
	ContentCodeTruncation lipgloss.Style // Code truncation message with bgBase
	ContentCodeBg         color.Color    // Background color for syntax highlighting
	Body                  lipgloss.Style // Body content padding (PaddingLeft(2))

	// Deprecated - kept for backward compatibility
	ContentBg         lipgloss.Style // Content background
	ContentText       lipgloss.Style // Content text
	ContentLineNumber lipgloss.Style // Line numbers in code

	// State message styles
	StateWaiting   lipgloss.Style // "Waiting for tool response..."
	StateCancelled lipgloss.Style // "Canceled."

	// Error styles
	ErrorTag     lipgloss.Style // ERROR tag
	ErrorMessage lipgloss.Style // Error message text

	// Warning styles (used for permission denied)
	WarnTag     lipgloss.Style // WARN tag
	WarnMessage lipgloss.Style // Warning message text

	// Diff styles
	DiffTruncation lipgloss.Style // Diff truncation message with padding

	// Multi-edit note styles
	NoteTag     lipgloss.Style // NOTE tag (yellow background)
	NoteMessage lipgloss.Style // Note message text

	// Job header styles (for bash jobs)
	JobIconPending lipgloss.Style // Pending job icon (green dark)
	JobIconError   lipgloss.Style // Error job icon (red dark)
	JobIconSuccess lipgloss.Style // Success job icon (green)
	JobToolName    lipgloss.Style // Job tool name "Bash" (blue)
	JobAction      lipgloss.Style // Action text (Start, Output, Kill)
	JobPID         lipgloss.Style // PID text
	JobDescription lipgloss.Style // Description text

	// Agent task styles
	AgentTaskTag lipgloss.Style // Agent task tag (blue background, bold)
	AgentPrompt  lipgloss.Style // Agent prompt text

	// Agentic fetch styles
	AgenticFetchPromptTag lipgloss.Style // Agentic fetch prompt tag (green background, bold)

	// Todo styles
	TodoRatio          lipgloss.Style // Todo ratio (e.g., "2/5")
	TodoCompletedIcon  lipgloss.Style // Completed todo icon
	TodoInProgressIcon lipgloss.Style // In-progress todo icon
	TodoPendingIcon    lipgloss.Style // Pending todo icon
	TodoStatusNote     lipgloss.Style // " · completed N" / " · starting task" trailing note
	TodoItem           lipgloss.Style // Default body text for todo list items
	TodoJustStarted    lipgloss.Style // Text of the just-started todo in tool-call bodies

	// MCP tools
	MCPName     lipgloss.Style // The mcp name
	MCPToolName lipgloss.Style // The mcp tool name
	MCPArrow    lipgloss.Style // The mcp arrow icon

	// Images and external resources
	ResourceLoadedText      lipgloss.Style
	ResourceLoadedIndicator lipgloss.Style
	ResourceName            lipgloss.Style
	ResourceSize            lipgloss.Style
	MediaType               lipgloss.Style

	// Hooks
	HookLabel        lipgloss.Style // "Hook" label
	HookName         lipgloss.Style // Hook command name
	HookMatcher      lipgloss.Style // Matcher regex pattern
	HookArrow        lipgloss.Style // Arrow indicator
	HookDetail       lipgloss.Style // Decision detail text
	HookOK           lipgloss.Style // "OK" status
	HookDenied       lipgloss.Style // "Denied" status
	HookDeniedLabel  lipgloss.Style // "Hook" label when denied
	HookDeniedReason lipgloss.Style // Denied reason text
	HookRewrote      lipgloss.Style // "Rewrote Input" indicator

	// Action verb colors for tool-call headers.
	ActionCreate  lipgloss.Style // Constructive actions (e.g. "Add", "Create")
	ActionDestroy lipgloss.Style // Destructive actions (e.g. "Remove", "Delete")

	// Tool result helpers.
	ResultEmpty      lipgloss.Style // "No results" placeholder
	ResultTruncation lipgloss.Style // "… and N more" truncation line
	ResultItemName   lipgloss.Style // Item name (left column in result lists)
	ResultItemDesc   lipgloss.Style // Item description (right column)
}
