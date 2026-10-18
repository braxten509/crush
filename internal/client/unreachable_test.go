package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// answerStatus returns the error a client call gets when the server answers
// status with an error body.
func answerStatus(t *testing.T, status int, code string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestError(w, status, code)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(t.TempDir(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	_, err = c.EngineInfo(t.Context())
	require.Error(t, err)
	return err
}

func writeTestError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"message":"answered %d","code":%q}`, status, code)
}

func TestIsUnreachable(t *testing.T) {
	t.Parallel()

	require.False(t, IsUnreachable(nil))

	// A client whose engine socket is gone.
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("", "cu")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		missing := filepath.Join(dir, "gone.sock")
		c, err := NewClient(dir, "unix", missing)
		require.NoError(t, err)
		_, err = c.EngineInfo(t.Context())
		require.True(t, IsUnreachable(err), "no socket file: %v", err)

		// A socket file nobody listens on (an engine that was killed).
		stale := filepath.Join(dir, "stale.sock")
		ln, err := net.Listen("unix", stale) //nolint:noctx
		require.NoError(t, err)
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		require.NoError(t, ln.Close())
		c, err = NewClient(dir, "unix", stale)
		require.NoError(t, err)
		_, err = c.EngineInfo(t.Context())
		require.True(t, IsUnreachable(err), "stale socket: %v", err)
	}

	// An engine that is exiting.
	require.True(t, IsUnreachable(answerStatus(t, http.StatusServiceUnavailable, "shutting_down")))

	// Alive engines.
	require.False(t, IsUnreachable(answerStatus(t, http.StatusNotFound, "not_found")), "a 404 means the workspace is gone, not the engine")
	require.False(t, IsUnreachable(answerStatus(t, http.StatusConflict, "client_retired")))
	require.False(t, IsUnreachable(answerStatus(t, http.StatusUpgradeRequired, "protocol_mismatch")))
	require.False(t, IsUnreachable(answerStatus(t, http.StatusInternalServerError, "internal")))
	require.False(t, IsUnreachable(fmt.Errorf("dial: %w", timeoutError{})), "a timeout is not a dead engine")
	require.False(t, IsUnreachable(errors.New("boom")))
}

// TestEventStreamEndsWhenEngineDies: an engine that dies mid-stream ends
// the body with io.ErrUnexpectedEOF, not io.EOF. The events channel must
// close, so the subscriber reconnects and recovers, instead of retrying
// the dead body forever.
func TestEventStreamEndsWhenEngineDies(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// Drop the connection without ending the chunked body.
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(t.TempDir(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	events, err := c.SubscribeEvents(t.Context(), "ws-1")
	require.NoError(t, err)
	select {
	case _, ok := <-events:
		require.False(t, ok, "no event was sent")
	case <-time.After(5 * time.Second):
		t.Fatal("the events channel did not close after the stream died")
	}
}

func TestEngineInfoRoundTrip(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/engine", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":1,"version":"v1.2.3","pid":42,"data_dir":"/p/.crush","address":"unix:///s.sock","apps":2,"closing":true}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(t.TempDir(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	info, err := c.EngineInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, proto.EngineInfo{Protocol: 1, Version: "v1.2.3", PID: 42, DataDir: "/p/.crush", Address: "unix:///s.sock", Apps: 2, Closing: true}, info)
}

func TestCreateWorkspaceSendsProtocol(t *testing.T) {
	t.Parallel()

	var got proto.Workspace
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ws-1","path":"/p"}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(t.TempDir(), "tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	_, err = c.CreateWorkspace(t.Context(), proto.Workspace{Path: "/p"})
	require.NoError(t, err)
	require.Equal(t, proto.ProtocolVersion, got.Protocol)
	require.Equal(t, c.ClientID(), got.ClientID)
}
