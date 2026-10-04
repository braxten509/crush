package workspace

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
)

type FileRestoration interface {
	PreviewFiles(context.Context, string, string, bool) (*filehistory.Plan, error)
	PreviewUndoFiles(context.Context, string) (*filehistory.Plan, error)
	UndoFiles(context.Context, *filehistory.Plan) (filehistory.Result, error)
}

func (w *AppWorkspace) fileHistory() (*filehistory.Store, error) {
	service, ok := w.app.Messages.(message.FileHistoryService)
	if !ok || service.FileHistory() == nil {
		return nil, errors.New("file history unavailable")
	}
	return service.FileHistory(), nil
}
func (w *AppWorkspace) PreviewFiles(ctx context.Context, id, target string, fork bool) (*filehistory.Plan, error) {
	store, err := w.fileHistory()
	if err != nil {
		return nil, err
	}
	if err = w.app.Messages.FlushAll(ctx); err != nil {
		return nil, err
	}
	if fork {
		nodes, err := w.Tree(ctx, id)
		if err != nil {
			return nil, err
		}
		found := false
		for _, node := range nodes {
			if node.MessageID == target {
				target = node.ParentID
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("choose a user prompt to fork")
		}
	}
	return store.Preview(ctx, id, target)
}
func (w *AppWorkspace) PreviewUndoFiles(ctx context.Context, id string) (*filehistory.Plan, error) {
	store, err := w.fileHistory()
	if err != nil {
		return nil, err
	}
	return store.UndoPreview(ctx, id)
}
func (w *AppWorkspace) UndoFiles(ctx context.Context, plan *filehistory.Plan) (filehistory.Result, error) {
	agent, ok := w.app.AgentCoordinator.(interface {
		UndoFiles(context.Context, *filehistory.Plan) (filehistory.Result, error)
	})
	if !ok {
		return filehistory.Result{}, errors.New("file restore needs a local agent")
	}
	return agent.UndoFiles(ctx, plan)
}
