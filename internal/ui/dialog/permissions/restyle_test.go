package permissions

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kittest"
)

var _ dialog.StyleRefresher = (*Permissions)(nil)

func TestRefreshStyles_PermissionsDiff(t *testing.T) {
	t.Parallel()
	kittest.RequireRestyled(t, func(com *common.Common) *Permissions {
		return NewPermissions(com, permission.PermissionRequest{
			ID:         "perm",
			ToolCallID: "call",
			ToolName:   tools.EditToolName,
			Params: tools.EditPermissionsParams{
				FilePath:   "main.go",
				OldContent: "package main\n\nfunc main() {}\n",
				NewContent: "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
			},
		})
	})
}
