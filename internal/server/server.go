package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
)

// maxUnixSocketPathLen is the maximum length of a Unix domain socket
// path. The macOS sun_path field is 104 bytes; Linux allows 108. We
// use 104 so the resulting path is portable across both platforms.
const maxUnixSocketPathLen = 104

// ErrServerClosed is returned when the server is closed.
var ErrServerClosed = http.ErrServerClosed

// ParseHostURL parses a host URL into a [url.URL].
func ParseHostURL(host string) (*url.URL, error) {
	proto, addr, ok := strings.Cut(host, "://")
	if !ok {
		return nil, fmt.Errorf("invalid host format: %s", host)
	}

	var basePath string
	if proto == "tcp" {
		parsed, err := url.Parse("tcp://" + addr)
		if err != nil {
			return nil, fmt.Errorf("invalid tcp address: %v", err)
		}
		addr = parsed.Host
		basePath = parsed.Path
	}
	return &url.URL{
		Scheme: proto,
		Host:   addr,
		Path:   basePath,
	}, nil
}

// defaultSocketName is the legacy shared server's socket in the private
// socket folder.
const defaultSocketName = "crush.sock"

// DefaultHost returns the default server host: the legacy shared
// server's address.
//
// On Windows the address is a named pipe under \\.\pipe\, named
// crush-<uid>.sock (crush.sock when the uid cannot be determined). On
// Unix platforms it is unix://<PrivateSocketDir>/crush.sock, a socket in
// a checked private folder (engine-design.md 5.1 d); the 104-byte limit is
// checked for crush.sock itself. When no folder is private enough it
// returns "": there is no safe default, and callers report
// [PrivateSocketDir]'s error instead.
func DefaultHost() string {
	if runtime.GOOS == "windows" {
		sock := "crush.sock"
		usr, err := user.Current()
		if err == nil && usr.Uid != "" {
			sock = fmt.Sprintf("crush-%s.sock", usr.Uid)
		}
		return fmt.Sprintf("npipe:////./pipe/%s", sock)
	}
	dir, err := privateSocketDir(defaultSocketName)
	if err != nil {
		return ""
	}
	return "unix://" + filepath.Join(dir, defaultSocketName)
}

// Server represents a Crush server bound to a specific address.
type Server struct {
	// Addr can be a TCP address, a Unix socket path, or a Windows named pipe.
	Addr    string
	network string

	h  *http.Server
	ln net.Listener

	backend *backend.Backend
	logger  *slog.Logger
}

// SetLogger sets the logger for the server.
func (s *Server) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

// Backend returns the server's backend. Intended for integration tests
// that drive lifecycle transitions (detach, grace tuning) against a live
// HTTP surface.
func (s *Server) Backend() *backend.Backend {
	return s.backend
}

// NewServer creates a new [Server] with the given network and address.
func NewServer(cfg *config.ConfigStore, network, address string) *Server {
	s := new(Server)
	s.Addr = address
	s.network = network

	// The backend is created with a shutdown callback that triggers
	// a graceful server shutdown (e.g. when the last workspace is
	// removed).
	s.backend = backend.New(context.Background(), cfg, func() {
		go func() {
			slog.Info("Shutting down server...")
			if err := s.Shutdown(context.Background()); err != nil {
				slog.Error("Failed to shutdown server", "error", err)
			}
		}()
	})
	s.installHandler()
	if network == "tcp" {
		s.h.Addr = address
	}
	return s
}

// installHandler builds the protocol/router around s.backend and
// assigns the resulting http.Server to s.h. Extracted from
// [NewServer] so test harnesses can wire a Server around a
// pre-constructed backend.
func (s *Server) installHandler() {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	c := &controllerV1{backend: s.backend, server: s}
	mux := http.NewServeMux()
	for _, e := range c.endpoints() {
		mux.HandleFunc(e.Method()+" "+e.Path(), e.Handler())
	}
	mux.HandleFunc("GET /v1/docs/", c.handleDocsIndex)
	mux.HandleFunc("GET /v1/docs/openapi.json", c.handleDocsSpec)
	s.h = &http.Server{
		Protocols: &p,
		Handler:   s.recoverHandler(s.loggingHandler(mux)),
	}
}

// Handler returns the server's HTTP handler. Exposed so test harnesses
// can wrap it in an httptest.Server without going through the
// production listener setup.
func (s *Server) Handler() http.Handler {
	return s.h.Handler
}

// Serve accepts incoming connections on the listener.
func (s *Server) Serve(ln net.Listener) error {
	return s.h.Serve(ln)
}

// ListenAndServe starts the server and begins accepting connections.
func (s *Server) ListenAndServe() error {
	if s.ln != nil {
		return fmt.Errorf("server already started")
	}
	ln, removedStale, err := listen(s.network, s.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.Addr, err)
	}
	if removedStale && s.logger != nil {
		s.logger.Warn("Removed stale socket before binding", "address", s.Addr)
	}
	// An engine waits for its first app from the moment it listens.
	s.backend.ArmFirstJoinGrace()
	return s.Serve(ln)
}

func (s *Server) closeListener() {
	if s.ln != nil {
		s.ln.Close()
		s.ln = nil
	}
}

// Close force closes all listeners and connections.
func (s *Server) Close() error {
	defer func() { s.closeListener() }()
	return s.h.Close()
}

// Shutdown gracefully shuts down the server without interrupting active
// connections.
func (s *Server) Shutdown(ctx context.Context) error {
	defer func() { s.closeListener() }()
	return s.h.Shutdown(ctx)
}

func (s *Server) logDebug(r *http.Request, msg string, args ...any) {
	if s.logger != nil {
		s.logger.With(
			slog.String("method", r.Method),
			slog.String("url", r.URL.String()),
			slog.String("remote_addr", r.RemoteAddr),
		).Debug(msg, args...)
	}
}

func (s *Server) logError(r *http.Request, msg string, args ...any) {
	if s.logger != nil {
		s.logger.With(
			slog.String("method", r.Method),
			slog.String("url", r.URL.String()),
			slog.String("remote_addr", r.RemoteAddr),
		).Error(msg, args...)
	}
}
