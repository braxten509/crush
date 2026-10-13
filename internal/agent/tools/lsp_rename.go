package tools

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	lsputil "github.com/charmbracelet/crush/internal/lsp/util"
	"github.com/charmbracelet/crush/internal/permission"
)

type RenameParams struct {
	Symbol  string `json:"symbol" description:"The symbol name to rename"`
	NewName string `json:"new_name" description:"The new name for the symbol"`
	Path    string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
}

const RenameToolName = "lsp_rename"

//go:embed lsp_rename.md
var renameDescription string

func NewRenameTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		RenameToolName,
		renameDescription,
		func(ctx context.Context, params RenameParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Symbol == "" {
				return fantasy.NewTextErrorResponse("symbol is required"), nil
			}
			if params.NewName == "" {
				return fantasy.NewTextErrorResponse("new_name is required"), nil
			}
			searchDir := cmp.Or(params.Path, ".")
			resolved, err := resolveSymbol(ctx, lspManager, params.Symbol, searchDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Symbol '%s' not found", params.Symbol)), nil
			}

			edit, err := resolved.client.Rename(ctx, resolved.path, resolved.line, resolved.char, params.NewName)
			if err != nil {
				slog.Error("Failed to rename symbol", "error", err, "symbol", params.Symbol)
				return fantasy.NewTextErrorResponse(fmt.Sprintf("rename failed: %s", err)), nil
			}
			if edit == nil {
				return fantasy.NewTextResponse(fmt.Sprintf("No rename edits generated for symbol '%s'", params.Symbol)), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID != "" && permissions != nil {
				granted, err := permissions.Request(ctx, permission.CreatePermissionRequest{
					SessionID:   sessionID,
					ToolName:    RenameToolName,
					Description: fmt.Sprintf("Rename '%s' to '%s'", params.Symbol, params.NewName),
				})
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("permission request failed: %w", err)
				}
				if !granted {
					return NewPermissionDeniedResponse(), nil
				}
			}

			affectedFiles := collectAffectedFiles(edit)

			// Keep the files' contents from before the rename, so the file
			// history can record both versions of each.
			before := make(map[string]string, len(affectedFiles))
			if files != nil && sessionID != "" {
				for _, path := range affectedFiles {
					content, err := os.ReadFile(path)
					if err != nil {
						slog.Warn("Failed to read file for version tracking", "path", path, "error", err)
						continue
					}
					before[path] = string(content)
				}
			}

			encoding := resolved.client.GetOffsetEncoding()
			if err := lsputil.ApplyWorkspaceEdit(*edit, encoding); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to apply rename edits: %s", err)), nil
			}

			if sessionID != "" {
				record := editContext{ctx: ctx, files: files, filetracker: filetracker, workingDir: workingDir}
				for _, path := range affectedFiles {
					oldContent, ok := before[path]
					var newContent []byte
					var err error
					if ok {
						newContent, err = os.ReadFile(path)
						if err != nil {
							slog.Warn("Failed to read renamed file for version tracking", "path", path, "error", err)
						}
					}
					if !ok || err != nil {
						if filetracker != nil {
							filetracker.RecordRead(ctx, sessionID, path)
						}
						continue
					}
					if err := recordFileChange(record, sessionID, path, oldContent, string(newContent)); err != nil {
						slog.Warn("Failed to create file version", "path", path, "error", err)
					}
				}
			}

			notifyLSPs(ctx, lspManager, "")

			var b strings.Builder
			fmt.Fprintf(&b, "Renamed '%s' to '%s' in %d file(s):\n\n", params.Symbol, params.NewName, len(affectedFiles))
			for _, f := range affectedFiles {
				fmt.Fprintf(&b, "  %s\n", f)
			}

			text := b.String()
			if len(affectedFiles) > 0 {
				text += "\n" + getDiagnostics(affectedFiles[0], lspManager)
			}

			return fantasy.NewTextResponse(text), nil
		},
	)
}
