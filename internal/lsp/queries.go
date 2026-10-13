package lsp

import (
	"context"
	"time"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// FindReferences finds all references to the symbol at the given position.
func (c *Client) FindReferences(ctx context.Context, filepath string, line, character int, includeDeclaration bool) ([]protocol.Location, error) {
	if err := c.OpenFileOnDemand(ctx, filepath); err != nil {
		return nil, err
	}

	// Add timeout to prevent hanging on slow LSP servers.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// NOTE: line and character should be 0-based.
	// See: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#position
	return c.client.FindReferences(ctx, filepath, line-1, character-1, includeDeclaration)
}

// Rename renames the symbol at the given position across all files.
func (c *Client) Rename(ctx context.Context, filepath string, line, character int, newName string) (*protocol.WorkspaceEdit, error) {
	if err := c.OpenFileOnDemand(ctx, filepath); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.client.RequestRename(ctx, filepath, line-1, character-1, newName) //nolint:wrapcheck
}

// DocumentSymbols returns the document symbols for the given file.
func (c *Client) DocumentSymbols(ctx context.Context, filepath string) ([]protocol.DocumentSymbolResult, error) {
	if err := c.OpenFileOnDemand(ctx, filepath); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return c.client.RequestDocumentSymbols(ctx, filepath) //nolint:wrapcheck
}

// Definition finds the definition of the symbol at the given position.
func (c *Client) Definition(ctx context.Context, filepath string, line, character int) ([]protocol.Location, error) {
	if err := c.OpenFileOnDemand(ctx, filepath); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return c.client.RequestDefinition(ctx, filepath, line-1, character-1) //nolint:wrapcheck
}

// PrepareCallHierarchy prepares a call hierarchy item at the given position.
func (c *Client) PrepareCallHierarchy(ctx context.Context, filepath string, line, character int) ([]protocol.CallHierarchyItem, error) {
	if err := c.OpenFileOnDemand(ctx, filepath); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return c.client.PrepareCallHierarchy(ctx, filepath, line-1, character-1) //nolint:wrapcheck
}

// IncomingCalls returns all callers of the given call hierarchy item.
func (c *Client) IncomingCalls(ctx context.Context, item protocol.CallHierarchyItem) ([]protocol.CallHierarchyIncomingCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.client.IncomingCalls(ctx, item) //nolint:wrapcheck
}

// OutgoingCalls returns all callees of the given call hierarchy item.
func (c *Client) OutgoingCalls(ctx context.Context, item protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.client.OutgoingCalls(ctx, item) //nolint:wrapcheck
}
