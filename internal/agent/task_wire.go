package agent

import "github.com/charmbracelet/crush/internal/proto"

// HelperFromTask is the wire form of a helper.
func HelperFromTask(t Task) proto.Helper {
	return proto.Helper{
		ID:             t.ID,
		SessionID:      t.SessionID,
		ChildSessionID: t.ChildID,
		Name:           t.Name,
		CLI:            t.CLI,
		Model:          t.Model,
		Account:        t.Account,
		Effort:         t.Effort,
		Fast:           t.Fast,
		ReadOnly:       t.ReadOnly,
		Status:         string(t.Status),
		Started:        t.Started,
		Ended:          t.Ended,
		Delivered:      t.Delivered,
		StartedBy:      t.StartedBy,
	}
}
