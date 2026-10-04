package agent

import (
	"context"
	"errors"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/message"
)

func (c *coordinator) UndoFiles(ctx context.Context, plan *filehistory.Plan) (result filehistory.Result, err error) {
	err = c.withIdleTree(ctx, plan.SessionID, func(_ *sessionAgent, _ message.TreeService) error {
		service, ok := c.messages.(message.FileHistoryService)
		if !ok || service.FileHistory() == nil {
			return errors.New("file history unavailable")
		}
		var restoreErr error
		result, restoreErr = service.FileHistory().Apply(ctx, plan)
		return restoreErr
	})
	return
}
