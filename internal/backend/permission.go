package backend

import (
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

// SetPermissionsSkip sets whether permission prompts are skipped, then
// sends config_changed (the ConfigEvent hook: no MCP reconcile) so every
// app on the project re-reads it.
func (b *Backend) SetPermissionsSkip(workspaceID string, skip bool) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.App.Permissions.SetSkipRequests(skip)
	o.configEvent()
	return nil
}

// GetPermissionsSkip returns whether permission prompts are skipped.
func (b *Backend) GetPermissionsSkip(workspaceID string) (bool, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return false, err
	}
	return o.App.Permissions.SkipRequests(), nil
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
