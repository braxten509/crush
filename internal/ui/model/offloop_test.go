package model

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/stretchr/testify/require"
)

// prefWorkspace records the preference, Copilot import and agent calls a
// model selection makes, in order.
type prefWorkspace struct {
	countingWorkspace
	cfg *config.Config

	mu      sync.Mutex
	calls   []string
	prefErr error
	// copilot is added to the providers by ImportCopilot when set.
	copilot *config.ProviderConfig
}

func (w *prefWorkspace) Config() *config.Config { return w.cfg }

func (w *prefWorkspace) record(call string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, call)
}

func (w *prefWorkspace) recorded() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

func (w *prefWorkspace) UpdatePreferredModel(_ config.Scope, kind config.SelectedModelType, model config.SelectedModel) error {
	w.record("pref " + string(kind) + " " + model.Provider + "/" + model.Model)
	if w.prefErr != nil {
		return w.prefErr
	}
	w.cfg.Models[kind] = model
	return nil
}

func (w *prefWorkspace) GetDefaultSmallModel(providerID string) config.SelectedModel {
	w.record("default small " + providerID)
	return config.SelectedModel{Provider: providerID, Model: "small"}
}

func (w *prefWorkspace) ImportCopilot() (*oauth.Token, bool) {
	w.record("import copilot")
	if w.copilot != nil {
		w.cfg.Providers.Set(w.copilot.ID, *w.copilot)
		return nil, true
	}
	return nil, false
}

func (w *prefWorkspace) InitCoderAgent(context.Context) error {
	w.record("init coder")
	return nil
}

func (w *prefWorkspace) UpdateAgentModel(context.Context) error {
	w.record("update agent model")
	return nil
}

func newPrefUI(t *testing.T) (*UI, *prefWorkspace) {
	t.Helper()
	providers := csync.NewMap[string, config.ProviderConfig]()
	providers.Set("p", config.ProviderConfig{ID: "p", Models: []catwalk.Model{{ID: "m1", Name: "Model One"}}})
	ws := &prefWorkspace{
		countingWorkspace: countingWorkspace{ready: true},
		cfg: &config.Config{
			Options:   &config.Options{},
			Providers: providers,
			Agents:    map[string]config.Agent{config.AgentCoder: {Model: config.SelectedModelTypeLarge}},
			Models:    map[config.SelectedModelType]config.SelectedModel{},
		},
	}
	u := newBusyUI(&ws.countingWorkspace)
	u.com.Workspace = ws
	// Keep the theme out of it: the user's choice always wins.
	u.theme.userSelected = true
	return u, ws
}

// runAll executes a command tree, sequences included, feeding workDoneMsg
// results back into Update and executing the commands those return.
func runAll(m *UI, cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runAll(m, c)...)
		}
		return out
	case workDoneMsg:
		_, next := m.Update(msg)
		return append([]tea.Msg{msg}, runAll(m, next)...)
	}
	// tea.Sequence's message type is unexported; it is a slice of
	// commands.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, runAll(m, v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestSelectModelWritesPreferenceOffLoop(t *testing.T) {
	t.Parallel()

	u, ws := newPrefUI(t)
	u.dialog.OpenDialog(idDialog{id: dialog.ModelsID})
	selected := config.SelectedModel{Provider: "p", Model: "m1"}
	cmd := u.handleDialogAction(dialog.ActionSelectModel{ModelType: config.SelectedModelTypeLarge, Model: selected})
	require.Empty(t, ws.recorded(), "the writes must not run on the Update goroutine")
	require.False(t, u.dialog.ContainsDialog(dialog.ModelsID), "the dialog closes at once")

	msgs := runAll(u, cmd)
	require.Equal(t, []string{
		"pref large p/m1",
		"default small p",
		"pref small p/small",
		"update agent model",
	}, ws.recorded())
	info, ok := findMsg[util.InfoMsg](msgs)
	require.True(t, ok)
	require.Equal(t, "Large model changed to Model One", info.Msg)
}

