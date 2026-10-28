package proto

// Sign-in routes for the desktop app (desktop design 5-6, 2.4; G-5).

// AuthFlowRequest starts a sign-in flow.
type AuthFlowRequest struct {
	// Platform is "hyper", "copilot", "openai" or "grok".
	Platform string `json:"platform"`
}

// AuthFlow is a started sign-in flow.
type AuthFlow struct {
	FlowID   string `json:"flow_id"`
	URL      string `json:"url"`
	UserCode string `json:"user_code"`
	// CodeEntry is set when the flow can also finish with a code pasted
	// from the provider's page (Grok's browser flow).
	CodeEntry bool `json:"code_entry"`
}

// AuthCodeRequest sends a code pasted from the provider's page.
type AuthCodeRequest struct {
	Code string `json:"code"`
}

// Sign-in flow states.
const (
	AuthFlowWaiting = "waiting"
	AuthFlowSaving  = "saving"
	AuthFlowDone    = "done"
	AuthFlowFailed  = "failed"
)

// AuthFlowEvent reports a sign-in flow's state.
type AuthFlowEvent struct {
	FlowID string `json:"flow_id"`
	// State is "waiting", "saving", "done" or "failed".
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// VerifyKeyRequest asks whether an API key works with a provider.
type VerifyKeyRequest struct {
	ProviderID string `json:"provider_id"`
	APIKey     string `json:"api_key"`
}

// VerifyKey answers a VerifyKeyRequest.
type VerifyKey struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// AuthStateResponse says, by provider id, whether an agent CLI holds its own
// login on this machine. Only the CLIs that sign in themselves are listed:
// Grok, Antigravity, OpenCode Go and OpenCode Zen. Emails holds the email
// a signed-in CLI's login uses, where the CLI keeps one (Antigravity).
type AuthStateResponse struct {
	Providers map[string]bool   `json:"providers"`
	Emails    map[string]string `json:"emails,omitempty"`
}
