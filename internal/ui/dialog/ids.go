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

	// SessionsID is the identifier for the session selector dialog.
	SessionsID = "session"

	// RemoteID is the identifier for the Remote Control dialog.
	RemoteID = "remote"

	// RemoteSetupID is the identifier for the Remote Control setup dialog.
	RemoteSetupID = "remote_setup"

	// MCPAuthID is the identifier for the MCP authentication dialog.
	MCPAuthID = "mcp_auth"

	// MCPTogglesID is the identifier for the MCP toggles dialog.
	MCPTogglesID = "mcp_toggles"

	// PermissionsID is the identifier for the permissions dialog.
	PermissionsID = "permissions"
)
