package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/pkg/browser"
	"golang.org/x/oauth2"
)

// ErrInteractiveAuthRequired is returned by Authorize when a server needs
// interactive (browser) authorization but the current context does not
// permit it. Background connections such as startup deliberately withhold
// permission so a failed or missing token surfaces as a needs-auth state
// instead of silently opening a browser and blocking initialization. The
// user then triggers the interactive flow explicitly.
var ErrInteractiveAuthRequired = errors.New("interactive OAuth authorization required")

// interactiveKey marks a context as permitting the interactive browser flow.
type interactiveKey struct{}

// WithInteractive returns a context that permits the interactive browser
// authorization flow. Only user-initiated authentication should use it.
func WithInteractive(ctx context.Context) context.Context {
	return context.WithValue(ctx, interactiveKey{}, true)
}

// IsInteractive reports whether ctx permits the interactive browser flow.
func IsInteractive(ctx context.Context) bool {
	v, _ := ctx.Value(interactiveKey{}).(bool)
	return v
}

// callbackPath is the path the authorization server redirects back to. It
// is part of the registered redirect URI, so it must not change without
// re-registering clients.
const callbackPath = "/callback"

// callbackPorts are the localhost ports tried, in order, for the OAuth
// redirect listener. The first available one is used.
var callbackPorts = []int{
	40704, 40705, 40706, 40707, 40708,
	40709, 40710, 40711, 40712, 40713,
}

// Handler implements auth.OAuthHandler for MCP HTTP servers. It wraps
// the go-sdk AuthorizationCodeHandler and persists the token (plus the
// client registration and endpoints needed to refresh it) so that
// restarts and background refreshes never force the user back through
// the browser.
//
// Persistence is wired through the SDK's own hooks: NewTokenSource is
// invoked after the code exchange and for every refresh, and
// InitialTokenSource injects a restored token at startup. That removes
// the need to hand-roll authorization-server discovery.
type Handler struct {
	inner    auth.OAuthHandler
	receiver *callbackReceiver

	// openURL opens the authorization URL in the user's browser. It is
	// a field so tests can simulate a headless environment or drive the
	// callback directly.
	openURL func(string) error

	// interactive permits the browser authorization flow. It is false for
	// background connections (startup) so a missing or unrefreshable token
	// surfaces as a needs-auth state instead of opening a browser and
	// blocking initialization.
	interactive bool

	mu             sync.Mutex
	cachedToken    *oauth2.Token
	authURL        string
	serverURL      string
	onTokenRefresh func(*oauth.Token)

	// suppressBrowser, when true, prevents openURL from being invoked
	// (the authorization URL is still recorded and logged). Used when
	// the flow is driven remotely, e.g. by a connected client that opens
	// the browser on its own machine.
	suppressBrowser bool
}

var _ auth.OAuthHandler = (*Handler)(nil)

// NewHandler creates a new OAuth handler for an MCP server. savedToken,
// if present, restores a prior session: its access/refresh tokens and
// captured client registration are injected so the SDK can use and
// silently refresh them without a browser round-trip. preregistered, if
// set, supplies an explicit OAuth client for servers that do not support
// dynamic client registration. onTokenRefresh is called whenever a token
// is obtained or refreshed so the caller can persist it. interactive
// permits the browser flow; pass false for background connections
// (startup) so a bad token never opens a browser.
func NewHandler(
	serverName string,
	serverURL string,
	savedToken *oauth.Token,
	preregistered *oauth.OAuthClient,
	onTokenRefresh func(*oauth.Token),
	interactive bool,
	callbackPort int,
) (*Handler, error) {
	receiver := &callbackReceiver{
		serverName: serverName,
		fixedPort:  callbackPort,
	}

	port, err := resolveCallbackPort(callbackPort)
	if err != nil {
		return nil, err
	}
	receiver.fixedPort = port

	redirectURL := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)

	h := &Handler{
		receiver:       receiver,
		serverURL:      serverURL,
		openURL:        browser.OpenURL,
		interactive:    interactive,
		onTokenRefresh: onTokenRefresh,
	}
	receiver.handler = h

	cfg := h.authorizationConfig(redirectURL)
	cfg.PreregisteredClient = clientCredentials(preregistered, savedToken)
	h.restoreToken(cfg, savedToken)

	inner, err := auth.NewAuthorizationCodeHandler(cfg)
	if err != nil {
		receiver.close()
		return nil, fmt.Errorf("failed to create OAuth handler: %w", err)
	}
	h.inner = inner

	slog.Info(
		"MCP OAuth handler created",
		"name", serverName,
		"redirect_url", redirectURL,
		"restored_token", h.cachedToken != nil,
	)

	return h, nil
}

