package tools

import (
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
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

type ReplaceSymbolParams struct {
	Symbol      string `json:"symbol" description:"The symbol name to target (e.g., function name, method name, type name)"`
	FilePath    string `json:"file_path" description:"The path to the file containing the symbol"`
	Replacement string `json:"replacement,omitempty" description:"The replacement text. Required for 'replace' action. For 'add_before'/'add_after', the text to insert. Ignored for 'delete'."`
	Action      string `json:"action,omitempty" description:"Operation to perform: 'replace' (default, replace entire symbol), 'add_before' (insert before symbol), 'add_after' (insert after symbol), 'delete' (remove symbol entirely)"`
}

const ReplaceSymbolToolName = "lsp_replace_symbol"

//go:embed lsp_replace_symbol.md
var replaceSymbolDescription string

// ReplaceSymbolResponseMetadata carries diff data for the renderer.
type ReplaceSymbolResponseMetadata struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
	Action     string `json:"action"`
}

// ReplaceSymbolPermissionsParams carries diff data for the permission dialog.
type ReplaceSymbolPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
}

func NewReplaceSymbolTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) fantasy.AgentTool {
	t := &replaceSymbolTool{
		lspManager:  lspManager,
		permissions: permissions,
		files:       files,
		filetracker: filetracker,
		workingDir:  workingDir,
	}
	return fantasy.NewAgentTool(
		ReplaceSymbolToolName,
		replaceSymbolDescription,
		t.run,
	)
}

// replaceSymbolTool holds what the replace_symbol tool needs to edit a
// symbol.
type replaceSymbolTool struct {
	lspManager  *lsp.Manager
	permissions permission.Service
	files       history.Service
	filetracker filetracker.Service
	workingDir  string
}

func (t *replaceSymbolTool) run(ctx context.Context, params ReplaceSymbolParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	action, errMsg := replaceSymbolAction(params)
	if errMsg != "" {
		return fantasy.NewTextErrorResponse(errMsg), nil
	}

	t.lspManager.Start(ctx, params.FilePath)

	client := findLSPClient(t.lspManager, params.FilePath)
	if client == nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("no LSP client handles file: %s", params.FilePath)), nil
	}

	symbols, err := client.DocumentSymbols(ctx, params.FilePath)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to get document symbols: %s", err)), nil
	}

	target := findSymbolByName(symbols, params.Symbol)
	if target == nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("symbol '%s' not found in %s", params.Symbol, params.FilePath)), nil
	}

	rng := target.GetRange()

	content, err := os.ReadFile(params.FilePath)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	startLine := int(rng.Start.Line)
	endLine := int(rng.End.Line)
	if startLine >= len(lines) || endLine >= len(lines) {
		return fantasy.NewTextErrorResponse("symbol range exceeds file length"), nil
	}

	// Compute new content before permission so the dialog can show a diff.
	newLines := applySymbolAction(lines, action, params.Replacement, startLine, endLine)
	newContent := strings.Join(newLines, "\n")

	sessionID := GetSessionFromContext(ctx)
	if sessionID != "" && t.permissions != nil {
		granted, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        params.FilePath,
			ToolName:    ReplaceSymbolToolName,
			Description: fmt.Sprintf("%s symbol '%s' in %s", action, params.Symbol, params.FilePath),
			Params: ReplaceSymbolPermissionsParams{
				FilePath:   params.FilePath,
				OldContent: string(content),
				NewContent: newContent,
			},
		})
		if err != nil {
			return fantasy.ToolResponse{}, fmt.Errorf("permission request failed: %w", err)
		}
		if !granted {
			return NewPermissionDeniedResponse(), nil
		}
	}

	if err := os.WriteFile(params.FilePath, []byte(newContent), 0o644); err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	if sessionID != "" {
		record := editContext{ctx: ctx, files: t.files, filetracker: t.filetracker, workingDir: t.workingDir}
		if err := recordFileChange(record, sessionID, params.FilePath, string(content), newContent); err != nil {
			slog.Warn("Failed to create file version after replace", "path", params.FilePath, "error", err)
		}
	}

	notifyLSPs(ctx, t.lspManager, params.FilePath)

	summary := replaceSymbolSummary(action, params.Symbol, params.FilePath, startLine, endLine)
	resp := fantasy.NewTextResponse(summary + "\n" + getDiagnostics(params.FilePath, t.lspManager))
	resp = fantasy.WithResponseMetadata(resp, ReplaceSymbolResponseMetadata{
		FilePath:   params.FilePath,
		OldContent: string(content),
		NewContent: newContent,
		Action:     action,
	})
	return resp, nil
}

