package backend

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/proto"
)

// ErrNoPendingQuestion is returned when the chat waits on no question.
var ErrNoPendingQuestion = errors.New("no pending question")

// PendingQuestion returns the question form the chat waits on: the
// pending request of the chat itself or of one of its helpers, whose
// prompts the chat's viewer answers (as the event filter routes them).
func (b *Backend) PendingQuestion(ctx context.Context, workspaceID, sessionID string) (proto.QuestionRequest, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.QuestionRequest{}, err
	}
	if ws.App == nil || ws.Questions == nil {
		return proto.QuestionRequest{}, ErrNoPendingQuestion
	}
	req, ok := ws.Questions.Pending()
	if !ok {
		return proto.QuestionRequest{}, ErrNoPendingQuestion
	}
	if req.SessionID != sessionID {
		root, err := b.RootSessionID(ctx, workspaceID, req.SessionID)
		if err != nil || root != sessionID {
			return proto.QuestionRequest{}, ErrNoPendingQuestion
		}
	}
	return proto.QuestionRequestFromDomain(req), nil
}