// resolveCallbackPort picks the redirect port without binding it. The
// listener is only opened when an authorization actually runs (see
// fetchAuthorizationCode), so a handler that restores a valid token never
// occupies the port and several Crush processes can share it; only the one
// doing a live login binds, and only for the duration of that login.
//
// A fixed port comes straight from config. Otherwise we probe for the
// first free candidate, just long enough to learn which is open. Either
// way the chosen port is baked into the redirect URI and pinned on the
// receiver, so bindLocked always rebinds the SAME port. The probe is not a
// reservation — another process can take the port before the first real
// login — but a busy port then fails loudly rather than silently binding a
// port the redirect URI does not point at.
func resolveCallbackPort(callbackPort int) (int, error) {
	if callbackPort != 0 {
		return callbackPort, nil
	}
	lc := &net.ListenConfig{}
	for _, p := range callbackPorts {
		probe, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf("localhost:%d", p))
		if err == nil {
			_ = probe.Close()
			return p, nil
		}
	}
	return 0, errors.New("failed to start OAuth callback listener: all candidate ports in use")
}

// authorizationConfig builds the SDK configuration for the authorization
// code flow, redirecting to redirectURL.
func (h *Handler) authorizationConfig(redirectURL string) *auth.AuthorizationCodeHandlerConfig {
	// newTokenSource is the SDK hook invoked once after a successful code
	// exchange. The token it hands us is brand new, so persist it right
	// away, then wrap the source so later refreshes persist on change. The
	// resolved oauth2.Config carries the registered client ID and
	// discovered endpoints, which we persist alongside the token so a
	// later start can refresh without rediscovery.
	newTokenSource := func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
		h.persist(cfg, tok)
		base := cfg.TokenSource(ctx, tok)
		return NewSavingTokenSource(base, cfg, tok, func(c *oauth2.Config, t *oauth2.Token) {
			h.persist(c, t)
		}), nil
	}

	return &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: h.receiver.fetchAuthorizationCode,
		RequestRefreshToken:      true,
		NewTokenSource:           newTokenSource,
		// Some servers (e.g. Sentry) send the RFC 9207 iss parameter
		// without advertising it in their metadata. Accept a matching
		// unadvertised iss instead of failing the flow.
		AcceptUnadvertisedIss: true,
		// Use a metadata-fixing HTTP client so trailing-slash issuers in
		// OAuth metadata responses don't trip the SDK's strict RFC 8414
		// validation. Also rewrite internal-cluster redirects back to the
		// external hostname so the flow works outside the cluster.
		// Based on Bruno Krugel's fix from PR #3396.
		Client: newOAuthMetadataClient(http.DefaultTransport, h.serverURL),
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:   "Crush",
				RedirectURIs: []string{redirectURL},
				GrantTypes:   []string{"authorization_code", "refresh_token"},
			},
		},
	}
}

// clientCredentials returns the pre-registered client the SDK should use
// instead of dynamic registration, or nil to register dynamically. An
// explicitly configured client (for servers that don't support DCR, like
// GitHub or Slack) takes precedence over one captured from a previous
// registration and restored from savedToken.
func clientCredentials(preregistered *oauth.OAuthClient, savedToken *oauth.Token) *oauthex.ClientCredentials {
	client := preregistered
	if client == nil || client.ClientID == "" {
		if savedToken != nil && savedToken.Client != nil {
			client = savedToken.Client
		}
	}
	if client == nil || client.ClientID == "" {
		return nil
	}
	creds := &oauthex.ClientCredentials{
		ClientID: client.ClientID,
	}
	if client.ClientSecret != "" {
		creds.ClientSecretAuth = &oauthex.ClientSecretAuth{
			ClientSecret: client.ClientSecret,
		}
	}
	return creds
}

