package workspace

import (
	"context"
	"fmt"
	"github.com/charmbracelet/crush/internal/message"
)

// TreeManagement deliberately stays optional until the server and phone
// protocols gain branch revision checks.
type TreeManagement interface {
	TreeMessage(context.Context, string) (message.Message, error)
	Tree(context.Context, string) ([]message.TreeEntry, error)
	SwitchTree(context.Context, string, string) error
	LabelTree(context.Context, string, string, string) error
	NavigateTree(context.Context, string, string, bool) error
	CopyTree(context.Context, string, string, bool) (string, string, error)
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

func (w *AppWorkspace) NavigateTree(ctx context.Context, id, target string, summary bool) error {
	manager, ok := w.app.AgentCoordinator.(interface {
		NavigateTree(context.Context, string, string, bool) error
	})
	if !ok {
		return fmt.Errorf("session trees need a local workspace")
	}
	return manager.NavigateTree(ctx, id, target, summary)
}
func (w *AppWorkspace) CopyTree(ctx context.Context, id, target string, fork bool) (string, string, error) {
	manager, ok := w.app.AgentCoordinator.(interface {
		CopyTree(context.Context, string, string, bool) (string, string, error)
	})
	if !ok {
		return "", "", fmt.Errorf("session trees need a local workspace")
	}
	return manager.CopyTree(ctx, id, target, fork)
}

func (w *AppWorkspace) TreeMessage(ctx context.Context, id string) (message.Message, error) {
	return w.app.Messages.Get(ctx, id)
}
