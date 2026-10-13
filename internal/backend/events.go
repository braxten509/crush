package backend

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// SubscribeEvents returns a per-caller event channel for a workspace.
// Each caller receives all events; multiple callers do not compete.
func (b *Backend) SubscribeEvents(ctx context.Context, workspaceID string) (<-chan pubsub.Event[tea.Msg], error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Events(ctx), nil
}

// GetLSPStates returns the state of all LSP clients.
func (b *Backend) GetLSPStates(workspaceID string) (map[string]app.LSPClientInfo, error) {
	_, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return app.GetLSPStates(), nil
}

// GetLSPDiagnostics returns diagnostics for a specific LSP client in
// the workspace.
func (b *Backend) GetLSPDiagnostics(workspaceID, lspName string) (map[protocol.DocumentURI][]protocol.Diagnostic, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	for name, client := range ws.LSPManager.Clients().Seq2() {
		if name == lspName {
			return client.GetDiagnostics(), nil
		}
	}

	return nil, ErrLSPClientNotFound
}

// GetWorkspaceConfig returns the workspace-level configuration.
func (b *Backend) GetWorkspaceConfig(workspaceID string) (*config.Config, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	return ws.Cfg.Config(), nil
}

// GetWorkspaceProviders returns the configured providers for a
// workspace.
func (b *Backend) GetWorkspaceProviders(workspaceID string) ([]catwalk.Provider, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	providers, _ := config.Providers(ws.Cfg.Config())
	return providers, nil
}

// LSPStart starts an LSP server for the given path.
func (b *Backend) LSPStart(ctx context.Context, workspaceID, path string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.LSPStart(ctx, path)
	return nil
}

// LSPStopAll stops all LSP servers for a workspace.
func (b *Backend) LSPStopAll(ctx context.Context, workspaceID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.LSPStopAll(ctx)
	return nil
}
