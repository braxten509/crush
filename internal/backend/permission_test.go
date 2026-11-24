package backend

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A workspace started by an old client always skips prompts, and old mode
// requests cannot reintroduce prompts or disable command guards.
func TestWorkspacePermissionModeIsFixed(t *testing.T) {
	xdgIsolated(t)
	b := New(context.Background(), nil, func() {})
	b.SetCreateGrace(2 * time.Second)
	t.Cleanup(func() { drainBackend(t, b) })

	args := protoWS(t.TempDir(), t.TempDir(), uuid.New().String())
	args.PermissionMode = proto.WorkspacePermissionModeNormal
	ws, created, err := b.CreateWorkspace(args)
	require.NoError(t, err)
	require.True(t, created.YOLO)

	require.NoError(t, b.SetPermissionMode(ws.ID, permission.PermissionModeNormal))
	require.NoError(t, b.SetPermissionMode(ws.ID, permission.PermissionModeSysadmin))
	require.NoError(t, b.SetPermissionsSkip(ws.ID, false))
	mode, err := b.GetPermissionMode(ws.ID)
	require.NoError(t, err)
	require.Equal(t, permission.PermissionModeYolo, mode)
}
