package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
)

// PersistBackgroundReview attaches a completed job's review to its saved Bash
// result even when the turn has ended and nobody calls job_output.
// Call only after the originating tool result has been persisted.
func PersistBackgroundReview(ctx context.Context, messages message.Service, sessionID string, result message.ToolResult) {
	if result.Name != BashToolName {
		return
	}
	var metadata BashResponseMetadata
	if json.Unmarshal([]byte(result.Metadata), &metadata) != nil || !metadata.Background || metadata.ShellID == "" {
		return
	}
	job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
	if !ok {
		return
	}
	go func() {
		job.Wait()
		review := job.Review()
		if review == nil {
			return
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := saveBackgroundReview(saveCtx, messages, sessionID, result.ToolCallID, job); err != nil {
			slog.Warn("Cannot save background file changes", "error", err)
		}
	}()
}

func saveBackgroundReview(ctx context.Context, messages message.Service, sessionID, callID string, job *shell.BackgroundShell) error {
	stored, err := messages.List(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, msg := range stored {
		for index, part := range msg.Parts {
			result, ok := part.(message.ToolResult)
			if !ok || result.ToolCallID != callID {
				continue
			}
			result.Review = job.Review()
			msg.Parts[index] = result
			if err := messages.Update(ctx, msg); err != nil {
				return err
			}
			return messages.Flush(ctx, msg.ID)
		}
	}
	return nil
}
