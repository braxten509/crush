package webtool

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
)

func TestFetchWithoutSessionNamesFetching(t *testing.T) {
	t.Parallel()

	tool := NewFetchTool(&mockPermissionService{}, t.TempDir(), nil)
	input, err := json.Marshal(tools.FetchParams{URL: "https://example.com", Format: "text"})
	require.NoError(t, err)

	_, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "fetch", Name: tools.FetchToolName, Input: string(input)})
	require.EqualError(t, err, "session ID is required for fetching a URL")
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

func (m *mockPermissionService) GrantPersistent(req permission.PermissionRequest) bool {
	return true
}

func (m *mockPermissionService) AutoApproveSession(sessionID string) {}

func (m *mockPermissionService) SetSkipRequests(skip bool) {}

func (m *mockPermissionService) SkipRequests() bool {
	return false
}

func (m *mockPermissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}
