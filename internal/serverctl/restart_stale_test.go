package serverctl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/stretchr/testify/require"
)

// controlServerOpts describes the server checkEngine will find.
type controlServerOpts struct {
	// vi is what /version reports.
	vi proto.VersionInfo
	// legacy makes the server reject the conditional shutdown command as
	// unknown, the way every server released before the idleness guard
	// does. Such a server obeys a plain shutdown unconditionally, so a
	// client must never send it one without first verifying idleness.
	legacy bool
	// busy makes the server decline the shutdown because it is in use.
	busy bool
	// workspaces is the count the server reports for GET /workspaces,
	// used by the legacy fallback to decide whether it is safe to send
	// the unconditional shutdown. Defaults to 0 (idle).
	workspaces int
}

// controlLog records the control commands a server received.
type controlLog struct {
	mu       sync.Mutex
	commands []string
}

func (l *controlLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.commands...)
}

func newControlServer(t *testing.T, opts controlServerOpts) (*url.URL, *controlLog) {
	t.Helper()
	log := &controlLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/version":
			require.NoError(t, json.NewEncoder(w).Encode(opts.vi))
		case "/v1/workspaces":
			ws := make([]proto.Workspace, opts.workspaces)
			require.NoError(t, json.NewEncoder(w).Encode(ws))
		case "/v1/control":
			var req proto.ServerControl
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			log.mu.Lock()
			log.commands = append(log.commands, req.Command)
			log.mu.Unlock()
			switch {
			case opts.legacy && req.Command != proto.ServerControlShutdown:
				http.Error(w, "unknown command", http.StatusBadRequest)
			case opts.busy:
				http.Error(w, "server is hosting live workspaces", http.StatusConflict)
			default:
				w.WriteHeader(http.StatusOK)
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return &url.URL{Scheme: "tcp", Host: u.Host}, log
}

// staleVersion never matches this build but speaks this protocol, so
// checkEngine considers the server a candidate for replacement by the
// build rule.
func staleVersion() proto.VersionInfo {
	return proto.VersionInfo{
		Version:  "not-" + version.Version,
		BuildID:  "stale-build",
		Protocol: proto.ProtocolVersion,
	}
}

// checkControlServer runs checkEngine against the server at hostURL.
func checkControlServer(t *testing.T, hostURL *url.URL) (bool, error) {
	t.Helper()
	c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
	require.NoError(t, err)
	return checkEngine(t.Context(), c)
}

// TestCheckEngine_HonorsRefusedShutdown is the headline regression.
// BuildID derives from the executable's mtime, so any rebuild — including
// every `go run` — makes a second session look like an upgrade. Launching
// it used to shut down the first session's server, taking its workspaces
// with it and stranding it on a dead workspace ID. Now the server has the
// final say, and a refusal means "keep using me".
func TestCheckEngine_HonorsRefusedShutdown(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{
		vi:   staleVersion(),
		busy: true,
	})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.True(t, reuse, "a refused shutdown means the server is reused")
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, log.all(),
		"the request must be the conditional one the server can veto")
}

// TestCheckEngine_LeavesLegacyServerBusy is the upgrade-transition case. A
// server predating the idleness guard rejects the conditional command, and
// the only command it does understand is unconditional — sending that
// would kill every session it hosts. When the server reports active
// workspaces, it must be left alone.
func TestCheckEngine_LeavesLegacyServerBusy(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{
		vi:         staleVersion(),
		legacy:     true,
		workspaces: 3,
	})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.True(t, reuse, "a legacy server with workspaces must be reused, not replaced")
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, log.all(),
		"the unconditional shutdown must never be sent when workspaces are active")
}

// TestCheckEngine_ShutsDownLegacyServerWhenIdle covers the case where a
// legacy server (predating shutdown_if_idle) is idle. The client verifies
// idleness via ListWorkspaces, then sends the unconditional shutdown the
// old server understands.
func TestCheckEngine_ShutsDownLegacyServerWhenIdle(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{
		vi:     staleVersion(),
		legacy: true,
	})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.False(t, reuse, "an idle legacy server must be replaced")
	require.Equal(t,
		[]string{proto.ServerControlShutdownIfIdle, proto.ServerControlShutdown},
		log.all(),
		"the conditional shutdown is tried first, then the unconditional fallback")
}

