package backend

import (
	"github.com/charmbracelet/crush/internal/proto"
)

// GrantPermission grants, denies, or persistently grants a permission
// request. The returned bool reports whether this call resolved the
// pending request (true) or found it already resolved by a previous
// caller (false). A false return is not an error.
func (b *Backend) GrantPermission(workspaceID string, req proto.PermissionGrant) (bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}

	perm := proto.PermissionRequestToDomain(req.Permission)

	switch req.Action {
	case proto.PermissionAllow:
		return ws.Permissions.Grant(perm), nil
	case proto.PermissionAllowForSession:
		return ws.Permissions.GrantPersistent(perm), nil
	case proto.PermissionDeny:
		return ws.Permissions.Deny(perm), nil
	default:
		return false, ErrInvalidPermissionAction
	}
}

// SetPermissionsSkip sets whether permission prompts are skipped.
func (b *Backend) SetPermissionsSkip(workspaceID string, skip bool) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	ws.Permissions.SetSkipRequests(skip)
	return nil
}

// GetPermissionsSkip returns whether permission prompts are skipped.
func (b *Backend) GetPermissionsSkip(workspaceID string) (bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}

	return ws.Permissions.SkipRequests(), nil
}
