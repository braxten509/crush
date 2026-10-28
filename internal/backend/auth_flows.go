package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/config/providers"
	"github.com/charmbracelet/crush/internal/login"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// Sign-in for the desktop app (desktop design 5-6, 2.4; G-5). The engine
// runs `crush login`'s flows (internal/login) and saves the token as it
// does, with SetProviderAPIKey, which also fetches the account's models.
// Neither keys nor tokens are ever logged, and the routes answer on the
// engine's unix socket only.

var (
	// ErrAuthFlowNotFound is a sign-in flow that is unknown or over.
	ErrAuthFlowNotFound = errors.New("sign-in flow not found")
	// ErrAuthPlatform is a platform with no sign-in flow.
	ErrAuthPlatform = errors.New("unknown platform")
	// ErrAuthNoCode is a code sent to a flow that takes none.
	ErrAuthNoCode = errors.New("this sign-in takes no code")
	// ErrAuthStart is a sign-in that could not start.
	ErrAuthStart = errors.New("sign-in start")
	// ErrAuthFlowSaving is a cancel that came after the token was taken.
	ErrAuthFlowSaving = errors.New("the sign-in is already saving")
	// ErrAuthProvider is a provider the key check does not know.
	ErrAuthProvider = errors.New("provider not found")
)

// authFlowTimeout bounds a sign-in, as `crush login` does; the crushtest
// seams shorten it.
var authFlowTimeout = 10 * time.Minute

// SetAuthTimeoutForTest shortens the sign-in time limit and returns a
// function that restores it.
func SetAuthTimeoutForTest(d time.Duration) func() {
	prev := authFlowTimeout
	authFlowTimeout = d
	return func() { authFlowTimeout = prev }
}

// hyperMinimumWait is how long Hyper's start takes at least, so its
// "Initializing..." is seen (internal/ui/dialog/auth/oauth_hyper.go:36).
var hyperMinimumWait = 750 * time.Millisecond

// newAuthFlow makes a platform's flow; tests and the crushtest seams
// replace it.
var newAuthFlow = login.NewFlow

// verifyAPIKey tests key against provider as the terminal's API key
// sheet does; tests and the crushtest seams replace it. Its error holds
// only trusted text: see [trustedKeyError].
var verifyAPIKey = func(provider config.ProviderConfig, resolver config.VariableResolver) error {
	return trustedKeyError(provider.ID, provider.TestConnection(resolver))
}

// trustedKeyError rebuilds a key check's error from what Crush itself
// knows, in the terminal's wording, so nothing the provider sent (its
// status line's reason or its body) and nothing of the request (a key in
// its address) reaches an app: a refusal is its status code and Go's text
// for it, a format check keeps Crush's own text, and any other failure is
// the terminal's request failure without its cause.
func trustedKeyError(providerID string, err error) error {
	var status *providers.StatusError
	var format *providers.KeyFormatError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &status):
		return fmt.Errorf("failed to connect to provider %s: %s", providerID,
			strings.TrimSpace(fmt.Sprintf("%d %s", status.StatusCode, http.StatusText(status.StatusCode))))
	case errors.As(err, &format):
		return format
	default:
		return fmt.Errorf("failed to create request for provider %s", providerID)
	}
}

// SetAuthForTest replaces the sign-in flows and the key check; nil keeps
// the real one. It returns a function that restores both.
func SetAuthForTest(flows func(platform string) (login.Flow, error), verify func(providerID, apiKey string) error) func() {
	prevFlows, prevVerify := newAuthFlow, verifyAPIKey
	if flows != nil {
		newAuthFlow = flows
	}
	if verify != nil {
		verifyAPIKey = func(p config.ProviderConfig, _ config.VariableResolver) error { return verify(p.ID, p.APIKey) }
	}
	return func() { newAuthFlow, verifyAPIKey = prevFlows, prevVerify }
}

// authFlow is a running sign-in.
type authFlow struct {
	id, workspaceID, platform string
	flow                      login.Flow
	ctx                       context.Context
	cancel                    context.CancelFunc
	// tokens takes a token from a pasted code.
	tokens chan *oauth.Token
	once   sync.Once

	// mu guards state: one claim decides whether the flow saves, fails or
	// is cancelled, so a cancelled flow never saves.
	mu    sync.Mutex
	state authFlowState
}

type authFlowState int

const (
	authFlowRunning authFlowState = iota
	authFlowSavingState
	authFlowOver
)

// claim moves a running flow to next and reports whether it did. A flow
// whose context is done (cancelled, out of time, or its workspace gone)
// can't move to saving, even with a token at hand.
func (a *authFlow) claim(next authFlowState) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != authFlowRunning {
		return false
	}
	if next == authFlowSavingState && a.ctx.Err() != nil {
		return false
	}
	a.state = next
	return true
}

// authFlows are the engine's running sign-ins.
type authFlows struct {
	mu    sync.Mutex
	flows map[string]*authFlow
}