func TestSelectModelReportsFailedWrite(t *testing.T) {
	t.Parallel()

	u, ws := newPrefUI(t)
	ws.prefErr = os.ErrPermission
	cmd := u.handleDialogAction(dialog.ActionSelectModel{ModelType: config.SelectedModelTypeLarge, Model: config.SelectedModel{Provider: "p", Model: "m1"}})
	msgs := runAll(u, cmd)
	// As before, the coordinator still rebuilds after a failed write.
	require.Equal(t, []string{"pref large p/m1", "update agent model"}, ws.recorded())
	var types []util.InfoType
	for _, msg := range msgs {
		if info, ok := msg.(util.InfoMsg); ok {
			types = append(types, info.Type)
		}
	}
	require.Contains(t, types, util.InfoTypeError)
}

// TestSelectModelOnboardingStartsCoderAfterWrite pins that onboarding ends
// at once, while the agents are set up and the coder started only once the
// selected model was written, and its model rebuilt after that.
func TestSelectModelOnboardingStartsCoderAfterWrite(t *testing.T) {
	t.Parallel()

	u, ws := newPrefUI(t)
	u.state = uiOnboarding
	ws.cfg.Models[config.SelectedModelTypeSmall] = config.SelectedModel{Provider: "p", Model: "m1"}
	ws.cfg.Agents = nil
	cmd := u.handleDialogAction(dialog.ActionSelectModel{ModelType: config.SelectedModelTypeLarge, Model: config.SelectedModel{Provider: "p", Model: "m1"}})
	require.Equal(t, uiLanding, u.state)
	require.Empty(t, ws.recorded())
	require.Nil(t, ws.cfg.Agents, "the agents are set up once the model was written")

	runAll(u, cmd)
	require.Equal(t, []string{"pref large p/m1", "init coder", "update agent model"}, ws.recorded())
	require.Contains(t, ws.cfg.Agents, config.AgentCoder)
}

// TestSelectModelImportsCopilotOffLoop pins that the Copilot token import
// runs in a command and the selection resumes, once, when it finished.
func TestSelectModelImportsCopilotOffLoop(t *testing.T) {
	t.Parallel()

	for _, imported := range []bool{true, false} {
		u, ws := newPrefUI(t)
		if imported {
			ws.copilot = &config.ProviderConfig{ID: "copilot", Models: []catwalk.Model{{ID: "c1"}}}
		}
		u.dialog.OpenDialog(idDialog{id: dialog.ModelsID})
		action := dialog.ActionSelectModel{
			Provider:  catwalk.Provider{ID: catwalk.InferenceProviderCopilot},
			ModelType: config.SelectedModelTypeLarge,
			Model:     config.SelectedModel{Provider: "copilot", Model: "c1"},
		}
		msgs := collectMsgs(u.handleDialogAction(action))
		require.Equal(t, []string{"import copilot"}, ws.recorded())
		require.True(t, u.dialog.ContainsDialog(dialog.ModelsID), "the selection waits for the import")

		done, ok := findMsg[copilotImportDoneMsg](msgs)
		require.True(t, ok)
		_, cmd := u.Update(done)
		require.False(t, u.dialog.ContainsDialog(dialog.ModelsID))
		if imported {
			runAll(u, cmd)
			require.Equal(t, []string{"import copilot", "pref large copilot/c1", "default small copilot", "pref small copilot/small", "update agent model"}, ws.recorded())
		} else {
			// Still not configured: the sign-in opens, without importing again.
			require.Equal(t, []string{"import copilot"}, ws.recorded())
			require.True(t, u.dialog.HasDialogs())
		}
	}
}

