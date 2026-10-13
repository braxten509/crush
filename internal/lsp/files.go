package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/fsext"
	powernap "github.com/charmbracelet/x/powernap/pkg/lsp"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// OpenFileInfo contains information about an open file
type OpenFileInfo struct {
	Version int32
	URI     protocol.DocumentURI
}

// HandlesFile checks if this LSP client handles the given file based on its
// extension and whether it's within the working directory.
func (c *Client) HandlesFile(path string) bool {
	if c == nil {
		return false
	}
	if !fsext.HasPrefix(path, c.cwd) {
		slog.Debug("File outside workspace", "name", c.name, "file", path, "workDir", c.cwd)
		return false
	}
	return handlesFiletype(c.name, c.fileTypes, path)
}

// OpenFile opens a file in the LSP server.
func (c *Client) OpenFile(ctx context.Context, filepath string) error {
	if !c.HandlesFile(filepath) {
		return nil
	}

	uri := string(protocol.URIFromPath(filepath))

	if _, exists := c.openFiles.Get(uri); exists {
		return nil // Already open
	}

	// Skip files that do not exist or cannot be read
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	// Notify the server about the opened document
	if err = c.client.NotifyDidOpenTextDocument(ctx, uri, string(powernap.DetectLanguage(filepath)), 1, string(content)); err != nil {
		return err
	}

	c.openFiles.Set(uri, &OpenFileInfo{
		Version: 1,
		URI:     protocol.DocumentURI(uri),
	})

	return nil
}

// NotifyChange notifies the server about a file change.
func (c *Client) NotifyChange(ctx context.Context, filepath string) error {
	if c == nil {
		return nil
	}
	uri := string(protocol.URIFromPath(filepath))

	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	fileInfo, isOpen := c.openFiles.Get(uri)
	if !isOpen {
		return fmt.Errorf("cannot notify change for unopened file: %s", filepath)
	}

	// Increment version
	fileInfo.Version++

	// Create change event
	changes := []protocol.TextDocumentContentChangeEvent{
		{
			Value: protocol.TextDocumentContentChangeWholeDocument{
				Text: string(content),
			},
		},
	}

	return c.client.NotifyDidChangeTextDocument(ctx, uri, int(fileInfo.Version), changes)
}

// IsFileOpen checks if a file is currently open.
func (c *Client) IsFileOpen(filepath string) bool {
	uri := string(protocol.URIFromPath(filepath))
	_, exists := c.openFiles.Get(uri)
	return exists
}

// CloseAllFiles closes all currently open files.
func (c *Client) CloseAllFiles(ctx context.Context) {
	for uri := range c.openFiles.Seq2() {
		if c.debug {
			slog.Debug("Closing file", "file", uri)
		}
		if err := c.client.NotifyDidCloseTextDocument(ctx, uri); err != nil {
			slog.Warn("Error closing file", "uri", uri, "error", err)
			continue
		}
		c.openFiles.Del(uri)
	}
}

// OpenFileOnDemand opens a file only if it's not already open.
func (c *Client) OpenFileOnDemand(ctx context.Context, filepath string) error {
	if c == nil {
		return nil
	}
	// Check if the file is already open
	if c.IsFileOpen(filepath) {
		return nil
	}

	// Open the file
	return c.OpenFile(ctx, filepath)
}

// openKeyConfigFiles opens important configuration files that help initialize the server.
func (c *Client) openKeyConfigFiles(ctx context.Context) {
	// Try to open each file, ignoring errors if they don't exist
	for _, file := range c.config.RootMarkers {
		file = filepath.Join(c.cwd, file)
		if _, err := os.Stat(file); err == nil {
			// File exists, try to open it
			if err := c.OpenFile(ctx, file); err != nil {
				slog.Error("Failed to open key config file", "file", file, "error", err)
			} else {
				slog.Debug("Opened key config file for initialization", "file", file)
			}
		}
	}
}

// NotifyWorkspaceChange sends a workspace-level file change notification to
// trigger re-analysis of all files. This is useful when the overall project
// state may have changed (e.g., after a project-wide refactoring) and
// diagnostics for files not currently being edited may be stale.
func (c *Client) NotifyWorkspaceChange(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.client.NotifyDidChangeWatchedFiles(ctx, []protocol.FileEvent{
		{URI: protocol.DocumentURI(protocol.URIFromPath(c.cwd)), Type: protocol.Changed},
	})
}

// RefreshOpenFiles re-notifies the LSP server about all currently open files,
// which triggers re-analysis and fresh diagnostics for the entire project.
func (c *Client) RefreshOpenFiles(ctx context.Context) {
	if c == nil {
		return
	}
	for uri, info := range c.openFiles.Seq2() {
		path, err := protocol.DocumentURI(uri).Path()
		if err != nil {
			slog.Warn("Failed to convert URI to path", "uri", uri, "error", err)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("Failed to read file for refresh", "path", path, "error", err)
			continue
		}
		info.Version++
		changes := []protocol.TextDocumentContentChangeEvent{
			{
				Value: protocol.TextDocumentContentChangeWholeDocument{
					Text: string(content),
				},
			},
		}
		if err := c.client.NotifyDidChangeTextDocument(ctx, uri, int(info.Version), changes); err != nil {
			slog.Warn("Failed to notify file change", "uri", uri, "error", err)
		}
	}
}
