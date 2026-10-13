package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/oauth"
	mcpoauth "github.com/charmbracelet/crush/internal/oauth/mcp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

// AuthenticateMCP initiates the OAuth flow for an MCP server that is in
// StateNeedsAuth. It creates the OAuth handler (which starts a local
// callback server), connects to the server (which triggers the browser
// auth flow on 401), and transitions to StateConnected on success.
func AuthenticateMCP(ctx context.Context, cfg *config.ConfigStore, name string) error {
	m, exists := cfg.Config().MCP[name]
	if !exists {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}

	if !m.OAuth || m.Type != config.MCPHttp {
		return fmt.Errorf("mcp '%s' does not use OAuth authentication", name)
	}

	updateState(name, StateStarting, nil, nil, Counts{}, withPending(m))

	// This is the user-initiated flow, so permit the interactive browser
	// authorization the handler otherwise withholds during startup.
	ctx = mcpoauth.WithInteractive(ctx)

	// The OAuth handler persists the token automatically as it is
	// exchanged, so a successful connection has already saved it.
	_, err := connectAndRegister(ctx, cfg, name, m, currentGen(name), cfg.Resolver(), ChannelOptIn(m, cfg.Overrides().EnabledChannels, name))
	if err != nil {
		return err
	}
	return nil
}

// PendingAuthServer describes an MCP server awaiting OAuth.
type PendingAuthServer struct {
	Name string
	URL  string
}

// MCPAuthURL returns the current OAuth authorization URL for the named
// MCP, or empty if none is in progress.
func MCPAuthURL(name string) string {
	h, ok := authURLs.Get(name)
	if !ok || h == nil {
		return ""
	}
	return h.AuthURL()
}

// PendingAuthMCPs returns MCP servers in StateNeedsAuth with their URLs.
func PendingAuthMCPs(cfg *config.ConfigStore) []PendingAuthServer {
	var pending []PendingAuthServer
	for name, info := range states.Seq2() {
		if info.State == StateNeedsAuth {
			url := ""
			if m, ok := cfg.Config().MCP[name]; ok {
				url = m.URL
			}
			pending = append(pending, PendingAuthServer{Name: name, URL: url})
		}
	}
	slices.SortFunc(pending, func(a, b PendingAuthServer) int {
		return strings.Compare(a.Name, b.Name)
	})
	return pending
}

// BeginAuth starts the OAuth flow for a server in StateNeedsAuth but
// suppresses opening a local browser; the caller is responsible for
// surfacing the authorization URL (via [MCPAuthURL]) to the user. It returns
// a finish function that must be called exactly once with the request
// context: finish blocks until the flow completes and returns the result.
//
// Only one browser-suppressed flow per server may be in progress. The
// returned cancel function aborts the flow without waiting; use it when the
// caller's context is cancelled.
func BeginAuth(cfg *config.ConfigStore, name string) (finish func(ctx context.Context) error, cancel context.CancelFunc, err error) {
	m, exists := cfg.Config().MCP[name]
	if !exists {
		return nil, nil, fmt.Errorf("mcp '%s' not found in configuration", name)
	}
	if !m.OAuth || m.Type != config.MCPHttp {
		return nil, nil, fmt.Errorf("mcp '%s' does not use OAuth authentication", name)
	}

	lock := suppressLock(name)
	if !lock.TryLock() {
		return nil, nil, fmt.Errorf("mcp '%s' already has an authentication in progress", name)
	}

	flowCtx, flowCancel := context.WithCancel(context.Background())
	flowCtx = mcpoauth.WithInteractive(flowCtx)
	flowCtx = context.WithValue(flowCtx, suppressBrowserKey{}, true)

	finish = func(ctx context.Context) error {
		defer lock.Unlock()
		defer flowCancel()

		done := make(chan error, 1)
		go func() {
			done <- runAuthFlow(flowCtx, cfg, name, m)
		}()

		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			flowCancel()
			<-done
			return ctx.Err()
		}
	}
	return finish, flowCancel, nil
}