func TestRestoreModelFromSessionWritesOffLoop(t *testing.T) {
	t.Parallel()

	u, ws := newPrefUI(t)
	ws.cfg.Models[config.SelectedModelTypeLarge] = config.SelectedModel{Provider: "p", Model: "other"}
	ws.cfg.Models[config.SelectedModelTypeSmall] = config.SelectedModel{Provider: "p", Model: "other"}
	ws.cfg.Providers.Set("p", config.ProviderConfig{ID: "p", Models: []catwalk.Model{{ID: "m1"}, {ID: "other"}}})
	cmd := u.restoreModelFromSession([]message.Message{{Role: message.Assistant, Provider: "p", Model: "m1"}})
	require.NotNil(t, cmd)
	require.Empty(t, ws.recorded())

	runAll(u, cmd)
	require.Equal(t, []string{"pref large p/m1", "update agent model"}, ws.recorded())
	require.Equal(t, "m1", ws.cfg.Models[config.SelectedModelTypeLarge].Model)
}

func TestToggleThinkingCapturesModel(t *testing.T) {
	t.Parallel()

	u, ws := newPrefUI(t)
	ws.cfg.Models[config.SelectedModelTypeLarge] = config.SelectedModel{Provider: "p", Model: "m1"}
	ws.cfg.Providers.Set("p", config.ProviderConfig{ID: "p", Type: catwalk.TypeAnthropic, Models: []catwalk.Model{{ID: "m1", CanReason: true}}})
	cmd := u.handleDialogAction(dialog.ActionToggleThinking{})
	// The command must not read the model or the config.
	var msgs []tea.Msg
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The rebuild is sequenced after the toggle; run the toggle.
		seq := reflect.ValueOf(cmd())
		msgs = collectMsgs(seq.Index(0).Interface().(tea.Cmd))
	}()
	com := u.com
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			u.com = com
		}
	}
	info, ok := findMsg[util.InfoMsg](msgs)
	require.True(t, ok)
	require.Equal(t, "Thinking mode enabled", info.Msg)
	require.True(t, ws.cfg.Models[config.SelectedModelTypeLarge].Think)
}

// These tests pin that settings writes behind key presses and dialog
// actions run off the Update goroutine (each is a file write, or an HTTP
// round-trip in client/server mode), and that their outcome reaches the
// user once Update applied the result message.

// runWorkDone executes a command tree and feeds the workDoneMsg results
// back into Update, as the Bubble Tea runtime would. Other messages are
// dropped.
func runWorkDone(m *UI, cmd tea.Cmd) {
	for _, msg := range collectMsgs(cmd) {
		if done, ok := msg.(workDoneMsg); ok {
			m.Update(done)
		}
	}
}

// TestComposerFooterFormSavesOffLoop pins that saving the Customize
// Composer form writes the config when its command runs and reports the
// result once Update applied it.
func TestComposerFooterFormSavesOffLoop(t *testing.T) {
	t.Parallel()

	for _, fail := range []bool{false, true} {
		u, ws := newCmdStateUI(t)
		if fail {
			ws.setErr = os.ErrPermission
		}
		u.openComposerFooterForm()
		form, ok := u.activeInline.(*questionform.QuestionForm)
		require.True(t, ok)
		cmd := form.OnAnswerCmd([]question.Answer{{SelectedIDs: []string{footerTokens, footerModel}}})
		_, written := ws.set("options.tui.composer_footer")
		require.False(t, written, "the write must not run on the Update goroutine")

		done, ok := findMsg[workDoneMsg](collectMsgs(cmd))
		require.True(t, ok)
		if !fail {
			v, _ := ws.set("options.tui.composer_footer")
			require.Equal(t, []string{footerModel, footerTokens}, v)
		}
		_, reply := u.Update(done)
		info, ok := findMsg[util.InfoMsg](collectMsgs(reply))
		require.True(t, ok)
		if fail {
			require.Equal(t, util.InfoTypeError, info.Type)
		} else {
			require.Equal(t, "Composer updated", info.Msg)
		}
	}
}
