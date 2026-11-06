package backend

import (
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
)

// GrantPermission grants, denies, or persistently grants a permission
// request. The returned bool reports whether this call resolved the
// pending request (true) or found it already resolved by a previous
// caller (false). A false return is not an error.
func (b *Backend) GrantPermission(workspaceID string, req proto.PermissionGrant) (bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return false, err
	}
	return o.ResolvePermission(proto.PermissionRequestToDomain(req.Permission), req.Action)
}

// SetPermissionMode sets the permission mode for a workspace, then sends
// config_changed (the ConfigEvent hook: no MCP reconcile) so every app on
// the project re-reads the workspace and its reported mode.
//
// Only the permission service is updated; the config override is left
// holding the startup flag on purpose. The override is a seed that
// app.New reads once at construction, and config.Overrides() hands
// back a bare pointer with no locking, so writing it here would race
// the readers on the CreateWorkspace path for no benefit. Leaving the
// service as the single writable source of truth is what keeps the
// enforced mode and the mode reported to clients from drifting apart.
func (b *Backend) SetPermissionMode(workspaceID string, mode permission.PermissionMode) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.App.Permissions.SetPermissionMode(mode)
	o.configEvent()
	return nil
}

// GetPermissionMode returns the permission mode the workspace enforces.
func (b *Backend) GetPermissionMode(workspaceID string) (permission.PermissionMode, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return permission.PermissionModeNormal, err
	}
	return o.App.Permissions.PermissionMode(), nil
}

// SetPermissionsSkip is the on/off form of [Backend.SetPermissionMode],
// kept for apps that still speak it (the desktop's permissions/skip).
// Skipping keeps a mode that already skips prompts (yolo or sysadmin) and
// otherwise turns yolo on; not skipping returns to normal. Like
// SetPermissionMode it sends config_changed so every app on the project
// re-reads it.
func (b *Backend) SetPermissionsSkip(workspaceID string, skip bool) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	mode := permission.PermissionModeNormal
	if skip {
		mode = permission.PermissionModeYolo
		if current := o.App.Permissions.PermissionMode(); current.SkipsPrompts() {
			mode = current
		}
	}
	o.App.Permissions.SetPermissionMode(mode)
	o.configEvent()
	return nil
}

// GetPermissionsSkip returns whether permission prompts are skipped: the
// workspace runs in yolo or sysadmin mode.
func (b *Backend) GetPermissionsSkip(workspaceID string) (bool, error) {
	mode, err := b.GetPermissionMode(workspaceID)
	if err != nil {
		return false, err
	}
	return mode.SkipsPrompts(), nil
}

// AnswerQuestion submits answers for a question. The returned bool
// reports whether this call resolved the pending request (true) or
// found it already resolved by a previous caller (false).
func (b *Backend) AnswerQuestion(workspaceID string, req proto.QuestionAnswer) (bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return false, err
	}
	return o.App.Questions.Answer(proto.QuestionAnswerToDomain(req)), nil
}

// CancelQuestion cancels the pending question for a workspace.
// Returns true if a question was cancelled, false if none was
// pending.
func (b *Backend) CancelQuestion(workspaceID string) (bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return false, err
	}
	return o.App.Questions.Cancel(), nil
}
