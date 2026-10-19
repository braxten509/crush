package backend

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/login"
	"github.com/charmbracelet/crush/internal/oauth"
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
	// ErrAuthProvider is a provider the key check does not know.
	ErrAuthProvider = errors.New("provider not found")
)

// authFlowTimeout bounds a sign-in, as `crush login` does.
const authFlowTimeout = 10 * time.Minute

// hyperMinimumWait is how long Hyper's start takes at least, so its
// "Initializing..." is seen (internal/ui/dialog/auth/oauth_hyper.go:36).
var hyperMinimumWait = 750 * time.Millisecond

// newAuthFlow makes a platform's flow; tests and the crushtest seams
// replace it.
var newAuthFlow = login.NewFlow

// verifyAPIKey tests key against provider as the terminal's API key
// sheet does; tests and the crushtest seams replace it.
var verifyAPIKey = func(provider config.ProviderConfig, resolver config.VariableResolver) error {
	return provider.TestConnection(resolver)
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
}

// authFlows are the engine's running sign-ins.
type authFlows struct {
	mu    sync.Mutex
	flows map[string]*authFlow
}

func (b *Backend) authFlows() *authFlows { return &b.desktopEngine().auth }

// StartAuthFlow starts platform's sign-in for the workspace and returns
// the URL and code to show. The flow then runs on its own and reports
// through auth_flow events until it is done, failed or cancelled.
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
	ctx, cancel := context.WithTimeout(context.Background(), authFlowTimeout)
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
		return proto.AuthFlow{}, err
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
	b.sendAuthFlow(a, proto.AuthFlowWaiting, nil)
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
	var token *oauth.Token
	select {
	case token = <-a.tokens:
	case err := <-waited:
		if err != nil {
			if errors.Is(a.ctx.Err(), context.Canceled) {
				return // cancelled: nobody waits for it.
			}
			b.sendAuthFlow(a, proto.AuthFlowFailed, err)
			return
		}
		token = <-a.tokens
	case <-a.ctx.Done():
		if errors.Is(a.ctx.Err(), context.DeadlineExceeded) {
			b.sendAuthFlow(a, proto.AuthFlowFailed, a.ctx.Err())
		}
		return
	}
	b.sendAuthFlow(a, proto.AuthFlowSaving, nil)
	if err := b.SetProviderAPIKey(a.workspaceID, config.ScopeGlobal, login.ProviderID(a.platform), token); err != nil {
		b.sendAuthFlow(a, proto.AuthFlowFailed, fmt.Errorf("failed to save API key: %w", err))
		return
	}
	b.sendAuthFlow(a, proto.AuthFlowDone, nil)
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
				b.sendAuthFlow(a, proto.AuthFlowFailed, err)
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
// closed its sheet.
func (b *Backend) CancelAuthFlow(flowID string) error {
	a, ok := b.authFlow(flowID)
	if !ok {
		return ErrAuthFlowNotFound
	}
	b.endAuthFlow(a)
	return nil
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
		return proto.VerifyKey{Error: err.Error()}, nil
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

// sendAuthFlow reports a flow's state to the apps of its workspace.
func (b *Backend) sendAuthFlow(a *authFlow, state string, err error) {
	ws, getErr := b.GetWorkspace(a.workspaceID)
	if getErr != nil || ws.App == nil {
		return
	}
	ev := proto.AuthFlowEvent{FlowID: a.id, State: state}
	if err != nil {
		ev.Error = err.Error()
	}
	ws.SendEvent(pubsub.Event[proto.AuthFlowEvent]{Type: pubsub.UpdatedEvent, Payload: ev})
}