// TestCheckEngine_RestartsWhenServerAgrees preserves the upgrade path: a
// mismatched server that grants the shutdown is replaced promptly.
func TestCheckEngine_RestartsWhenServerAgrees(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{vi: staleVersion()})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.False(t, reuse, "an idle stale server must be restarted")
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, log.all())
}

// TestCheckEngine_MatchingServerUntouched is the control: same protocol,
// version and build ID means no control request at all.
func TestCheckEngine_MatchingServerUntouched(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{
		vi: proto.VersionInfo{Version: version.Version, BuildID: version.BuildID, Protocol: proto.ProtocolVersion},
	})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.True(t, reuse)
	require.Empty(t, log.all())
}

// TestCheckEngine_ReplacesIdleOtherProtocol: an engine from before the
// protocol field reports 0, and an idle one is replaced.
func TestCheckEngine_ReplacesIdleOtherProtocol(t *testing.T) {
	t.Parallel()

	hostURL, log := newControlServer(t, controlServerOpts{
		vi: proto.VersionInfo{Version: version.Version, BuildID: version.BuildID},
	})

	reuse, err := checkControlServer(t, hostURL)
	require.NoError(t, err)
	require.False(t, reuse, "an idle engine of another protocol is replaced, even with the same build")
	require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, log.all())
}

// TestCheckEngine_NeverReusesBusyOtherProtocol: a busy engine of another
// protocol is refused with ErrProtocolMismatch, and left running.
func TestCheckEngine_NeverReusesBusyOtherProtocol(t *testing.T) {
	t.Parallel()

	for _, legacy := range []bool{false, true} {
		hostURL, log := newControlServer(t, controlServerOpts{
			vi:         proto.VersionInfo{Version: "v0.1.0", Protocol: proto.ProtocolVersion + 1},
			busy:       true,
			legacy:     legacy,
			workspaces: 1,
		})

		reuse, err := checkControlServer(t, hostURL)
		require.False(t, reuse)
		require.ErrorIs(t, err, backend.ErrProtocolMismatch)
		require.EqualError(t, err, fmt.Sprintf("The Crush engine for this project is busy and speaks protocol %d; this crush speaks %d. Wait for its work to finish or close the apps using it, then try again.",
			proto.ProtocolVersion+1, proto.ProtocolVersion))
		require.Equal(t, []string{proto.ServerControlShutdownIfIdle}, log.all(), "only the conditional shutdown is asked")
	}
}

// TestCreateWorkspaceOnLiveServer_RetriesPastExitingServer covers the
// startup race left over from making the shutdown decision final: a client
// can reach a server in the instant between its committing to an idle
// shutdown and its socket disappearing. Failing the CLI there would be a
// regression, so the client has to bring up a replacement and ask again.
func TestCreateWorkspaceOnLiveServer_RetriesPastExitingServer(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	replaced := 0
	creates := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/workspaces", r.URL.Path)
		mu.Lock()
		creates++
		// Only once the client has stood up a replacement does the create
		// succeed, so passing this test requires the retry to be real.
		serve := replaced > 0
		mu.Unlock()
		if !serve {
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(proto.Workspace{ID: "ws-new"}))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	c, err := client.NewClient(t.TempDir(), "tcp", u.Host)
	require.NoError(t, err)

	ws, err := CreateWorkspace(t.Context(), c, prototest.CreateRequest(proto.Workspace{Path: t.TempDir()}),
		func() error {
			mu.Lock()
			replaced++
			mu.Unlock()
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, "ws-new", ws.ID)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, replaced, "exactly one replacement must be spawned")
	require.Equal(t, 2, creates)
}

// TestCreateWorkspaceOnLiveServer_GivesUpOnOtherFailures makes sure the
// retry is scoped to the shutdown race and does not paper over real
// errors by respawning servers.
func TestCreateWorkspaceOnLiveServer_GivesUpOnOtherFailures(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	c, err := client.NewClient(t.TempDir(), "tcp", u.Host)
	require.NoError(t, err)

	_, err = CreateWorkspace(t.Context(), c, prototest.CreateRequest(proto.Workspace{Path: t.TempDir()}),
		func() error {
			t.Fatal("a non-lifecycle failure must not spawn a replacement server")
			return nil
		})
	require.Error(t, err)
}
