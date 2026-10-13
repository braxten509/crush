package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// DialogStyles groups the Dialog styles of [Styles].
type DialogStyles struct {
	Title              lipgloss.Style
	TitleText          lipgloss.Style
	TitleError         lipgloss.Style
	TitleAccent        lipgloss.Style
	TitleLineBase      lipgloss.Style // Base for the gradient ╱╱╱ next to dialog titles
	TitleGradFromColor color.Color    // Default dialog title ╱╱╱ gradient start
	TitleGradToColor   color.Color    // Default dialog title ╱╱╱ gradient end
	// View is the main content area style.
	View          lipgloss.Style
	PrimaryText   lipgloss.Style
	SecondaryText lipgloss.Style
	// HelpView is the line that contains the help.
	HelpView lipgloss.Style
	Help     struct {
		Ellipsis       lipgloss.Style
		ShortKey       lipgloss.Style
		ShortDesc      lipgloss.Style
		ShortSeparator lipgloss.Style
		FullKey        lipgloss.Style
		FullDesc       lipgloss.Style
		FullSeparator  lipgloss.Style
	}

	NormalItem   lipgloss.Style
	SelectedItem lipgloss.Style
	InputPrompt  lipgloss.Style

	List lipgloss.Style

	Spinner lipgloss.Style

	// ContentPanel is used for content blocks with subtle background.
	ContentPanel   lipgloss.Style
	ContentPanelBg color.Color // Background color for ContentPanel syntax highlighting.

	// Scrollbar styles for scrollable content.
	ScrollbarThumb lipgloss.Style
	ScrollbarTrack lipgloss.Style

	// Arguments
	Arguments struct {
		Content                  lipgloss.Style
		Description              lipgloss.Style
		InputLabelBlurred        lipgloss.Style
		InputLabelFocused        lipgloss.Style
		InputRequiredMarkBlurred lipgloss.Style
		InputRequiredMarkFocused lipgloss.Style
	}

	// ListItem styles the info-text rendered alongside list items (commands,
	// models, reasoning options). Sessions have their own overrides below.
	ListItem struct {
		InfoBlurred lipgloss.Style
		InfoFocused lipgloss.Style
	}

	Models struct {
		ConfiguredText lipgloss.Style // "Configured" badge shown on the ModelGroup header
	}

	Permissions struct {
		KeyText   lipgloss.Style // Left key cell of a key/value row
		ValueText lipgloss.Style // Right value cell of a key/value row
		ParamsBg  color.Color    // Background color behind highlighted JSON parameters
	}

	Quit struct {
		Content lipgloss.Style // Wrapper for the quit dialog's inner content
		Hint    lipgloss.Style // Style for quit hint
		Frame   lipgloss.Style // Outer rounded border framing the quit dialog
	}

	APIKey struct {
		Spinner lipgloss.Style // Loading spinner while validating the key
	}

	// AuthMethod styles the OAuth-vs-API-key choice dialog.
	AuthMethod struct {
		Prompt      lipgloss.Style // "How would you like to authenticate?" question line
		CardBlurred lipgloss.Style // Unselected choice card frame and label
		CardFocused lipgloss.Style // Selected choice card frame and label
	}

	OAuth struct {
		Spinner      lipgloss.Style // Loading spinner
		Instructions lipgloss.Style // Emphasized instruction text
		UserCode     lipgloss.Style // Prominent user code display
		Success      lipgloss.Style // Positive status text (e.g. "Authentication successful!")
		Link         lipgloss.Style // Underlined verification URL
		Enter        lipgloss.Style // "enter" keyword highlight in instructions
		ErrorText    lipgloss.Style // Error message when authentication fails
		StatusText   lipgloss.Style // Narrative status text ("Initializing...", "Verifying...", etc.)
		UserCodeBg   color.Color    // Background color of the centered user-code box
	}

	ImagePreview lipgloss.Style

	Sessions struct {
		// styles for when we are in delete mode
		DeletingView                   lipgloss.Style
		DeletingItemFocused            lipgloss.Style
		DeletingItemBlurred            lipgloss.Style
		DeletingTitle                  lipgloss.Style
		DeletingMessage                lipgloss.Style
		DeletingTitleGradientFromColor color.Color
		DeletingTitleGradientToColor   color.Color

		// styles for when we are in update mode
		RenamingView                   lipgloss.Style
		RenamingingItemFocused         lipgloss.Style
		RenamingItemBlurred            lipgloss.Style
		RenamingingTitle               lipgloss.Style
		RenamingingMessage             lipgloss.Style
		RenamingTitleGradientFromColor color.Color
		RenamingTitleGradientToColor   color.Color
		RenamingPlaceholder            lipgloss.Style

		InfoBlurred lipgloss.Style // Timestamp text on unfocused session items
		InfoFocused lipgloss.Style // Timestamp text on the focused session item
	}
}
