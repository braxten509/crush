package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// newBareHarness is a test server with no workspace.
func newBareHarness(t *testing.T) *e2eHarness {
	t.Helper()
	h := &e2eHarness{}
	h.installServer(t)
	return h
}

func TestEngineInfoReportsClosing(t *testing.T) {
	t.Parallel()
	h := newBareHarness(t)

	info := callAPI[proto.EngineInfo](t, h, http.MethodGet, "/v1/engine", nil)
	require.Equal(t, proto.ProtocolVersion, info.Protocol)
	require.Equal(t, os.Getpid(), info.PID)
	require.NotEmpty(t, info.Version)
	require.False(t, info.StartedAt.IsZero())
	require.Empty(t, info.DataDir, "the legacy server has no project")
	require.False(t, info.Closing)

	require.True(t, h.backend.ShutdownIfIdle())
	require.True(t, h.shutdownHit.Load())
	info = callAPI[proto.EngineInfo](t, h, http.MethodGet, "/v1/engine", nil)
	require.True(t, info.Closing, "a closing engine says so while it still answers")
}

// requireAPIError checks an error answer's status and code, and returns its
// message.
func requireAPIError(t *testing.T, status int, data []byte, wantStatus int, wantCode string) string {
	t.Helper()
	require.Equal(t, wantStatus, status, string(data))
	var body proto.Error
	require.NoError(t, json.Unmarshal(data, &body))
	require.Equal(t, wantCode, body.Code)
	return body.Message
}

func TestCreateRefusesProtocolMismatch(t *testing.T) {
	t.Parallel()
	h := newBareHarness(t)

	// Not built with prototest.CreateRequest: an app that sends no
	// protocol, like a crush from before the field.
	status, data := doAPI(t, h, http.MethodPost, "/v1/workspaces", proto.Workspace{Path: t.TempDir(), ClientID: uuid.NewString()})
	requireAPIError(t, status, data, http.StatusUpgradeRequired, "protocol_mismatch")
	require.Empty(t, h.backend.ListWorkspaces())
}

func TestEngineRefusesOtherProject(t *testing.T) {
	h := newRealCreateHarness(t)
	project := t.TempDir()
	dataDir, err := config.CanonicalDataDir(filepath.Join(project, ".crush"))
	require.NoError(t, err)
	h.backend.SetEngine(dataDir)

	other := t.TempDir()
	status, data := doAPI(t, h, http.MethodPost, "/v1/workspaces",
		prototest.CreateRequest(proto.Workspace{Path: other, ClientID: uuid.NewString()}))
	message := requireAPIError(t, status, data, http.StatusConflict, "wrong_project")
	require.Contains(t, message, dataDir)
	require.Empty(t, h.backend.ListWorkspaces())

	info := callAPI[proto.EngineInfo](t, h, http.MethodGet, "/v1/engine", nil)
	require.Equal(t, dataDir, info.DataDir)
}
