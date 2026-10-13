package dialog

// Identifiers of the dialogs that live in subpackages. They are declared
// here so the overlay's callers and the commands palette can refer to a
// dialog without importing its package.
const (
	// AuthMethodID is the identifier for the auth method selection dialog.
	AuthMethodID = "auth_method"

	// APIKeyInputID is the identifier for the API key input dialog.
	APIKeyInputID = "api_key_input"

	// AWSSSOID is the identifier for the AWS SSO auth dialog.
	AWSSSOID = "aws_sso"

	// OAuthID is the identifier for the OAuth sign-in dialog.
	OAuthID = "oauth"

	// ThemeID is the identifier for the theme management dialog.
	ThemeID = "theme"

	// ThemeEditorID is the identifier for the theme palette editor dialog.
	ThemeEditorID = "theme_editor"

	// ThemeNewID is the identifier for the new theme dialog.
	ThemeNewID = "theme_new"

	// ModelsID is the identifier for the model selection dialog.
	ModelsID = "models"
)