func (b *Backend) authFlows() *authFlows { return &b.desktopEngine().auth }

// StartAuthFlow starts platform's sign-in for the workspace and returns
// the URL and code to show. The flow then runs on its own and reports
// through auth_flow events until it is done, failed or cancelled; it ends
// with the workspace at the latest.
func (b *Backend) StartAuthFlow(workspaceID, platform string) (proto.AuthFlow, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AuthFlow{}, err
	}
	switch platform {
	case login.PlatformHyper, login.PlatformCopilot, login.PlatformOpenAI, login.PlatformGrok:
	default:
		return proto.AuthFlow{}, fmt.Errorf("%w: %s", ErrAuthPlatform, platform)
	}
	f, err := newAuthFlow(platform)
	if err != nil {
		return proto.AuthFlow{}, fmt.Errorf("%w: %s", ErrAuthPlatform, platform)
	}
	parent := ws.ctx
	if parent == nil {
		parent = b.ctx
	}
	ctx, cancel := context.WithTimeout(parent, authFlowTimeout)
	start := time.Now()
	url, userCode, err := f.Start(ctx)
	if platform == login.PlatformHyper {
		if wait := hyperMinimumWait - time.Since(start); wait > 0 {
			time.Sleep(wait)
		}
	}
	if err != nil {
		cancel()
		f.Close()
		// The provider's answer is not passed on: it may hold secrets.
		return proto.AuthFlow{}, authStartError(platform)
	}
	a := &authFlow{
		id: uuid.NewString(), workspaceID: ws.ID, platform: platform,
		flow: f, ctx: ctx, cancel: cancel, tokens: make(chan *oauth.Token, 1),
	}
	flows := b.authFlows()
	flows.mu.Lock()
	if flows.flows == nil {
		flows.flows = map[string]*authFlow{}
	}
	flows.flows[a.id] = a
	flows.mu.Unlock()
	go b.runAuthFlow(a)
	return proto.AuthFlow{FlowID: a.id, URL: url, UserCode: userCode, CodeEntry: login.TakesCode(f)}, nil
}

// runAuthFlow waits for the user, then saves the token.
func (b *Backend) runAuthFlow(a *authFlow) {
	defer b.endAuthFlow(a)
	b.sendAuthFlow(a, proto.AuthFlowWaiting, "")
	waited := make(chan error, 1)
	go func() {
		token, err := a.flow.Wait(a.ctx)
		if err == nil {
			select {
			case a.tokens <- token:
			default:
			}
		}
		waited <- err
	}()
	// The context's end also ends the flow, so a provider Wait that never
	// returns can't keep the flow, its record or its listener alive. A
	// token that is taken anyway is saved only if the claim below finds
	// the context still live.
	var token *oauth.Token
	select {
	case token = <-a.tokens:
	case err := <-waited:
		if err != nil {
			b.failAuthFlow(a, authWaitError(a.ctx, err))
			return
		}
		token = <-a.tokens
	case <-a.ctx.Done():
		b.failAuthFlow(a, authWaitError(a.ctx, a.ctx.Err()))
		return
	}
	// A flow cancelled or out of time meanwhile never saves, even with a
	// token at hand.
	if !a.claim(authFlowSavingState) {
		b.failAuthFlow(a, authWaitError(a.ctx, a.ctx.Err()))
		return
	}
	b.sendAuthFlow(a, proto.AuthFlowSaving, "")
	if err := b.SetProviderAPIKey(a.workspaceID, config.ScopeGlobal, login.ProviderID(a.platform), token); err != nil {
		b.sendAuthFlow(a, proto.AuthFlowFailed, "failed to save API key")
		return
	}
	b.sendAuthFlow(a, proto.AuthFlowDone, "")
}

// failAuthFlow reports a running flow as failed with a safe text. A flow
// already cancelled or saving reports nothing more.
func (b *Backend) failAuthFlow(a *authFlow, text string) {
	if !a.claim(authFlowOver) {
		return
	}
	if text != "" {
		b.sendAuthFlow(a, proto.AuthFlowFailed, text)
	}
}

// SendAuthCode finishes a flow with a code pasted from the provider's
// page. A wrong code fails the flow, as in the terminal.
func (b *Backend) SendAuthCode(flowID, code string) error {
	a, ok := b.authFlow(flowID)
	if !ok {
		return ErrAuthFlowNotFound
	}
	if !login.TakesCode(a.flow) {
		return ErrAuthNoCode
	}
	go func() {
		token, err := login.CompleteWithCode(a.ctx, a.flow, code)
		if err != nil {
			if a.ctx.Err() == nil {
				b.failAuthFlow(a, "the code was not accepted")
				b.endAuthFlow(a)
			}
			return
		}
		select {
		case a.tokens <- token:
		default:
		}
	}()
	return nil
}

