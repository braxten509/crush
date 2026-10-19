package login

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/oauth"
)

// Flow is a platform's sign-in flow for a caller that drives it itself,
// such as the engine's sign-in routes (desktop design 5-6, 2.4; G-5).
// Start is called once; Wait then blocks until the user finishes in the
// browser; Close releases what the flow holds.
type Flow interface {
	Start(ctx context.Context) (url string, userCode string, err error)
	Wait(ctx context.Context) (*oauth.Token, error)
	Close()
}

// CodeFlow is a Flow whose browser sign-in can also finish with a code
// pasted from the provider's page.
type CodeFlow interface {
	Flow
	// TakesCode reports whether the started flow takes a pasted code.
	TakesCode() bool
	CompleteWithCode(ctx context.Context, input string) (*oauth.Token, error)
}

// ErrNoCodeEntry is returned for a code sent to a flow that takes none.
var ErrNoCodeEntry = errors.New("no browser flow in progress")

// NewFlow returns a new sign-in flow for platform (PlatformHyper,
// PlatformCopilot, PlatformOpenAI or PlatformGrok), the one `crush login`
// runs.
func NewFlow(platform string) (Flow, error) {
	newFlow, err := flowFor(platform)
	if err != nil {
		return nil, err
	}
	return newFlow(), nil
}

// TakesCode reports whether f, once started, can finish with a pasted
// code: Grok's browser flow can, its device fallback and the others can't.
func TakesCode(f Flow) bool {
	c, ok := f.(CodeFlow)
	return ok && c.TakesCode()
}

// CompleteWithCode finishes f with a code pasted from the provider's page.
func CompleteWithCode(ctx context.Context, f Flow, input string) (*oauth.Token, error) {
	c, ok := f.(CodeFlow)
	if !ok || !c.TakesCode() {
		return nil, ErrNoCodeEntry
	}
	return c.CompleteWithCode(ctx, input)
}

// TakesCode reports whether the Grok browser flow runs, rather than the
// device fallback.
func (f *grokFlow) TakesCode() bool { return f.f != nil }

// ProviderID is the provider a platform's sign-in saves its token under,
// as `crush login` saves it.
func ProviderID(platform string) string {
	if platform == PlatformGrok {
		return "xai"
	}
	return platform
}
