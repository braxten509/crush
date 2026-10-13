package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/permission"
)

// The file-changing tools (edit, multiedit, write, lsp_replace_symbol and
// lsp_rename) share one pipeline: check the session's last read is fresh,
// ask permission with a diff, write the file, record it in the file history
// and the file tracker, and tell the LSPs. The helpers below are those
// steps; each tool keeps its own wording and response metadata.

// staleStyle selects how a stale-read rejection is worded. The texts are
// part of the tools' output, so each tool keeps the one it always had.
type staleStyle int

const (
	// staleEdit requires a prior read and words the rejection tersely.
	staleEdit staleStyle = iota
	// staleWrite only rejects files changed since the last read.
	staleWrite
)

// checkFresh returns why filePath must be read again before it is changed,
// or "" when the session's last read is at least as new as the file.
func checkFresh(ctx context.Context, tracker filetracker.Service, sessionID, filePath string, info os.FileInfo, style staleStyle) string {
	lastRead := tracker.LastReadTime(ctx, sessionID, filePath)
	if style == staleEdit && lastRead.IsZero() {
		return "you must read the file before editing it. Use the View tool first"
	}
	modTime := info.ModTime().Truncate(time.Second)
	if !modTime.After(lastRead) {
		return ""
	}
	if style == staleWrite {
		return fmt.Sprintf("File %s has been modified since it was last read.\nLast modification: %s\nLast read: %s\n\nPlease read the file again before modifying it.",
			filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339))
	}
	return fmt.Sprintf(
		"file %s has been modified since it was last read (mod time: %s, last read: %s)",
		filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
	)
}

// editRequest describes a pending change to one file for requestEdit.
type editRequest struct {
	sessionID   string
	filePath    string
	callID      string
	toolName    string
	description string
	oldContent  string
	newContent  string
	// params is what the permission dialog renders.
	params any
}

// editDiff is the diff requestEdit computed for the permission dialog.
type editDiff struct {
	text      string
	additions int
	removals  int
}

// requestEdit computes the diff of a change and asks permission to write
// it. The diff is returned whether or not permission was granted, so a
// denied response can still carry it.
func requestEdit(edit editContext, req editRequest) (bool, editDiff, error) {
	var d editDiff
	d.text, d.additions, d.removals = diff.GenerateDiff(
		req.oldContent,
		req.newContent,
		strings.TrimPrefix(req.filePath, edit.workingDir),
	)
	granted, err := edit.permissions.Request(
		edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   req.sessionID,
			Path:        fsext.PathOrPrefix(req.filePath, edit.workingDir),
			ToolCallID:  req.callID,
			ToolName:    req.toolName,
			Action:      "write",
			Description: req.description,
			Params:      req.params,
		},
	)
	return granted, d, err
}

// deniedResponse is the permission-denied response carrying metadata.
func deniedResponse(metadata any) fantasy.ToolResponse {
	return fantasy.WithResponseMetadata(NewPermissionDeniedResponse(), metadata)
}

// commitFileChange writes newContent to filePath, updates the file history,
// and records the read in the file tracker. Callers must convert line endings
// before calling this function.
func commitFileChange(edit editContext, sessionID, filePath, oldContent, newContent string) error {
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return recordFileChange(edit, sessionID, filePath, oldContent, newContent)
}

// recordFileChange records a change already written to filePath: the file
// history gets the old version on first touch (or when the file changed
// outside the session since its last version) and the new one, and the
// file tracker counts the change as a read. History is kept only for files
// inside the working directory's repository.
func recordFileChange(edit editContext, sessionID, filePath, oldContent, newContent string) error {
	if edit.files != nil && filechange.InRepository(filechange.RepositoryRoot(edit.workingDir), edit.workingDir, filePath) {
		file, err := edit.files.GetByPathAndSession(edit.ctx, filePath, sessionID)
		if err != nil {
			file, err = edit.files.Create(edit.ctx, sessionID, filePath, oldContent)
			if err != nil {
				return fmt.Errorf("error creating file history: %w", err)
			}
		}
		if file.Content != oldContent {
			// User manually changed the content; store an intermediate version.
			if _, err := edit.files.CreateVersion(edit.ctx, sessionID, filePath, oldContent); err != nil {
				slog.Error("Error creating file history version", "error", err)
			}
		}
		if _, err := edit.files.CreateVersion(edit.ctx, sessionID, filePath, newContent); err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	if edit.filetracker != nil {
		edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)
	}
	return nil
}

// recordNewFile records a file the session just created: an empty first
// history version and its content, and a read in the file tracker.
func recordNewFile(edit editContext, sessionID, filePath, content string) error {
	if filechange.InRepository(filechange.RepositoryRoot(edit.workingDir), edit.workingDir, filePath) {
		// File can't be in the history so we create a new file history.
		if _, err := edit.files.Create(edit.ctx, sessionID, filePath, ""); err != nil {
			return fmt.Errorf("error creating file history: %w", err)
		}
		if _, err := edit.files.CreateVersion(edit.ctx, sessionID, filePath, content); err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)
	return nil
}

// afterWrite tells the LSPs filePath changed and returns result followed by
// the diagnostics they report.
func afterWrite(ctx context.Context, lspManager *lsp.Manager, filePath, result string) string {
	notifyLSPs(ctx, lspManager, filePath)
	return result + getDiagnostics(filePath, lspManager)
}
