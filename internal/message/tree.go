package message

import (
	"context"
	"errors"
	"github.com/charmbracelet/crush/internal/db"
)

// TreeService is optional so existing upstream stores and mocks remain valid.
type TreeService interface {
	Tree(context.Context, string) ([]TreeEntry, error)
	SwitchTree(context.Context, string, string) error
	LabelTree(context.Context, string, string, string) error
	TreeRevision(context.Context, string) (int64, error)
}
type TreeEntry struct {
	db.TreeNode
	Message Message
}
type treeStorage interface {
	TreeNodes(context.Context, string) ([]db.TreeNode, error)
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
	nodes, err := storage.TreeNodes(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMessagesBySession(ctx, id)
	if err != nil {
		return nil, err
	}
	messages := make(map[string]Message, len(rows))
	for _, row := range rows {
		msg, err := s.fromDBItem(row)
		if err != nil {
			return nil, err
		}
		messages[msg.ID] = msg
	}
	entries := make([]TreeEntry, 0, len(nodes))
	for _, node := range nodes {
		entries = append(entries, TreeEntry{node, messages[node.MessageID]})
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
