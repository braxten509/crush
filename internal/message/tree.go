package message

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/google/uuid"
)

// TreeService is optional so existing upstream stores and mocks remain valid.
type TreeService interface {
	Tree(context.Context, string) ([]TreeEntry, error)
	SwitchTree(context.Context, string, string) error
	LabelTree(context.Context, string, string, string) error
	TreeRevision(context.Context, string) (int64, error)
	SwitchTreeNote(context.Context, string, string, string, string, string) error
	CopyTree(context.Context, string, string, bool) (string, string, error)
}
type TreeEntry struct {
	db.TreeNode
	Message Message
}
type treeStorage interface {
	SwitchTreeNote(context.Context, string, string, *db.CreateMessageParams) error
	CopyTree(context.Context, string, string, string) error
	TreeNodes(context.Context, string) ([]db.TreeNode, error)
	TreePreviews(context.Context, string) ([]db.TreePreview, error)
	ListTreeMessages(context.Context, string) ([]db.Message, error)
	TreePath(context.Context, string) ([]string, error)
	SwitchTree(context.Context, string, string) error
	LabelTree(context.Context, string, string, string) error
	TreeRevision(context.Context, string) (int64, error)
}

func (s *service) Tree(ctx context.Context, id string) ([]TreeEntry, error) {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return nil, errors.New("tree storage unavailable")
	}
	previews, err := storage.TreePreviews(ctx, id)
	if err != nil {
		return nil, err
	}
	entries := make([]TreeEntry, 0, len(previews))
	for _, p := range previews {
		entries = append(entries, TreeEntry{p.TreeNode, Message{ID: p.MessageID, SessionID: id, Role: MessageRole(p.Role), Parts: []ContentPart{TextContent{Text: p.Text}}}})
	}

	return entries, nil
}
func (s *service) SwitchTree(ctx context.Context, id, target string) error {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return errors.New("tree storage unavailable")
	}
	if err := s.FlushAll(ctx); err != nil {
		return err
	}
	return storage.SwitchTree(ctx, id, target)
}
func (s *service) LabelTree(ctx context.Context, id, target, label string) error {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return errors.New("tree storage unavailable")
	}
	return storage.LabelTree(ctx, id, target, label)
}
func (s *service) TreeRevision(ctx context.Context, id string) (int64, error) {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return 0, nil
	}
	return storage.TreeRevision(ctx, id)
}
func (s *service) activeTree(ctx context.Context, id string, rows []db.Message) ([]db.Message, error) {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return rows, nil
	}
	ids, err := storage.TreePath(ctx, id)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]db.Message, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	active := make([]db.Message, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			active = append(active, row)
		}
	}
	return active, nil
}

func (s *service) SwitchTreeNote(ctx context.Context, id, target, text, model, provider string) error {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return errors.New("tree storage unavailable")
	}
	if err := s.FlushAll(ctx); err != nil {
		return err
	}
	m := Message{Parts: []ContentPart{TextContent{Text: text}}}
	m.AddFinish(FinishReasonEndTurn, "", "")
	parts, err := marshalParts(m.Parts)
	if err != nil {
		return err
	}
	return storage.SwitchTreeNote(ctx, id, target, &db.CreateMessageParams{ID: uuid.NewString(), SessionID: id, Role: string(Assistant), Parts: string(parts), Model: sql.NullString{String: model, Valid: model != ""}, Provider: sql.NullString{String: provider, Valid: provider != ""}})
}
func (s *service) CopyTree(ctx context.Context, id, target string, fork bool) (string, string, error) {
	storage, ok := s.q.(treeStorage)
	if !ok {
		return "", "", errors.New("tree storage unavailable")
	}
	if err := s.FlushAll(ctx); err != nil {
		return "", "", err
	}
	prompt := ""
	if fork {
		msg, err := s.Get(ctx, target)
		if err != nil {
			return "", "", err
		}
		if msg.SessionID != id || msg.Role != User {
			return "", "", fmt.Errorf("choose a user prompt to fork")
		}

		prompt = msg.Content().Text
		nodes, err := storage.TreeNodes(ctx, id)
		if err != nil {
			return "", "", err
		}
		for _, node := range nodes {
			if node.MessageID == target {
				target = node.ParentID
				break
			}
		}
	}
	newID := uuid.NewString()
	if err := storage.CopyTree(ctx, id, target, newID); err != nil {
		return "", "", err
	}
	return newID, prompt, nil
}