// CancelAuthFlow stops a flow. No event follows: whoever cancelled it
// closed its sheet. A flow already saving its token can't be cancelled.
func (b *Backend) CancelAuthFlow(flowID string) error {
	a, ok := b.authFlow(flowID)
	if !ok {
		return ErrAuthFlowNotFound
	}
	if !a.claim(authFlowOver) {
		a.mu.Lock()
		saving := a.state == authFlowSavingState
		a.mu.Unlock()
		if saving {
			return ErrAuthFlowSaving
		}
		return ErrAuthFlowNotFound
	}
	b.endAuthFlow(a)
	return nil
}

// authStartError is the terminal's text for a sign-in that could not
// start (internal/ui/dialog/auth/oauth_hyper.go, oauth_grok.go), without
// the provider's answer.
func authStartError(platform string) error {
	text := "failed to initiate device auth"
	if platform == login.PlatformOpenAI || platform == login.PlatformGrok {
		text = "failed to start browser auth"
	}
	return kindErr(ErrAuthStart, errors.New(text))
}

// authWaitError is a safe text for a sign-in that ended without a token:
// provider answers (a token exchange's body) are never passed on. A
// cancelled flow gets "".
func authWaitError(ctx context.Context, err error) string {
	var exchange *oauth.TokenExchangeError
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return ""
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "authentication timed out"
	case errors.Is(err, copilot.ErrNotAvailable):
		return "GitHub Copilot is unavailable for this account"
	case errors.As(err, &exchange):
		return "token exchange failed"
	default:
		return "authentication failed"
	}
}

// VerifyAPIKey tests key against the provider as the terminal's API key
// sheet does, without saving it. A key that does not work is ok false
// with the reason; the key itself is never logged.
func (b *Backend) VerifyAPIKey(workspaceID, providerID, apiKey string) (proto.VerifyKey, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.VerifyKey{}, err
	}
	provider, ok := verifyKeyProvider(ws.Cfg, providerID)
	if !ok {
		return proto.VerifyKey{}, fmt.Errorf("%w: %s", ErrAuthProvider, providerID)
	}
	provider.APIKey = apiKey
	if err := verifyAPIKey(provider, ws.Cfg.Resolver()); err != nil {
		return proto.VerifyKey{Error: redactSecrets(err.Error(), apiKey)}, nil
	}
	return proto.VerifyKey{OK: true}, nil
}

// verifyKeyProvider is the provider the terminal's sheet tests: the
// configured one, else the known one, with its type and endpoint only.
func verifyKeyProvider(cfg *config.ConfigStore, providerID string) (config.ProviderConfig, bool) {
	if pc, ok := cfg.Config().Providers.Get(providerID); ok {
		return config.ProviderConfig{ID: providerID, Name: pc.Name, Type: pc.Type, BaseURL: pc.BaseURL}, true
	}
	for _, p := range cfg.KnownProviders() {
		if string(p.ID) == providerID {
			return config.ProviderConfig{ID: providerID, Name: p.Name, Type: p.Type, BaseURL: p.APIEndpoint}, true
		}
	}
	return config.ProviderConfig{}, false
}

func (b *Backend) authFlow(id string) (*authFlow, bool) {
	flows := b.authFlows()
	flows.mu.Lock()
	defer flows.mu.Unlock()
	a, ok := flows.flows[id]
	return a, ok
}

// endAuthFlow forgets a flow and releases what it holds.
func (b *Backend) endAuthFlow(a *authFlow) {
	a.once.Do(func() {
		flows := b.authFlows()
		flows.mu.Lock()
		delete(flows.flows, a.id)
		flows.mu.Unlock()
		a.cancel()
		a.flow.Close()
	})
}

// sendAuthFlow reports a flow's state to the apps of its workspace. text
// is one of the fixed texts above, never a provider's answer.
func (b *Backend) sendAuthFlow(a *authFlow, state, text string) {
	ws, getErr := b.GetWorkspace(a.workspaceID)
	if getErr != nil || ws.App == nil {
		return
	}
	ev := proto.AuthFlowEvent{FlowID: a.id, State: state, Error: text}
	ws.SendEvent(pubsub.Event[proto.AuthFlowEvent]{Type: pubsub.UpdatedEvent, Payload: ev})
}

// AuthState reports which agent CLIs hold their own login on this machine
// (cliagent.SignedIn), for Settings → Accounts, and the email a signed-in
// login uses where the CLI keeps one (cliagent.LoginEmail). No credential is
// read into the answer.
func (b *Backend) AuthState(workspaceID string) (proto.AuthStateResponse, error) {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
		return proto.AuthStateResponse{}, err
	}
	out := proto.AuthStateResponse{Providers: map[string]bool{}, Emails: map[string]string{}}
	for _, kind := range []catwalk.Type{config.TypeGrokCLI, config.TypeAGYCLI, config.TypeOpenCodeCLI, config.TypeOpenCodeZenCLI} {
		signedIn := cliagent.SignedIn(kind)
		out.Providers[string(kind)] = signedIn
		if email := cliagent.LoginEmail(kind); signedIn && email != "" {
			out.Emails[string(kind)] = email
		}
	}
	return out, nil
}
