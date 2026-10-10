package model

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

type modelSelectionWorkspace struct {
	*sessionSpecificWorkspace
	cfg *config.Config
}

func (w *modelSelectionWorkspace) Config() *config.Config { return w.cfg }

func (w *modelSelectionWorkspace) UpdatePreferredModel(_ config.Scope, kind config.SelectedModelType, selected config.SelectedModel) error {
	w.cfg.Models[kind] = selected
	return nil
}

func TestModelSettingsOnlyWaitForTheCurrentSession(t *testing.T) {
	for _, test := range []struct {
		name    string
		session *session.Session
		busy    bool
	}{
		{name: "new session"},
		{name: "empty session", session: &session.Session{}},
		{name: "idle session", session: &session.Session{ID: "idle"}},
		{name: "working session", session: &session.Session{ID: "other-active"}, busy: true},
	} {
		for _, setting := range []string{"model", "effort"} {
			t.Run(test.name+"/"+setting, func(t *testing.T) {
				initial := config.SelectedModel{Provider: "test-provider", Model: "first", ReasoningEffort: "max"}
				providers := csync.NewMap[string, config.ProviderConfig]()
				providers.Set(initial.Provider, config.ProviderConfig{
					ID: initial.Provider,
					Models: []catwalk.Model{
						{ID: "first", CanReason: true, ReasoningLevels: []string{"high", "max"}},
						{ID: "second"},
					},
				})
				base := &countingWorkspace{ready: true}
				ws := &modelSelectionWorkspace{
					sessionSpecificWorkspace: &sessionSpecificWorkspace{base},
					cfg: &config.Config{
						Providers: providers,
						Agents: map[string]config.Agent{
							config.AgentCoder: {Model: config.SelectedModelTypeLarge},
						},
						Models: map[config.SelectedModelType]config.SelectedModel{
							config.SelectedModelTypeLarge: initial,
							config.SelectedModelTypeSmall: initial,
						},
					},
				}
				m := newBusyUI(base)
				m.com.Workspace = ws
				m.session = test.session
				m.userThemeSelected = true
				m.applyBusyState(m.dispatchBusyRefresh()().(busyStateMsg))

				want := initial
				switch setting {
				case "model":
					want.Model = "second"
					m.handleDialogAction(dialog.ActionSelectModel{ModelType: config.SelectedModelTypeLarge, Model: want})
				case "effort":
					want.ReasoningEffort = "high"
					picker, err := dialog.NewReasoning(m.com)
					require.NoError(t, err)
					m.dialog.OpenDialog(picker)
					m.handleDialogAction(dialog.ActionSelectReasoningEffort{Effort: want.ReasoningEffort})
					require.Equal(t, test.busy, m.dialog.ContainsDialog(dialog.ReasoningID))
				}
				if test.busy {
					want = initial
				}
				require.Equal(t, want, ws.cfg.Models[config.SelectedModelTypeLarge])
			})
		}
	}
}

func TestNewSessionClearsBusyStateBeforeRefresh(t *testing.T) {
	m := newBusyUI(&countingWorkspace{ready: true, agentBusy: true})
	warmCaches(m, true)
	oldRefresh := m.dispatchBusyRefresh()

	m.newSession()
	require.Nil(t, m.session)
	require.False(t, m.isAgentBusy(), "the previous session must not block the new session's settings")

	// A reply from the old session's check must not make the new one busy.
	refresh := m.applyBusyState(oldRefresh().(busyStateMsg))
	require.Len(t, refresh, 1)
	m.applyBusyState(refresh[0]().(busyStateMsg))
	require.False(t, m.isAgentBusy())
}
