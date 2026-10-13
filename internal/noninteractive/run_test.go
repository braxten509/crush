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
// changes to its config like the real backends do.
type fakeBackend struct {
	cfg        *config.Config
	calls      []string
	last       [2]string // Provider and model of the last assistant message.
	prepareErr error
	resolveErr error
	sessionID  string
	streamed   string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		sessionID: "sess",
		cfg: &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {Provider: "openai", Model: "gpt-4o"},
			},
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

func (f *fakeBackend) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeBackend) Config(context.Context) (*config.Config, error) {
	return f.cfg, nil
}

func (f *fakeBackend) SetModel(_ context.Context, typ config.SelectedModelType, m config.SelectedModel) error {
	f.record("set %s %s/%s effort=%q tier=%q", typ, m.Provider, m.Model, m.ReasoningEffort, m.ServiceTier)
	f.cfg.Models[typ] = m
	return nil
}

func (f *fakeBackend) DefaultSmallModel(_ context.Context, providerID string) (*config.SelectedModel, error) {
	f.record("default-small %s", providerID)
	return &config.SelectedModel{Provider: providerID, Model: "claude-small"}, nil
}

func (f *fakeBackend) LastAssistantModel(_ context.Context, sessionID string) (string, string, error) {
	f.record("last-assistant %s", sessionID)
	return f.last[0], f.last[1], nil
}

func (f *fakeBackend) UpdateAgent(context.Context) error {
	f.record("update-agent")
	return nil
}

func (f *fakeBackend) Prepare(context.Context) error {
	f.record("prepare")
	return f.prepareErr
}

func (f *fakeBackend) ResolveSession(_ context.Context, id string, useLast bool) (string, error) {
	f.record("resolve %q last=%t", id, useLast)
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.sessionID, nil
}

func (f *fakeBackend) Stream(_ context.Context, out *Output, sessionID, prompt string) error {
	f.record("stream %s %s", sessionID, prompt)
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

func TestRun_LargeModelOverridePicksDefaultSmall(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", LargeModel: "Anthropic/CLAUDE-X"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		`set large anthropic/claude-x effort="" tier=""`,
		"default-small anthropic",
		`set small anthropic/claude-small effort="" tier=""`,
		"update-agent",
		"prepare",
		`resolve "" last=false`,
		"stream sess hi",
	}, b.calls)
}

func TestRun_OverrideErrorIsWrapped(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", LargeModel: "nope"}, quietTerminal(&out))
	require.EqualError(t, err, `failed to override models: large model "nope" not found`)
	require.Empty(t, b.calls)
	require.Empty(t, out.String())
}

func TestRun_EffortAppliedBeforeSessionWithoutContinue(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", LargeModel: "o3", ReasoningEffort: "high"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		`set large openai/o3 effort="" tier=""`,
		"default-small openai",
		`set small openai/claude-small effort="" tier=""`,
		"update-agent",
		`set large openai/o3 effort="high" tier=""`,
		"update-agent",
		"prepare",
		`resolve "" last=false`,
		"stream sess hi",
	}, b.calls)
}

func TestRun_ContinueRestoresModelThenAppliesEffort(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.last = [2]string{"anthropic", "claude-x"}
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", ContinueSessionID: "sess", ReasoningEffort: "high"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"prepare",
		`resolve "sess" last=false`,
		"last-assistant sess",
		`set large anthropic/claude-x effort="" tier=""`,
		"default-small anthropic",
		`set small anthropic/claude-small effort="" tier=""`,
		"update-agent",
		`set large anthropic/claude-x effort="high" tier=""`,
		"update-agent",
		"stream sess hi",
	}, b.calls)
}

func TestRun_ContinueKeepsSmallModelWhenSet(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.cfg.Models[config.SelectedModelTypeSmall] = config.SelectedModel{Provider: "openai", Model: "gpt-4o"}
	b.last = [2]string{"anthropic", "claude-x"}
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", UseLast: true}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		"prepare",
		`resolve "" last=true`,
		"last-assistant sess",
		`set large anthropic/claude-x effort="" tier=""`,
		"update-agent",
		"stream sess hi",
	}, b.calls)
}

func TestRun_ContinueSkipsUnavailableOrSameModel(t *testing.T) {
	t.Parallel()

	for _, last := range [][2]string{
		{"gone", "model"},
		{"openai", "gpt-4o"},
		{"", ""},
	} {
		b := newFakeBackend()
		b.last = last
		var out bytes.Buffer
		err := Run(t.Context(), b, Options{Prompt: "hi", UseLast: true}, quietTerminal(&out))
		require.NoError(t, err)
		require.Equal(t, []string{
			"prepare",
			`resolve "" last=true`,
			"last-assistant sess",
			"stream sess hi",
		}, b.calls, "last=%v", last)
	}
}

func TestRun_ContinueWithModelOverrideDoesNotRestore(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	b.last = [2]string{"anthropic", "claude-x"}
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", UseLast: true, SmallModel: "gpt-4o"}, quietTerminal(&out))
	require.NoError(t, err)
	require.Equal(t, []string{
		`set small openai/gpt-4o effort="" tier=""`,
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
	require.Empty(t, b.calls)
}

func TestRun_NoLargeModelForTuning(t *testing.T) {
	t.Parallel()

	b := newFakeBackend()
	delete(b.cfg.Models, config.SelectedModelTypeLarge)
	var out bytes.Buffer
	err := Run(t.Context(), b, Options{Prompt: "hi", Fast: true}, quietTerminal(&out))
	require.EqualError(t, err, "no large model selected; set one with the --model flag or 'model large'")
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
