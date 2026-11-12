package webtool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
)

func runFetchTool(t *testing.T, client *http.Client, url string) (fantasy.ToolResponse, error) {
	t.Helper()

	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")
	tool := NewFetchTool(&mockPermissionService{}, t.TempDir(), client)

	input, err := json.Marshal(tools.FetchParams{URL: url, Format: "text"})
	require.NoError(t, err)

	return tool.Run(ctx, fantasy.ToolCall{
		ID:    "fetch-call",
		Name:  tools.FetchToolName,
		Input: string(input),
	})
}

// A fetch that never completes must be reported to the model as a failed tool
// result. Returning a Go error instead ends the whole agent turn.
func TestFetchToolReportsNetworkFailuresAsToolErrors(t *testing.T) {
	t.Parallel()

	t.Run("connection refused", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("unused"))
		}))
		url := server.URL
		server.Close()

		resp, err := runFetchTool(t, server.Client(), url)
		require.NoError(t, err)
		require.True(t, resp.IsError)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.Write([]byte("too late"))
		}))
		defer server.Close()

		client := &http.Client{Timeout: 20 * time.Millisecond}
		resp, err := runFetchTool(t, client, server.URL)
		require.NoError(t, err)
		require.True(t, resp.IsError)
	})
}

func TestFetchToolReportsStatusCodesAsToolErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	resp, err := runFetchTool(t, server.Client(), server.URL)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "404")
}

func TestFetchToolReturnsContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello from fetch"))
	}))
	defer server.Close()

	resp, err := runFetchTool(t, server.Client(), server.URL)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "hello from fetch")
}

// mockPermissionService grants every request.
type mockPermissionService struct {
	*pubsub.Broker[permission.PermissionRequest]
}

func (m *mockPermissionService) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	return true, nil
}

func (m *mockPermissionService) Grant(req permission.PermissionRequest) bool { return true }

func (m *mockPermissionService) Deny(req permission.PermissionRequest) bool { return true }

func (m *mockPermissionService) ActiveRequest() (permission.PermissionRequest, bool) {
	return permission.PermissionRequest{}, false
}

func (m *mockPermissionService) GrantPersistent(req permission.PermissionRequest) bool {
	return true
}

func (m *mockPermissionService) AutoApproveSession(sessionID string) {}

func (m *mockPermissionService) SetPermissionMode(mode permission.PermissionMode) {}

func (m *mockPermissionService) PermissionMode() permission.PermissionMode {
	return permission.PermissionModeNormal
}

func (m *mockPermissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}

func (m *mockPermissionService) SubscribeModeChanges(ctx context.Context) <-chan pubsub.Event[permission.ModeChangedEvent] {
	return make(<-chan pubsub.Event[permission.ModeChangedEvent])
}
