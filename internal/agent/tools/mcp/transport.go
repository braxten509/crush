package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/oauth"
	mcpoauth "github.com/charmbracelet/crush/internal/oauth/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// transportWrapper is implemented by every transport decorator crush layers
// around a base transport, so diagnostics that need the innermost transport
// can reach it without knowing which decorators are in play.
type transportWrapper interface {
	unwrapTransport() mcp.Transport
}

// unwrapTransport peels every decorator off a transport and returns the
// innermost one.
func unwrapTransport(transport mcp.Transport) mcp.Transport {
	for {
		w, ok := transport.(transportWrapper)
		if !ok {
			return transport
		}
		transport = w.unwrapTransport()
	}
}

// maybeStdioErr if a stdio mcp prints an error in non-json format, it'll fail
// to parse, and the cli will then close it, causing the EOF error.
// so, if we got an EOF err, and the transport is STDIO, we try to exec it
// again with a timeout and collect the output so we can add details to the
// error.
// this happens particularly when starting things with npx, e.g. if node can't
// be found or some other error like that.
func maybeStdioErr(err error, transport mcp.Transport) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	// The transport is wrapped in one or more decorators before Connect (the
	// channel gate today); the stdio transport we're probing for is the
	// innermost one. Unwrap all of them — without this the assertion below
	// never matches and stdio startup failures report a bare EOF instead of
	// the child's actual output. Every wrapper must implement
	// unwrapTransport or it will hide this diagnostic again.
	transport = unwrapTransport(transport)
	ct, ok := transport.(*mcp.CommandTransport)
	if !ok {
		return err
	}
	if err2 := stdioCheck(ct.Command); err2 != nil {
		err = errors.Join(err, err2)
	}
	return err
}

func maybeTimeoutErr(err error, timeout time.Duration) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}

