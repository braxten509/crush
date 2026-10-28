package shelltool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// A read-only bash tool runs commands in the project, but they can't
// change it.
func TestReadOnlyBashCantChangeProject(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex isn't installed")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	// The lock always lets programs write in the temporary folder, so the
	// project must live elsewhere.
	project, err := os.MkdirTemp(home, ".crush-readonly-bash-test-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(project) })

	permissions := &mockBashPermissionService{Broker: pubsub.NewBroker[permission.PermissionRequest]()}
	tool := NewReadOnlyBashTool(permissions, project, t.TempDir(), &config.Attribution{TrailerStyle: config.TrailerStyleNone}, nil)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "read-only-bash")
	resp := runBashTool(t, tool, ctx, tools.BashParams{Command: `pwd; echo "it's" read; touch project-file`, Description: "write attempt"})
	require.Contains(t, resp.Content, project, "commands run in the project")
	require.Contains(t, resp.Content, "it's read", "the command's own quoting survives")
	require.NoFileExists(t, filepath.Join(project, "project-file"), resp.Content)
}
