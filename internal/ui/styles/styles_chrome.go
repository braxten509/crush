package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// HeaderStyles groups the Header styles of [Styles].
type HeaderStyles struct {
	Charm             lipgloss.Style // Style for "Charm™" label
	Diagonals         lipgloss.Style // Style for diagonal separators (╱)
	Percentage        lipgloss.Style // Style for context percentage
	HypercreditIcon   lipgloss.Style // Style for Hypercredit count (◆ N)
	Keystroke         lipgloss.Style // Style for keystroke hints (e.g., "ctrl+d")
	KeystrokeTip      lipgloss.Style // Style for keystroke action text (e.g., "open", "close")
	WorkingDir        lipgloss.Style // Style for current working directory
	GitBranch         lipgloss.Style // Style for the current git branch name
	Separator         lipgloss.Style // Style for separator dots (•)
	Wrapper           lipgloss.Style // Outer container for the entire header row
	LogoGradCanvas    lipgloss.Style // Canvas for the compact "CRUSH" gradient
	LogoGradFromColor color.Color    // "CRUSH" wordmark gradient start
	LogoGradToColor   color.Color    // "CRUSH" wordmark gradient end
}

// CompactDetailsStyles groups the CompactDetails styles of [Styles].
type CompactDetailsStyles struct {
	View    lipgloss.Style
	Version lipgloss.Style
	Title   lipgloss.Style
}

// LogoStyles groups the Logo styles of [Styles].
type LogoStyles struct {
	FieldColor         color.Color
	TitleColorA        color.Color
	TitleColorB        color.Color
	CharmColor         color.Color
	VersionColor       color.Color
	SmallCharm         lipgloss.Style // "Charm™" label in SmallRender
	SmallDiagonals     lipgloss.Style // Diagonal line fill in SmallRender
	GradCanvas         lipgloss.Style // Blank canvas for gradient painting
	SmallGradFromColor color.Color    // Small "Crush" wordmark gradient start
	SmallGradToColor   color.Color    // Small "Crush" wordmark gradient end
	ShineColor         color.Color    // Light the wordmark's moving shine blends toward
	FlowColor          color.Color    // Third color of the wordmark's flowing gradient
}

// SectionStyles groups the Section styles of [Styles].
type SectionStyles struct {
	Title lipgloss.Style
	Line  lipgloss.Style
}

// InitializeStyles groups the Initialize styles of [Styles].
type InitializeStyles struct {
	Header  lipgloss.Style
	Content lipgloss.Style
	Accent  lipgloss.Style
}

// LSPStyles groups the LSP styles of [Styles].
type LSPStyles struct {
	ErrorDiagnostic   lipgloss.Style
	WarningDiagnostic lipgloss.Style
	HintDiagnostic    lipgloss.Style
	InfoDiagnostic    lipgloss.Style
}

// SidebarStyles groups the Sidebar styles of [Styles].
type SidebarStyles struct {
	SessionTitle lipgloss.Style // Current session title at top of sidebar
	WorkingDir   lipgloss.Style // Working directory path (PrettyPath)
}

// ComposerFooterStyles groups the ComposerFooter styles of [Styles].
type ComposerFooterStyles struct {
	Model  lipgloss.Style
	Text   lipgloss.Style
	Accent lipgloss.Style
	// Each stat has its own color so they stand apart, as in Codex.
	Tokens      lipgloss.Style
	Context     lipgloss.Style
	Limit5h     lipgloss.Style
	LimitWeekly lipgloss.Style
	LimitOther  lipgloss.Style
	Resets      lipgloss.Style
	Separator   lipgloss.Style
	// Skill and SkillRemove draw a skill kept on for the chat and the ×
	// that turns it off.
	Skill       lipgloss.Style
	SkillRemove lipgloss.Style
}

// ModelInfoStyles groups the ModelInfo styles of [Styles].
type ModelInfoStyles struct {
	Icon                 lipgloss.Style // Model icon (◇)
	Name                 lipgloss.Style // Model name text
	Provider             lipgloss.Style // "via <provider>" text
	ProviderFallback     lipgloss.Style // Provider on its own second line
	Reasoning            lipgloss.Style // Reasoning effort text
	TokenCount           lipgloss.Style // "(42K)" token count
	TokenPercentage      lipgloss.Style // "42%" percent of context window
	EstimatedUsagePrefix lipgloss.Style // "~" prefix for estimated usage
	Cost                 lipgloss.Style // "$0.42" cost readout
	HypercreditIcon      lipgloss.Style // Hypercredit icon (◆)
	HypercreditText      lipgloss.Style // Remaining Hypercredits text
}

