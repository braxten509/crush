package noninteractive

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/crush/internal/config"
)

// Backend is what a non-interactive run needs from the place the agent
// runs: the in-process app or a server reached through the client. The
// two implementations differ only in transport; the run's sequencing
// lives in [Run].
type Backend interface {
	// Config returns the configuration the run's model choices are
	// resolved and validated against.
	Config(ctx context.Context) (*config.Config, error)
	// ChatModel returns the model the session owns; ok is false when it
	// has none.
	ChatModel(ctx context.Context, sessionID string) (model config.SelectedModel, ok bool, err error)
	// SetChatModel saves model on the session: every chat owns its model.
	SetChatModel(ctx context.Context, sessionID string, model config.SelectedModel) error
	// SetHelperModel sets the helper model (titles, branch summaries) for
	// this run only.
	SetHelperModel(ctx context.Context, model config.SelectedModel) error
	// UpdateAgent rebuilds the agent after a helper model change.
	UpdateAgent(ctx context.Context) error
	// Prepare waits until the agent is ready to take the prompt (MCP
	// initialization and the like) and refreshes its tools. Errors are
	// returned ready for the user.
	Prepare(ctx context.Context) error
	// ResolveSession returns the ID of the session the run continues or,
	// when neither continueSessionID nor useLast is set, of a new one.
	// Errors are returned ready for the user.
	ResolveSession(ctx context.Context, continueSessionID string, useLast bool) (string, error)
	// Stream sends prompt to the session and writes the reply to out
	// until the run completes or ctx is cancelled.
	Stream(ctx context.Context, out *Output, sessionID, prompt string) error
}

// Options are the user's choices for one non-interactive run.
type Options struct {
	Prompt string
	// Model is saved on the run's chat (--model).
	Model string
	// HelperModel is the helper model for this run only (--small-model).
	HelperModel       string
	ReasoningEffort   string
	Fast              bool
	ContinueSessionID string
	UseLast           bool
}

// continuing reports whether the run continues an existing session.
func (o Options) continuing() bool {
	return o.ContinueSessionID != "" || o.UseLast
}

// Run executes one non-interactive turn against b and writes the reply
// through term.
func Run(ctx context.Context, b Backend, opts Options, term Terminal) error {
	slog.Info("Running in non-interactive mode")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chosen, helper, err := resolveModels(ctx, b, opts.Model, opts.HelperModel)
	if err != nil {
		return fmt.Errorf("failed to override models: %w", err)
	}
	if helper != nil {
		slog.Info("Overriding the helper model for non-interactive run", "provider", helper.Provider, "model", helper.Model)
		if err := b.SetHelperModel(ctx, *helper); err != nil {
			return fmt.Errorf("failed to set helper model: %w", err)
		}
		if err := b.UpdateAgent(ctx); err != nil {
			return fmt.Errorf("failed to override models: %w", err)
		}
	}

	out := newOutput(ctx, cancel, term)

	if err := b.Prepare(ctx); err != nil {
		out.StopSpinner()
		return err
	}

	defer out.StopSpinner()

	sessionID, err := b.ResolveSession(ctx, opts.ContinueSessionID, opts.UseLast)
	if err != nil {
		return err
	}
	if opts.continuing() {
		slog.Info("Continuing session for non-interactive run", "session_id", sessionID)
	} else {
		slog.Info("Created session for non-interactive run", "session_id", sessionID)
	}

	// --model, the reasoning effort and fast mode change the run's own
	// chat, which keeps them; no other chat sees them.
	if chosen != nil || opts.ReasoningEffort != "" || opts.Fast {
		if err := tuneChatModel(ctx, b, sessionID, chosen, opts.ReasoningEffort, opts.Fast); err != nil {
			out.StopSpinner()
			return err
		}
	}

	return b.Stream(ctx, out, sessionID, opts.Prompt)
}

// resolveModels resolves the --model and --small-model strings against the
// configured providers, so a wrong name fails before anything runs.
func resolveModels(ctx context.Context, b Backend, chatModel, helperModel string) (chosen, helper *config.SelectedModel, err error) {
	if chatModel == "" && helperModel == "" {
		return nil, nil, nil
	}
	cfg, err := b.Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	providers := cfg.Providers.Copy()
	if chatModel != "" {
		found, err := ResolveModel(providers, chatModel, "chat")
		if err != nil {
			return nil, nil, err
		}
		chosen = &config.SelectedModel{Provider: found.Provider, Model: found.ModelID}
	}
	if helperModel != "" {
		found, err := ResolveModel(providers, helperModel, "helper")
		if err != nil {
			return nil, nil, err
		}
		helper = &config.SelectedModel{Provider: found.Provider, Model: found.ModelID}
	}
	return chosen, helper, nil
}

// tuneChatModel applies --model, the reasoning effort and fast mode to the
// session's own model, validated against the model that will run, and
// saves it on the session. An empty effort keeps the model's own.
func tuneChatModel(ctx context.Context, b Backend, sessionID string, chosen *config.SelectedModel, reasoningEffort string, fast bool) error {
	cfg, err := b.Config(ctx)
	if err != nil {
		return err
	}
	var selected config.SelectedModel
	if chosen != nil {
		selected = *chosen
		slog.Info("Overriding the chat's model for non-interactive run", "provider", selected.Provider, "model", selected.Model)
	} else {
		current, ok, err := b.ChatModel(ctx, sessionID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("the chat has no model; set one with the --model flag")
		}
		selected = current
	}
	if reasoningEffort != "" {
		if err := cfg.ValidateReasoningEffort(selected.Provider, selected.Model, reasoningEffort); err != nil {
			return err
		}
		selected.ReasoningEffort = reasoningEffort
	}
	if fast {
		if err := cfg.ValidateFastMode(selected.Provider, selected.Model); err != nil {
			return err
		}
		selected.ServiceTier = "fast"
	}
	slog.Info("Saving the chat's model for non-interactive run",
		"provider", selected.Provider,
		"model", selected.Model,
		"reasoning_effort", selected.ReasoningEffort,
		"service_tier", selected.ServiceTier)
	if err := b.SetChatModel(ctx, sessionID, selected); err != nil {
		return fmt.Errorf("failed to set the chat's model: %w", err)
	}
	return nil
}
