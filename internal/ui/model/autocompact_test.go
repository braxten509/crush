package model

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

type autocompactWorkspace struct {
	workspace.Workspace
	scope   config.Scope
	key     string
	value   any
	updated bool
}

func (w *autocompactWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	w.scope, w.key, w.value = scope, key, value
	return nil
}

func (w *autocompactWorkspace) UpdateAgentModel(context.Context) error {
	w.updated = true
	return nil
}

func TestAutocompactSavesGlobalOptionOffUIThread(t *testing.T) {
	t.Parallel()
	ws := &autocompactWorkspace{}
	u := &UI{com: &common.Common{Workspace: ws}}
	cmd := u.saveAutocompactCmd(325000)
	require.Empty(t, ws.key, "creating the command must not do file I/O")
	cmd()
	require.Equal(t, config.ScopeGlobal, ws.scope)
	require.Equal(t, "options.auto_compact_token_limit", ws.key)
	require.Equal(t, int64(325000), ws.value)
	require.True(t, ws.updated, "the driver must receive the saved threshold")
}
