package workspace

import (
	"context"
	"fmt"
	"github.com/charmbracelet/crush/internal/message"
)

// TreeManagement deliberately stays optional until the server and phone
// protocols gain branch revision checks.
type TreeManagement interface {
	Tree(context.Context, string) ([]message.TreeEntry, error)
	SwitchTree(context.Context, string, string) error
	LabelTree(context.Context, string, string, string) error
}

func (w *AppWorkspace) Tree(ctx context.Context, id string) ([]message.TreeEntry, error) {
	tree, ok := w.app.Messages.(message.TreeService)
	if !ok {
		return nil, fmt.Errorf("tree storage unavailable")
	}
	return tree.Tree(ctx, id)
}
func (w *AppWorkspace) SwitchTree(ctx context.Context, id, target string) error {
	switcher, ok := w.app.AgentCoordinator.(interface {
		SwitchTree(context.Context, string, string) error
	})
	if !ok {
		return fmt.Errorf("tree switching requires a local agent")
	}
	return switcher.SwitchTree(ctx, id, target)
}
func (w *AppWorkspace) LabelTree(ctx context.Context, id, target, label string) error {
	tree, ok := w.app.Messages.(message.TreeService)
	if !ok {
		return fmt.Errorf("tree storage unavailable")
	}
	return tree.LabelTree(ctx, id, target, label)
}
