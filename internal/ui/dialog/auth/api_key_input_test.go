package auth

import (
	"errors"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// keyWorkspace records the API keys saved through it.
type keyWorkspace struct {
	workspace.Workspace

	mu    sync.Mutex
	saved []string
	err   error
}

func (w *keyWorkspace) SetProviderAPIKey(_ config.Scope, providerID string, apiKey any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.saved = append(w.saved, providerID+"="+apiKey.(string))
	return w.err
}

func (w *keyWorkspace) savedKeys() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.saved...)
}

func newVerifiedKeyInput(t *testing.T, ws *keyWorkspace) *APIKeyInput {
	t.Helper()
	sty := styles.CharmtonePantera()
	m, _ := NewAPIKeyInput(&common.Common{Workspace: ws, Styles: &sty}, false,
		catwalk.Provider{ID: "p"}, config.SelectedModel{Provider: "p", Model: "m"}, config.SelectedModelTypeChat)
	m.input.SetValue("sk-test")
	m.state = APIKeyInputStateVerified
	return m
}

// TestAPIKeyInputSavesOffLoop pins that submitting a verified key saves it
// when the returned command runs, selects the model once the save landed,
// and ignores further submits meanwhile.
func TestAPIKeyInputSavesOffLoop(t *testing.T) {
	t.Parallel()

	ws := &keyWorkspace{}
	m := newVerifiedKeyInput(t, ws)
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Empty(t, ws.savedKeys(), "the key must not be saved on the Update goroutine")
	require.Nil(t, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "a second submit waits for the save")

	cmd, ok := action.(dialog.ActionCmd)
	require.True(t, ok)
	msg := cmd.Cmd()
	require.Equal(t, []string{"p=sk-test"}, ws.savedKeys())

	selected, ok := m.HandleMsg(msg).(dialog.ActionSelectModel)
	require.True(t, ok)
	require.Equal(t, config.SelectedModel{Provider: "p", Model: "m"}, selected.Model)
	require.Equal(t, config.SelectedModelTypeChat, selected.ModelType)
}

func TestAPIKeyInputReportsFailedSave(t *testing.T) {
	t.Parallel()

	ws := &keyWorkspace{err: errors.New("disk full")}
	m := newVerifiedKeyInput(t, ws)
	cmd, ok := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(dialog.ActionCmd)
	require.True(t, ok)
	report, ok := m.HandleMsg(cmd.Cmd()).(dialog.ActionCmd)
	require.True(t, ok)
	info, ok := report.Cmd().(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, util.InfoTypeError, info.Type)
	require.Contains(t, info.Msg, "disk full")

	// The dialog stays usable: the next submit saves again.
	_, ok = m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(dialog.ActionCmd)
	require.True(t, ok)
}