// replaceSymbolAction validates the parameters and returns the action to
// take. A non-empty error message means the parameters are invalid.
func replaceSymbolAction(params ReplaceSymbolParams) (string, string) {
	if params.Symbol == "" {
		return "", "symbol is required"
	}
	if params.FilePath == "" {
		return "", "file_path is required"
	}

	action := params.Action
	if action == "" {
		action = "replace"
	}
	switch action {
	case "replace", "add_before", "add_after", "delete":
	default:
		return "", fmt.Sprintf("invalid action %q: must be replace, add_before, add_after, or delete", action)
	}
	if (action == "replace" || action == "add_before" || action == "add_after") && params.Replacement == "" {
		return "", fmt.Sprintf("replacement is required for action %q", action)
	}
	return action, ""
}

// applySymbolAction returns the file's lines after applying action to the
// symbol spanning startLine to endLine.
func applySymbolAction(lines []string, action, replacement string, startLine, endLine int) []string {
	var newLines []string
	switch action {
	case "replace":
		newLines = make([]string, 0, len(lines))
		newLines = append(newLines, lines[:startLine]...)
		newLines = append(newLines, strings.Split(replacement, "\n")...)
		newLines = append(newLines, lines[endLine+1:]...)
	case "add_before":
		newLines = make([]string, 0, len(lines)+strings.Count(replacement, "\n")+1)
		newLines = append(newLines, lines[:startLine]...)
		newLines = append(newLines, strings.Split(replacement, "\n")...)
		newLines = append(newLines, lines[startLine:]...)
	case "add_after":
		newLines = make([]string, 0, len(lines)+strings.Count(replacement, "\n")+1)
		newLines = append(newLines, lines[:endLine+1]...)
		newLines = append(newLines, strings.Split(replacement, "\n")...)
		newLines = append(newLines, lines[endLine+1:]...)
	case "delete":
		newLines = make([]string, 0, len(lines))
		newLines = append(newLines, lines[:startLine]...)
		newLines = append(newLines, lines[endLine+1:]...)
	}
	return newLines
}

// replaceSymbolSummary describes the edit for the model.
func replaceSymbolSummary(action, symbol, filePath string, startLine, endLine int) string {
	var summary string
	switch action {
	case "replace":
		summary = fmt.Sprintf("Replaced symbol '%s' in %s (lines %d-%d)", symbol, filePath, startLine+1, endLine+1)
	case "add_before":
		summary = fmt.Sprintf("Inserted before symbol '%s' in %s (before line %d)", symbol, filePath, startLine+1)
	case "add_after":
		summary = fmt.Sprintf("Inserted after symbol '%s' in %s (after line %d)", symbol, filePath, endLine+1)
	case "delete":
		summary = fmt.Sprintf("Deleted symbol '%s' from %s (lines %d-%d)", symbol, filePath, startLine+1, endLine+1)
	}
	return summary
}

// findSymbolByName searches for a symbol by name in the document symbol tree.
func findSymbolByName(symbols []protocol.DocumentSymbolResult, name string) protocol.DocumentSymbolResult {
	for _, sym := range symbols {
		if sym.GetName() == name {
			return sym
		}
		if ds, ok := sym.(*protocol.DocumentSymbol); ok && len(ds.Children) > 0 {
			children := make([]protocol.DocumentSymbolResult, len(ds.Children))
			for i := range ds.Children {
				children[i] = &ds.Children[i]
			}
			if found := findSymbolByName(children, name); found != nil {
				return found
			}
		}
	}
	return nil
}