// restoreToken installs a saved token as the initial token source so the
// SDK uses it directly (and refreshes it) instead of triggering the browser
// flow. The saver is seeded with the restored token so only a genuine
// refresh writes to disk; a plain restart causes no token churn.
func (h *Handler) restoreToken(cfg *auth.AuthorizationCodeHandlerConfig, savedToken *oauth.Token) {
	if !hasRefreshableToken(savedToken) {
		return
	}
	restored := &oauth2.Token{
		AccessToken:  savedToken.AccessToken,
		RefreshToken: savedToken.RefreshToken,
		Expiry:       time.Unix(savedToken.ExpiresAt, 0),
	}
	oc := &oauth2.Config{
		ClientID:     savedToken.Client.ClientID,
		ClientSecret: savedToken.Client.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   savedToken.Client.AuthURL,
			TokenURL:  savedToken.Client.TokenURL,
			AuthStyle: oauth2.AuthStyle(savedToken.Client.AuthStyle),
		},
	}
	base := oc.TokenSource(context.Background(), restored)
	cfg.InitialTokenSource = NewSavingTokenSource(base, oc, restored, func(c *oauth2.Config, t *oauth2.Token) {
		h.persist(c, t)
	})
	h.cachedToken = restored
}

// hasRefreshableToken reports whether a saved token carries enough state
// to be used and refreshed without re-authorizing: an access token plus
// the token endpoint captured previously.
func hasRefreshableToken(t *oauth.Token) bool {
	return t != nil && t.AccessToken != "" && t.Client != nil && t.Client.TokenURL != ""
}

// AuthURL returns the last authorization URL opened in the browser.
func (h *Handler) AuthURL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.authURL
}

// SetBrowserSuppress controls whether the browser is opened automatically
// when the authorization URL is generated. Pass an unlock function that
// restores the previous behavior; the handler re-enables the browser when
// the returned function is called. This is used by the server-driven flow
// where a remote client opens the browser locally.
func (h *Handler) SetBrowserSuppress(suppress bool) func() {
	h.mu.Lock()
	prev := h.suppressBrowser
	h.suppressBrowser = suppress
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.suppressBrowser = prev
		h.mu.Unlock()
	}
}

// Token returns the current OAuth token, or nil if not yet authorized.
func (h *Handler) Token() *oauth2.Token {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cachedToken
}

// TokenSource implements auth.OAuthHandler. It delegates to the inner
// handler, whose token source is already wrapped for persistence via
// NewTokenSource and seeded (when restoring) via InitialTokenSource.
func (h *Handler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return h.inner.TokenSource(ctx)
}

// Authorize implements auth.OAuthHandler. It runs the SDK authorization
// flow; the resulting token is captured and persisted through the
// NewTokenSource saver.
func (h *Handler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	// Never open a browser for a background connection (e.g. startup). The
	// caller surfaces a needs-auth state and the user triggers the
	// interactive flow via a handler created with interactive=true.
	if !h.interactive {
		return ErrInteractiveAuthRequired
	}
	if err := h.inner.Authorize(ctx, req, resp); err != nil {
		// The SDK reports this when the server supports none of the
		// registration methods offered and no client was pre-registered.
		// Point the user at the config field that fixes it.
		if strings.Contains(err.Error(), "no configured client registration methods") {
			return fmt.Errorf("%q does not support automatic OAuth client registration; register an OAuth app with the provider and set oauth_client_id (and oauth_client_secret if required) for this MCP server: %w", h.serverURL, err)
		}
		return err
	}
	ts, err := h.inner.TokenSource(ctx)
	if err != nil {
		return err
	}
	if ts == nil {
		// The SDK short-circuits non-authorization responses (e.g. a
		// genuine 403) without establishing a token source. Leave any
		// restored token in place.
		return nil
	}
	// Reading the token drives the saver, which persists it.
	if _, err := ts.Token(); err != nil {
		return err
	}
	slog.Info("MCP OAuth token captured")
	return nil
}

// persist records the latest token in memory and hands a serialisable
// copy (including the client registration and endpoints from cfg) to the
// caller-supplied saver.
func (h *Handler) persist(cfg *oauth2.Config, tok *oauth2.Token) {
	h.mu.Lock()
	h.cachedToken = tok
	h.mu.Unlock()

	if h.onTokenRefresh == nil {
		return
	}

	out := &oauth.Token{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if !tok.Expiry.IsZero() {
		out.ExpiresIn = int(time.Until(tok.Expiry).Seconds())
	}
	out.SetExpiresAt()
	if cfg != nil {
		out.Client = &oauth.OAuthClient{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			AuthURL:      cfg.Endpoint.AuthURL,
			TokenURL:     cfg.Endpoint.TokenURL,
			AuthStyle:    int(cfg.Endpoint.AuthStyle),
		}
	}
	h.onTokenRefresh(out)
}

// Close shuts down the callback server.
func (h *Handler) Close() {
	h.receiver.close()
}
