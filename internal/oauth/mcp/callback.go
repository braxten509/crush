package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/oauth/callback"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

// callbackReceiver owns the localhost listener that the authorization
// server redirects back to, and hands each authorization result to the
// flow waiting for it.
//
// The listener is bound lazily, on the first authorization attempt, and
// released as soon as that attempt settles. A handler that holds a valid
// token never binds at all, so any number of Crush processes may coexist;
// the callback port is occupied only for the few seconds an actual login
// is in flight.
type callbackReceiver struct {
	handler *Handler
	// serverName labels the callback page so the user can see which MCP
	// server they just authorized.
	serverName string

	// fixedPort, when > 0, is the OAuthCallbackPort config: the single
	// port the registered redirect URI points at. When 0, a port is
	// chosen from callbackPorts at bind time.
	fixedPort int

	mu sync.Mutex
	// flight is the authorization currently awaiting a redirect, if any.
	// Connecting to a server can issue several requests at once, so more
	// than one of them can meet a 401 and ask to authorize. They share a
	// single flight rather than each opening their own browser tab and
	// racing for one redirect.
	flight *authFlight
	// server and port describe the live listener. They are set only while
	// an authorization is in flight and cleared the moment it settles.
	server *http.Server
	port   int
	closed bool
}

// authFlight is one authorization attempt: a browser tab was opened and a
// redirect is expected. It settles exactly once, whichever arrives first —
// a result, an error, or the receiver shutting down.
type authFlight struct {
	done   chan struct{}
	once   sync.Once
	result *auth.AuthorizationResult
	err    error

	// joined counts callers waiting on this flight instead of opening a tab.
	joined atomic.Int32
}

// settle records the outcome of the flight and wakes everyone waiting on
// it. Only the first call has any effect, so a duplicate redirect (a
// reloaded tab, say) cannot overwrite a result already in use.
func (f *authFlight) settle(result *auth.AuthorizationResult, err error) {
	f.once.Do(func() {
		f.result, f.err = result, err
		close(f.done)
	})
}

// begin returns the flight to wait on and whether the caller created it.
// The creator is responsible for opening the browser and, once the attempt
// is over, for clearing it so a later authorization can start fresh. The
// first (creating) caller also binds the callback listener; joiners wait
// on the flight that is already serving.
func (r *callbackReceiver) begin() (*authFlight, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flight != nil {
		r.flight.joined.Add(1)
		return r.flight, false, nil
	}
	if err := r.bindLocked(); err != nil {
		return nil, false, err
	}
	r.flight = &authFlight{done: make(chan struct{})}
	return r.flight, true, nil
}

// end retires the flight if it is still the current one, so the next
// authorization opens a fresh browser tab instead of waiting on a redirect
// that will never come.
func (r *callbackReceiver) end(flight *authFlight) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flight == flight {
		r.flight = nil
	}
}

// current returns the in-progress flight, or nil when no authorization is
// waiting on a redirect.
func (r *callbackReceiver) current() *authFlight {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flight
}

// bindLocked starts the listener if one is not already running; r.mu must
// be held. The port was resolved and pinned at construction, so this always
// targets the port the redirect URI points at; if it is busy the error
// surfaces loudly rather than silently binding a port nobody will redirect
// to. The listener starts accepting before this returns, so a browser opened
// immediately after cannot beat the server to the port.
func (r *callbackReceiver) bindLocked() error {
	if r.closed {
		return errors.New("OAuth callback listener closed")
	}
	if r.server != nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", r.handleCallback)
	server := &http.Server{Handler: mux}

	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf("localhost:%d", r.fixedPort))
	if err != nil {
		return fmt.Errorf("failed to bind OAuth callback port %d: %w", r.fixedPort, err)
	}
	r.port = r.fixedPort
	go r.serve(server, listener)
	r.server = server
	return nil
}

// serve runs the HTTP server until the listener is closed. An unexpected
// Serve error settles the in-flight flight so a waiting flow fails
// promptly instead of hanging until its context expires.
func (r *callbackReceiver) serve(server *http.Server, listener net.Listener) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if flight := r.current(); flight != nil {
			flight.settle(nil, err)
		}
	}
}

// release shuts the listener down and clears it, so the callback port is
// free for the next authorization (or another process). It is the mirror
// of bind and is called once an authorization settles and on close.
//
// The callback page is rendered before the flight settles, so the response
// is already on its way out by the time release runs. Shutdown (not Close)
// lets any in-flight write finish draining before the socket closes.
func (r *callbackReceiver) release() {
	r.mu.Lock()
	server := r.server
	r.server = nil
	r.mu.Unlock()
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}

