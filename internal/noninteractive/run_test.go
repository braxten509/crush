package noninteractive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// fakeBackend records the calls [Run] makes, in order, and applies model
// changes like the real backends do.
type fakeBackend struct {
	cfg        *config.Config
	calls      []string
	chats      map[string]config.SelectedModel // Each chat's own model.
	helper     config.SelectedModel
	prepareErr error
	resolveErr error
	sessionID  string
	streamed   string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		sessionID: "sess",
		chats:     map[string]config.SelectedModel{"sess": {Provider: "openai", Model: "gpt-4o"}},
		cfg: &config.Config{
			Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
				"openai": {ID: "openai", Models: []catwalk.Model{
					{ID: "gpt-4o"},
					{ID: "o3", ReasoningLevels: []string{"low", "high"}},
				}},
				"anthropic": {ID: "anthropic", Models: []catwalk.Model{
					{ID: "claude-x", ReasoningLevels: []string{"high"}},
					{ID: "claude-small"},
				}},
			}),
		},
	}
}

func (f *fakeBackend) recordf(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeBackend) Config(context.Context) (*config.Config, error) {
	return f.cfg, nil
}

func (f *fakeBackend) ChatModel(_ context.Context, sessionID string) (config.SelectedModel, bool, error) {
	f.recordf("chat-model %s", sessionID)
	m, ok := f.chats[sessionID]
	return m, ok, nil
}

func (f *fakeBackend) SetChatModel(_ context.Context, sessionID string, m config.SelectedModel) error {
	f.recordf("chat %s %s/%s effort=%q tier=%q", sessionID, m.Provider, m.Model, m.ReasoningEffort, m.ServiceTier)
	f.chats[sessionID] = m
	return nil
}

func (f *fakeBackend) SetHelperModel(_ context.Context, m config.SelectedModel) error {
	f.recordf("helper %s/%s", m.Provider, m.Model)
	f.helper = m
	return nil
}

func (f *fakeBackend) UpdateAgent(context.Context) error {
	f.recordf("update-agent")
	return nil
}

func (f *fakeBackend) Prepare(context.Context) error {
	f.recordf("prepare")
	return f.prepareErr
}

func (f *fakeBackend) ResolveSession(_ context.Context, id string, useLast bool) (string, error) {
	f.recordf("resolve %q last=%t", id, useLast)
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.sessionID, nil
}

func (f *fakeBackend) Stream(_ context.Context, out *Output, sessionID, prompt string) error {
	f.recordf("stream %s %s", sessionID, prompt)
	defer out.Finish()
	_, err := fmt.Fprint(out.Stdout, f.streamed)
	return err
}

// fakeSpinner counts starts and stops.
type fakeSpinner struct{ starts, stops int }

func (s *fakeSpinner) Start() { s.starts++ }
func (s *fakeSpinner) Stop()  { s.stops++ }

func quietTerminal(out *bytes.Buffer) Terminal {
	return Terminal{Stdout: out, HideSpinner: true}
}

func TestRun_NewSessionStreamsAndEndsWithNewline(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.streamed = "hello"
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, "hello\n", out.String())
	require.Equal(t, []string{
		"prepare",
		`resolve "" last=false`,
		"stream sess hi",
	}, b.calls)
}

// TestRun_ModelOverrideIsSavedOnTheChat: --model is saved on the run's own
// chat; no folder-wide model and no helper model is written.
func TestRun_ModelOverrideIsSavedOnTheChat(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", Model: "Anthropic/CLAUDE-X"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"prepare",
		`resolve "" last=false`,
		`chat sess anthropic/claude-x effort="" tier=""`,
		"stream sess hi",
	}, b.calls)
}

func TestRun_OverrideErrorIsWrapped(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", Model: "nope"}, quietTerminal(&out))
	require.EqualError(t, err, `failed to override models: chat model "nope" not found`)
	require.Empty(t, b.calls)
	require.Empty(t, out.String())
}

func TestRun_EffortAppliesToTheModelOverride(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", Model: "o3", ReasoningEffort: "high"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"prepare",
		`resolve "" last=false`,
		`chat sess openai/o3 effort="high" tier=""`,
		"stream sess hi",
	}, b.calls)
}

// TestRun_ContinueAppliesEffortToTheChatsModel: a continued chat keeps the
// model it owns; the effort is applied to that one.
func TestRun_ContinueAppliesEffortToTheChatsModel(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.chats["sess"] = config.SelectedModel{Provider: "anthropic", Model: "claude-x"}
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", ContinueSessionID: "sess", ReasoningEffort: "high"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"prepare",
		`resolve "sess" last=false`,
		"chat-model sess",
		`chat sess anthropic/claude-x effort="high" tier=""`,
		"stream sess hi",
	}, b.calls)
}

// TestRun_HelperModelIsForThisRunOnly: --small-model sets the helper model
// for this run; the chat keeps its own model.
func TestRun_HelperModelIsForThisRunOnly(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", UseLast: true, HelperModel: "gpt-4o"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"helper openai/gpt-4o",
		"update-agent",
		"prepare",
		`resolve "" last=true`,
		"stream sess hi",
	}, b.calls)
}

func TestRun_InvalidEffortFails(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", ReasoningEffort: "high"}, quietTerminal(&out))
	require.EqualError(t, err, `model "gpt-4o" does not support reasoning effort`)
	require.Equal(t, []string{"prepare", `resolve "" last=false`, "chat-model sess"}, b.calls)
}

func TestRun_NoChatModelForTuning(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	delete(b.chats, "sess")
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", Fast: true}, quietTerminal(&out))
	require.EqualError(t, err, "the chat has no model; set one with the --model flag")
}

func TestRun_PrepareAndResolveErrorsAreReturnedAsIs(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.prepareErr = errors.New("agent not ready: boom")
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi"}, quietTerminal(&out))
	require.EqualError(t, err, "agent not ready: boom")
	require.Equal(t, []string{"prepare"}, b.calls)
	require.Empty(t, out.String())

	b = newFakeBackend()
	b.resolveErr = errors.New("failed to resolve session: nope")
	err = Run(t.Context(), b, Options{Prompt: "hi", UseLast: true}, quietTerminal(&out))
	require.EqualError(t, err, "failed to resolve session: nope")
	require.Equal(t, []string{"prepare", `resolve "" last=true`}, b.calls)
}

func TestOutput_StopSpinnerIsIdempotentAndHonoursHide(t *testing.T) {
	t.Parallel()

	s := &fakeSpinner{}
	o := &Output{spinner: s}
	o.StopSpinner()
	o.StopSpinner()
	require.Equal(t, 1, s.stops)

	hidden := &fakeSpinner{}
	o = &Output{spinner: hidden, hideSpinner: true}
	o.StopSpinner()
	require.Zero(t, hidden.stops)

	var nilOutput *Output
	nilOutput.StopSpinner()
}

func TestOutput_SpinnerOnlyOnTerminal(t *testing.T) {
	t.Parallel()

	// Test stderr is not a terminal, so no spinner is built.
	built := false
	var out bytes.Buffer
	o := newOutput(t.Context(), func() {}, Terminal{
		Stdout: &out,
		NewSpinner: func(context.Context, context.CancelFunc) Spinner {
			built = true
			return &fakeSpinner{}
		},
	})
	require.False(t, built)
	o.Finish()
	require.Equal(t, "\n", out.String())
}

func TestOutput_SubAgentStatusStopsSpinner(t *testing.T) {
	t.Parallel()

	s := &fakeSpinner{}
	o := &Output{spinner: s}
	o.SubAgentStatus(true, false)
	require.Equal(t, 1, s.stops)
}