// ResourceStyles groups the Resource styles of [Styles].
type ResourceStyles struct {
	Heading         lipgloss.Style // Section header ("LSPs", "MCPs", "Skills")
	Name            lipgloss.Style // Resource name (e.g. "gopls")
	StatusText      lipgloss.Style // Row status description (e.g. "starting...")
	OfflineIcon     lipgloss.Style // Offline/unstarted/stopped status icon
	DisabledIcon    lipgloss.Style // Disabled status icon
	BusyIcon        lipgloss.Style // Busy/starting status icon
	ErrorIcon       lipgloss.Style // Error status icon
	OnlineIcon      lipgloss.Style // Online/ready status icon
	OnlineText      lipgloss.Style // Online/ready status text (e.g. "connected"), no icon
	NeedsAuthIcon   lipgloss.Style // Needs authentication status icon
	AdditionalText  lipgloss.Style // "None" and "…and N more" text
	CapabilityCount lipgloss.Style // "N tools" / "N prompts" / "N resources"
	RowTitleBase    lipgloss.Style // Base style applied over row titles in common.Status
	RowDescBase     lipgloss.Style // Base style applied over row descriptions in common.Status
	DefaultTitleFg  color.Color    // Default title color when opt is zero
	DefaultDescFg   color.Color    // Default description color when opt is zero
}

// FilesStyles groups the Files styles of [Styles].
type FilesStyles struct {
	Path           lipgloss.Style
	Additions      lipgloss.Style
	Deletions      lipgloss.Style
	SectionTitle   lipgloss.Style // "Modified Files" heading
	EmptyMessage   lipgloss.Style // "None" placeholder when no files
	TruncationHint lipgloss.Style // "…and N more" message
}

// StatusStyles groups the Status styles of [Styles].
type StatusStyles struct {
	Help lipgloss.Style

	// Mode badges shown before the help hints.
	ModeBadgePlan   lipgloss.Style
	ModeBadgeYolo   lipgloss.Style
	ModeBadgeRemote lipgloss.Style // Remote Control is sharing the chat

	// Full-width banners shown when switching modes.
	ModeBannerPlan      lipgloss.Style
	ModeBannerPlanBadge lipgloss.Style
	ModeBannerYolo      lipgloss.Style
	ModeBannerYoloBadge lipgloss.Style

	ErrorIndicator   lipgloss.Style
	WarnIndicator    lipgloss.Style
	InfoIndicator    lipgloss.Style
	UpdateIndicator  lipgloss.Style
	SuccessIndicator lipgloss.Style

	ErrorMessage   lipgloss.Style
	WarnMessage    lipgloss.Style
	InfoMessage    lipgloss.Style
	UpdateMessage  lipgloss.Style
	SuccessMessage lipgloss.Style
}

// PillsStyles groups the Pills styles of [Styles].
type PillsStyles struct {
	Base               lipgloss.Style // Base pill style with padding
	Focused            lipgloss.Style // Pill with visible rounded border
	QueueItemPrefix    lipgloss.Style // "›" before a queued message
	QueueItemText      lipgloss.Style // Queued message text
	QueueItemHint      lipgloss.Style // "waiting · sends next" beside the first
	QueueRowBg         color.Color    // Queued messages' band, dimmer than the composer's
	QueueLabel         lipgloss.Style // "N Queued" label text
	QueueIconBase      lipgloss.Style // Base style for queue gradient triangles
	QueueGradFromColor color.Color    // Start color for queue indicator gradient
	QueueGradToColor   color.Color    // End color for queue indicator gradient
	TodoLabel          lipgloss.Style // "TO-DO" label
	TodoProgress       lipgloss.Style // Todo ratio (e.g. "2/5")
	TodoCurrentTask    lipgloss.Style // Current in-progress task name
	TodoSpinner        lipgloss.Style // Todo spinner style
	HelpKey            lipgloss.Style // Keystroke hint style
	HelpText           lipgloss.Style // Help action text style
	Area               lipgloss.Style // Pills area container
}