// handleCallback receives the authorization redirect, hands the result to
// the waiting flow, and renders the page the user sees.
//
// Only the redirect path is treated as a callback. Browsers request extras
// such as /favicon.ico against the same origin, and letting one of those
// settle the flight would abort authorization with an empty code.
func (r *callbackReceiver) handleCallback(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != callbackPath {
		http.NotFound(w, req)
		return
	}

	query := req.URL.Query()
	result := callback.Result{
		Subject:          r.serverName,
		ErrorCode:        query.Get("error"),
		ErrorDescription: query.Get("error_description"),
	}

	// Render the page BEFORE settling the flight. settle unblocks await,
	// which releases the listener; settling first would close the connection
	// out from under this write and show the user a browser error instead
	// of the success page. Writing first keeps the response on the wire.
	if err := callback.Serve(w, result); err != nil {
		slog.Warn("Failed to render OAuth callback page", "error", err)
	}

	// A redirect with no flight waiting means the tab was reloaded or
	// revisited after the flow finished. The page above already described
	// the outcome accurately, so do not disturb any later authorization.
	if flight := r.current(); flight != nil {
		if result.Failed() {
			flight.settle(nil, fmt.Errorf("OAuth error: %s: %s", result.ErrorCode, result.ErrorDescription))
		} else {
			flight.settle(&auth.AuthorizationResult{
				Code:  query.Get("code"),
				State: query.Get("state"),
				// Required by servers that implement RFC 9207.
				Iss: query.Get("iss"),
			}, nil)
		}
	}
}

func (r *callbackReceiver) fetchAuthorizationCode(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
	flight, owned, err := r.begin()
	if err != nil {
		return nil, err
	}
	if !owned {
		// Another request already opened the browser for this server. Wait
		// for that redirect instead of opening a second tab.
		slog.Debug("Joining in-progress MCP OAuth authorization", "name", r.serverName, "waiting", flight.joined.Load())
		return r.await(ctx, flight, false)
	}
	defer r.end(flight)

	// Some authorization servers reject the "resource" query parameter in
	// the authorization URL (RFC 8707) but accept it during token exchange.
	// Strip it from the browser URL to avoid server_error responses.
	authURL := stripResourceParam(args.URL)
	slog.Info("Opening browser for MCP OAuth authorization")

	r.handler.mu.Lock()
	r.handler.authURL = authURL
	open := r.handler.openURL
	suppress := r.handler.suppressBrowser
	r.handler.mu.Unlock()

	if suppress {
		slog.Info("Browser suppressed; remote client must open the authorization URL", "url", authURL)
	} else if err := open(authURL); err != nil {
		// If the browser can't be opened (headless, remote SSH), keep
		// the callback listener running and tell the user to open the
		// URL manually.
		slog.Warn("Failed to open browser automatically", "error", err)
		slog.Info("Please open the following URL in your browser to authorize", "url", authURL)
	}

	return r.await(ctx, flight, true)
}

// await blocks until the flight settles or ctx is cancelled. The owner
// releases the callback listener once the flow is done, so the port is
// free again as soon as the authorization completes (or is abandoned).
func (r *callbackReceiver) await(ctx context.Context, flight *authFlight, owned bool) (*auth.AuthorizationResult, error) {
	select {
	case <-flight.done:
		if owned {
			r.release()
		}
		if flight.err != nil {
			slog.Error("MCP OAuth authorization failed", "error", flight.err)
			return nil, flight.err
		}
		slog.Info("MCP OAuth authorization completed")
		return flight.result, nil
	case <-ctx.Done():
		slog.Warn("MCP OAuth authorization cancelled")
		if owned {
			// Abandoning the tab we opened; make sure nobody keeps
			// waiting on a redirect that is no longer coming.
			flight.settle(nil, ctx.Err())
			r.release()
		}
		return nil, ctx.Err()
	}
}

// close shuts the receiver down permanently and fails any authorization
// still waiting on a redirect, so a pending flow ends promptly instead of
// hanging until its context expires. After close, bind refuses to start a
// new listener.
func (r *callbackReceiver) close() {
	r.mu.Lock()
	r.closed = true
	server, flight := r.server, r.flight
	r.server = nil
	r.flight = nil
	r.mu.Unlock()

	if server != nil {
		_ = server.Close()
	}
	if flight != nil {
		flight.settle(nil, errors.New("OAuth callback listener closed"))
	}
}