// runAuthFlow executes the OAuth connect for BeginAuth with browser
// suppression enabled on the freshly created handler.
func runAuthFlow(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig) error {
	updateState(name, StateStarting, nil, nil, Counts{}, withPending(m))
	_, err := connectAndRegister(ctx, cfg, name, m, currentGen(name), cfg.Resolver(), ChannelOptIn(m, cfg.Overrides().EnabledChannels, name))
	return err
}

// suppressLock returns the per-server mutex used to serialize
// browser-suppressed OAuth flows, creating it on first use.
func suppressLock(name string) *sync.Mutex {
	mu, ok := suppressMus.Get(name)
	if !ok {
		mu = &sync.Mutex{}
		suppressMus.Set(name, mu)
	}
	return mu
}

// oauthRoundTripper wraps an HTTP transport with OAuth bearer token
// injection and 401-triggered authorization. Used for SSE transports
// that don't support the SDK's OAuthHandler natively. Based on Bruno
// Krugel's implementation from PR #3396.
type oauthRoundTripper struct {
	base    http.RoundTripper
	handler auth.OAuthHandler
}

func newOAuthRoundTripper(handler auth.OAuthHandler, base http.RoundTripper) *oauthRoundTripper {
	return &oauthRoundTripper{base: base, handler: handler}
}

func (rt *oauthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.doRequestWithToken(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if authErr := rt.handler.Authorize(req.Context(), req, resp); authErr != nil {
			return resp, nil
		}
		resp.Body.Close()
		return rt.doRequestWithToken(req.Clone(req.Context()))
	}

	return resp, nil
}

func (rt *oauthRoundTripper) doRequestWithToken(req *http.Request) (*http.Response, error) {
	ts, err := rt.handler.TokenSource(req.Context())
	if err != nil {
		return nil, fmt.Errorf("oauth token source: %w", err)
	}
	if ts != nil {
		token, err := ts.Token()
		if err == nil && token != nil {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}
	}
	return rt.base.RoundTrip(req)
}

// hasUsableToken returns true if the saved OAuth token has an access
// token that can be used or refreshed. A token with an empty access
// token is structurally invalid and should be treated as missing.
func hasUsableToken(tok *oauth.Token) bool {
	return tok != nil && tok.AccessToken != ""
}

// isOAuthInitErr returns true if the error indicates the OAuth token
// is missing, no longer valid, or cannot be refreshed. This covers:
//   - invalid_grant: expired or revoked refresh tokens
//   - invalid_client: deleted or deactivated client registrations
//   - "no token available": the handler had no cached token to use
//   - interactive authorization was required but withheld during startup
func isOAuthInitErr(err error) bool {
	if errors.Is(err, mcpoauth.ErrInteractiveAuthRequired) {
		return true
	}
	var rErr *oauth2.RetrieveError
	if errors.As(err, &rErr) {
		return rErr.ErrorCode == "invalid_grant" || rErr.ErrorCode == "invalid_client"
	}
	msg := err.Error()
	return strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "invalid_client") ||
		strings.Contains(msg, "no token available")
}

// clearOAuthToken removes the persisted OAuth token for a named MCP
// server from the global config so subsequent startups don't retry
// with a known-bad refresh token.
func clearOAuthToken(cfg *config.ConfigStore, name string) {
	key := fmt.Sprintf("mcp.%s.oauth_token", name)
	if err := cfg.RemoveConfigField(config.ScopeGlobal, key); err != nil {
		slog.Warn("Failed to clear stale MCP OAuth token", "name", name, "error", err)
	}
}

// clearMCPData removes a stale MCP server's tools, prompts,
// resources, and auth handlers from global state so they are not
// served to the agent.
func clearMCPData(name string) {
	allTools.Del(name)
	allPrompts.Del(name)
	allResources.Del(name)
	if h, ok := authURLs.Get(name); ok {
		h.Close()
		authURLs.Del(name)
	}
}