func createTransport(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig, resolver config.VariableResolver) (mcp.Transport, *mcpoauth.Handler, error) {
	switch m.Type {
	case config.MCPStdio:
		command, err := resolver.ResolveValue(m.Command)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid mcp command: %w", err)
		}
		if strings.TrimSpace(command) == "" {
			return nil, nil, fmt.Errorf("mcp stdio config requires a non-empty 'command' field")
		}
		args, err := m.ResolvedArgs(resolver)
		if err != nil {
			return nil, nil, err
		}
		envs, err := m.ResolvedEnv(resolver)
		if err != nil {
			return nil, nil, err
		}
		cmd := exec.CommandContext(ctx, home.Long(command), args...)
		cmd.Env = append(os.Environ(), envs...)
		// Run the child in its own process group and kill the whole group when
		// the session context is cancelled. A stdio server often spawns its own
		// children (signal-mcp launches signal-cli); os/exec's default
		// cancellation kills only the direct child, orphaning the rest with
		// PPID 1 — production accumulated 15+ such zombies over two days.
		configureStdioProcess(cmd)
		return &mcp.CommandTransport{
			Command: cmd,
		}, nil, nil
	case config.MCPHttp:
		url, err := m.ResolvedURL(resolver)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(url) == "" {
			return nil, nil, fmt.Errorf("mcp http config requires a non-empty 'url' field")
		}

		// OAuth-enabled HTTP transport. The handler persists the token
		// (and the client registration/endpoints needed to refresh it)
		// on every exchange and refresh via this saver.
		if m.OAuth {
			tokenSaver := func(tok *oauth.Token) {
				if err := cfg.SetConfigField(config.ScopeGlobal, fmt.Sprintf("mcp.%s.oauth_token", name), tok); err != nil {
					slog.Warn("Failed to persist MCP OAuth token", "name", name, "error", err)
				} else {
					slog.Info("Persisted MCP OAuth token", "name", name)
				}
			}

			// A pre-registered client is required for servers that do not
			// support dynamic client registration (e.g. GitHub, Slack).
			// Resolve the credentials through the shell like other config
			// values so $VAR and $(cmd) work.
			var preregistered *oauth.OAuthClient
			if strings.TrimSpace(m.OAuthClientID) != "" {
				clientID, err := resolver.ResolveValue(m.OAuthClientID)
				if err != nil {
					return nil, nil, fmt.Errorf("oauth_client_id: %w", err)
				}
				clientSecret, err := resolver.ResolveValue(m.OAuthClientSecret)
				if err != nil {
					return nil, nil, fmt.Errorf("oauth_client_secret: %w", err)
				}
				preregistered = &oauth.OAuthClient{
					ClientID:     strings.TrimSpace(clientID),
					ClientSecret: strings.TrimSpace(clientSecret),
				}
			}

			// Normalize trailing slash for PRM discovery compatibility.
			normalizedURL := strings.TrimSuffix(url, "/")
			oauthHandler, oauthErr := mcpoauth.NewHandler(name, normalizedURL, m.OAuthToken, preregistered, tokenSaver, mcpoauth.IsInteractive(ctx), m.OAuthCallbackPort)
			if oauthErr != nil {
				return nil, nil, fmt.Errorf("failed to create OAuth handler for mcp %q: %w", name, oauthErr)
			}
			authURLs.Set(name, oauthHandler)
			return &mcp.StreamableClientTransport{
				Endpoint:     url,
				OAuthHandler: oauthHandler,
			}, oauthHandler, nil
		}

		headers, err := m.ResolvedHeaders(resolver)
		if err != nil {
			return nil, nil, err
		}
		client := &http.Client{
			Transport: &headerRoundTripper{
				headers: headers,
			},
		}
		return &mcp.StreamableClientTransport{
			Endpoint:   url,
			HTTPClient: client,
		}, nil, nil
	case config.MCPSSE:
		url, err := m.ResolvedURL(resolver)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(url) == "" {
			return nil, nil, fmt.Errorf("mcp sse config requires a non-empty 'url' field")
		}
		headers, err := m.ResolvedHeaders(resolver)
		if err != nil {
			return nil, nil, err
		}

		var transport http.RoundTripper = &headerRoundTripper{headers: headers}
		var oauthHandler *mcpoauth.Handler

		// SSE transports don't support the SDK's OAuthHandler natively,
		// so we wrap the HTTP transport with our own round-tripper that
		// injects bearer tokens and handles 401-triggered authorization.
		// Based on Bruno Krugel's oauthRoundTripper from PR #3396.
		if m.OAuth {
			tokenSaver := func(tok *oauth.Token) {
				if err := cfg.SetConfigField(config.ScopeGlobal, fmt.Sprintf("mcp.%s.oauth_token", name), tok); err != nil {
					slog.Warn("Failed to persist MCP OAuth token", "name", name, "error", err)
				} else {
					slog.Info("Persisted MCP OAuth token", "name", name)
				}
			}

			var preregistered *oauth.OAuthClient
			if strings.TrimSpace(m.OAuthClientID) != "" {
				clientID, err := resolver.ResolveValue(m.OAuthClientID)
				if err != nil {
					return nil, nil, fmt.Errorf("oauth_client_id: %w", err)
				}
				clientSecret, err := resolver.ResolveValue(m.OAuthClientSecret)
				if err != nil {
					return nil, nil, fmt.Errorf("oauth_client_secret: %w", err)
				}
				preregistered = &oauth.OAuthClient{
					ClientID:     strings.TrimSpace(clientID),
					ClientSecret: strings.TrimSpace(clientSecret),
				}
			}

			// Normalize trailing slash for PRM discovery compatibility.
			normalizedURL := strings.TrimSuffix(url, "/")
			handler, oauthErr := mcpoauth.NewHandler(name, normalizedURL, m.OAuthToken, preregistered, tokenSaver, mcpoauth.IsInteractive(ctx), m.OAuthCallbackPort)
			if oauthErr != nil {
				return nil, nil, fmt.Errorf("failed to create OAuth handler for mcp %q: %w", name, oauthErr)
			}
			oauthHandler = handler
			authURLs.Set(name, handler)
			transport = newOAuthRoundTripper(handler, transport)
		}

		client := &http.Client{Transport: transport}
		return &mcp.SSEClientTransport{
			Endpoint:   url,
			HTTPClient: client,
		}, oauthHandler, nil
	default:
		return nil, nil, fmt.Errorf("unsupported mcp type: %s", m.Type)
	}
}

type headerRoundTripper struct {
	headers map[string]string
}

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range rt.headers {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func mcpTimeout(m config.MCPConfig) time.Duration {
	if m.Timeout > 0 {
		return time.Duration(m.Timeout) * time.Second
	}
	// OAuth flows require user interaction in a browser, so use a
	// generous default to avoid timing out mid-auth.
	if m.OAuth {
		return 30 * time.Second
	}
	return 10 * time.Second
}

var stdioCheckTimeout = 5 * time.Second

func stdioCheck(old *exec.Cmd) error {
	ctx, cancel := context.WithTimeout(context.Background(), stdioCheckTimeout)
	defer cancel()
	// old.Args includes argv0 as the first element; exec.CommandContext
	// prepends old.Path as argv0, so we must skip it to avoid duplication
	// (e.g. "npx npx -y pkg" instead of "npx -y pkg").
	args := old.Args
	if len(args) > 0 {
		args = args[1:]
	}
	cmd := exec.CommandContext(ctx, old.Path, args...)
	cmd.Env = old.Env
	cmd.WaitDelay = stdioCheckTimeout
	out, err := cmd.CombinedOutput()
	if err == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return fmt.Errorf("%w: %s", err, string(out))
}
