package message

import (
	"context"
	"errors"
	"log/slog"

	"github.com/charmbracelet/crush/internal/filehistory"
)

func WithFileHistory(store *filehistory.Store) ServiceOption {
	return func(s *service) { s.fileHistory = store }
}

type FileHistoryService interface {
	FileHistory() *filehistory.Store
}

func (s *service) FileHistory() *filehistory.Store { return s.fileHistory }
func (s *service) captureFiles(ctx context.Context, id, sessionID string, parts []ContentPart) {
	if s.fileHistory == nil {
		return
	}
	for _, part := range parts {
		if result, ok := part.(ToolResult); ok {
			if err := s.fileHistory.Capture(ctx, sessionID, id, result.ToolCallID, result.Review); err != nil {
				slog.Warn("Could not capture file history", "message", id, "error", err)
			}
		}
	}
}

type restoreContextKey struct{}

// FileRestoreRequest is confined to a single idle, approved navigation.
type FileRestoreRequest struct {
	Plan   *filehistory.Plan
	Result filehistory.Result
}

func WithFileRestore(ctx context.Context, request *FileRestoreRequest) context.Context {
	return context.WithValue(ctx, restoreContextKey{}, request)
}
func (s *service) restoreFiles(ctx context.Context, operation func() error) error {
	request, _ := ctx.Value(restoreContextKey{}).(*FileRestoreRequest)
	if request == nil {
		return operation()
	}
	if s.fileHistory == nil {
		return errors.New("file history is unavailable")
	}
	var err error
	request.Result, err = s.fileHistory.Apply(ctx, request.Plan)
	if err != nil {
		return err
	}
	if err = operation(); err != nil {
		// Keep the undo snapshot available even when a database failure prevents
		// navigation. No attempt to hide partial filesystem effects.
		return errors.New(err.Error() + ". " + request.Result.Notice())
	}
	return nil
}
