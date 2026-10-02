package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
)

// turnFileReview records only files named by editing tools. Starting a turn or
// running a read, search, or shell command must never inventory the workspace.
type turnFileReview struct {
	root      string
	exclude   []string
	cli       bool
	trackers  map[string]*filechange.Tracker
	results   map[string]string
	messages  message.Service
	sessionID string
}

func (a *sessionAgent) startFileReview(ctx context.Context, sessionID string) *turnFileReview {
	r := &turnFileReview{messages: a.messages, sessionID: sessionID,
		trackers: map[string]*filechange.Tracker{}, results: map[string]string{}}
	_, r.cli = a.largeModel.Get().Model.(*cliagent.Model)
	var root string
	exclude := []string{config.GlobalCacheDir(), filepath.Dir(config.GlobalConfigData())}
	if a.cfg != nil {
		root = a.cfg.WorkingDir()
		if options := a.cfg.Config().Options; options != nil {
			exclude = append(exclude, options.DataDirectory)
		}
	} else if model, ok := a.largeModel.Get().Model.(*cliagent.Model); ok {
		root = model.Dir
	}
	if root == "" {
		return r
	}
	if a.tasks != nil {
		exclude = append(exclude, a.tasks.dir)
	}
	r.root, r.exclude = root, exclude
	return r
}

func (r *turnFileReview) track(id, name, input string) {
	path := cliagent.EditedFile(name, input)
	if r.root == "" || path == "" {
		return
	}
	tracker, err := filechange.New(context.Background(), r.root, r.exclude...)
	if err != nil {
		slog.Warn("Cannot start file change review", "error", err)
		return
	}
	tracker.Track(path)
	// Once another tool edits the same file, later writes belong to that
	// new action, not to the earlier completed result.
	for previousID, previous := range r.trackers {
		if r.results[previousID] != "" && previous.Contains(path) {
			delete(r.trackers, previousID)
			delete(r.results, previousID)
		}
	}
	r.trackers[id] = tracker
}

func (r *turnFileReview) checkpoint(ctx context.Context, id string) *filechange.Review {
	tracker := r.trackers[id]
	if tracker == nil {
		return nil
	}
	review, err := tracker.Checkpoint(ctx)
	if err != nil {
		slog.Warn("Cannot capture file changes", "error", err)
	}
	return review
}

func (r *turnFileReview) save(ctx context.Context, result message.ToolResult) error {
	if metadata, review := filechange.TakeReview(result.Metadata); review != nil {
		result.Metadata, result.Review = metadata, review
	}
	review := r.checkpoint(ctx, result.ToolCallID)
	// Some CLIs report a file tool after it has executed. An empty snapshot
	// in that case must not hide the edit metadata the CLI supplied.
	if review != nil && (!r.cli || len(review.Changes) > 0) {
		result.Review = review
	}
	msg, err := r.messages.Create(ctx, r.sessionID, message.CreateMessageParams{
		Role: message.Tool, Parts: []message.ContentPart{result},
	})
	if err == nil && r.trackers[result.ToolCallID] != nil {
		r.results[result.ToolCallID] = msg.ID
	}
	return err
}

// finish catches writes that landed after a tool's result, including partial
// work on cancellation. Appending immutable snapshots preserves the earliest
// before image when the UI merges all changes in an action group.
func (r *turnFileReview) finish(ctx context.Context) {
	if len(r.trackers) == 0 {
		return
	}
	defer func() { r.trackers = nil }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for id, resultID := range r.results {
		r.finishTool(ctx, id, resultID)
	}
}

func (r *turnFileReview) finishTool(ctx context.Context, id, resultID string) {
	review := r.checkpoint(ctx, id)
	if review == nil || len(review.Changes) == 0 {
		return
	}
	msg, err := r.messages.Get(ctx, resultID)
	if err != nil {
		slog.Warn("Cannot load file review", "error", err)
		return
	}
	for i, part := range msg.Parts {
		if result, ok := part.(message.ToolResult); ok {
			merged := *review
			if result.Review != nil {
				merged.Changes = append(slices.Clone(result.Review.Changes), review.Changes...)
			}
			result.Review = &merged
			msg.Parts[i] = result
			break
		}
	}
	if err := r.messages.Update(ctx, msg); err != nil {
		slog.Warn("Cannot save final file changes", "error", err)
	}
}
