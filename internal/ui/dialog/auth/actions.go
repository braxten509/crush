// Package auth holds the dialogs that authenticate a provider: choosing a
// method, entering an API key, OAuth sign-in flows and AWS SSO.
package auth

import (
	"github.com/charmbracelet/crush/internal/oauth"
)

// Messages for API key input dialog.
type (
	ActionChangeAPIKeyState struct {
		State APIKeyInputState
	}
)

// Messages for OAuth2 device flow dialog.
type (
	// ActionInitiateOAuth is sent when the device auth is initiated
	// successfully.
	ActionInitiateOAuth struct {
		DeviceCode      string
		UserCode        string
		ExpiresIn       int
		VerificationURL string
		Interval        int

		// flowState carries provider-private state (a browser flow or a
		// device code) from the initiating command back to the update
		// loop, so the command never writes to the provider itself.
		flowState any
	}

	// ActionCompleteOAuth is sent when the device flow completes successfully.
	ActionCompleteOAuth struct {
		Token *oauth.Token
	}

	// ActionOAuthErrored is sent when the device flow encounters an error.
	ActionOAuthErrored struct {
		Error error
	}
)
