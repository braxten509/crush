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
	// SetModel sets the preferred model of the given type for this run.
	SetModel(ctx context.Context, typ config.SelectedModelType, model config.SelectedModel) error
	// DefaultSmallModel returns the small model to pair with a large model
	// from providerID. A nil model means there is none to set.
	DefaultSmallModel(ctx context.Context, providerID string) (*config.SelectedModel, error)
	// LastAssistantModel returns the provider and model of the session's
	// last non-summary assistant message, or empty strings when there is
	// none.
	LastAssistantModel(ctx context.Context, sessionID string) (provider, model string, err error)
	// UpdateAgent rebuilds the agent's models after a model change.
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
	Prompt            string
	LargeModel        string
	SmallModel        string
	ReasoningEffort   string
	Fast              bool
	ContinueSessionID string
	UseLast           bool
}

// continuing reports whether the run continues an existing session.
func (o Options) continuing() bool {
	return o.ContinueSessionID != "" || o.UseLast
}

// modelOverride reports whether --model or --small-model was given.
func (o Options) modelOverride() bool {
	return o.LargeModel != "" || o.SmallModel != ""
}

// Run executes one non-interactive turn against b and writes the reply
// through term.
func Run(ctx context.Context, b Backend, opts Options, term Terminal) error {
	slog.Info("Running in non-interactive mode")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.modelOverride() {
		if err := overrideModels(ctx, b, opts.LargeModel, opts.SmallModel); err != nil {
			return fmt.Errorf("failed to override models: %w", err)
		}
	}

	// The reasoning effort and fast mode apply to the model that will
	// actually run. On a continued session without an explicit model
	// override, the model is resolved later from the session's last
	// assistant message, so the override is applied after that restore
	// instead.
	tuned := opts.ReasoningEffort != "" || opts.Fast
	deferredEffort := opts.continuing() && !opts.modelOverride()
	if tuned && !deferredEffort {
		if err := overrideTuning(ctx, b, opts.ReasoningEffort, opts.Fast); err != nil {
			return err
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
		// If no explicit model override was requested, restore the
		// model/provider from the last assistant message in the
		// session, provided it is still available.
		if !opts.modelOverride() {
			if err := restoreModelFromSession(ctx, b, sessionID); err != nil {
				slog.Warn("Failed to restore model from session", "error", err)
			}
		}
	} else {
		slog.Info("Created session for non-interactive run", "session_id", sessionID)
	}

	if tuned && deferredEffort {
		if err := overrideTuning(ctx, b, opts.ReasoningEffort, opts.Fast); err != nil {
			return err
		}
	}

	return b.Stream(ctx, out, sessionID, opts.Prompt)
}

// overrideModels resolves the --model and --small-model strings and sets
// them for this run, then rebuilds the agent. If only largeModel is given,
// the small model defaults to the large model's provider's default.
func overrideModels(ctx context.Context, b Backend, largeModel, smallModel string) error {
	cfg, err := b.Config(ctx)
	if err != nil {
		return err
	}
	providers := cfg.Providers.Copy()

	var largeProviderID string

	if largeModel != "" {
		found, err := ResolveModel(providers, largeModel, "large")
		if err != nil {
			return err
		}
		largeProviderID = found.Provider
		slog.Info("Overriding large model for non-interactive run", "provider", found.Provider, "model", found.ModelID)
		if err := b.SetModel(ctx, config.SelectedModelTypeLarge, config.SelectedModel{
			Provider: found.Provider,
			Model:    found.ModelID,
		}); err != nil {
			return fmt.Errorf("failed to set large model: %w", err)
		}
	}

	switch {
	case smallModel != "":
		found, err := ResolveModel(providers, smallModel, "small")
		if err != nil {
			return err
		}
		slog.Info("Overriding small model for non-interactive run", "provider", found.Provider, "model", found.ModelID)
		if err := b.SetModel(ctx, config.SelectedModelTypeSmall, config.SelectedModel{
			Provider: found.Provider,
			Model:    found.ModelID,
		}); err != nil {
			return fmt.Errorf("failed to set small model: %w", err)
		}

	case largeModel != "":
		sm, err := b.DefaultSmallModel(ctx, largeProviderID)
		if err != nil {
			slog.Warn("Failed to get default small model", "error", err)
		} else if sm != nil {
			if err := b.SetModel(ctx, config.SelectedModelTypeSmall, *sm); err != nil {
				return fmt.Errorf("failed to set small model: %w", err)
			}
		}
	}

	return b.UpdateAgent(ctx)
}

// restoreModelFromSession reads the last assistant message in the session
// and, if it used a different provider/model than the current config,
// sets that model for this run provided it is still available. This
// ensures that continuing a session uses the same model that produced the
// last response.
func restoreModelFromSession(ctx context.Context, b Backend, sessionID string) error {
	provider, model, err := b.LastAssistantModel(ctx, sessionID)
	if err != nil {
		return err
	}
	if provider == "" || model == "" {
		return nil
	}

	cfg, err := b.Config(ctx)
	if err != nil {
		return err
	}
	currentLarge := cfg.Models[config.SelectedModelTypeLarge]
	if currentLarge.Provider == provider && currentLarge.Model == model {
		return nil
	}

	if !cfg.IsModelAvailable(provider, model) {
		slog.Debug("Skipping model restoration: provider/model not available",
			"provider", provider,
			"model", model)
		return nil
	}

	if err := b.SetModel(ctx, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	}); err != nil {
		return fmt.Errorf("failed to set large model: %w", err)
	}

	if _, ok := cfg.Models[config.SelectedModelTypeSmall]; !ok {
		sm, err := b.DefaultSmallModel(ctx, provider)
		if err != nil {
			slog.Warn("Failed to get default small model", "error", err)
		} else if sm != nil {
			if err := b.SetModel(ctx, config.SelectedModelTypeSmall, *sm); err != nil {
				slog.Warn("Failed to set small model during session restore", "error", err)
			}
		}
	}

	if err := b.UpdateAgent(ctx); err != nil {
		return fmt.Errorf("failed to update agent models: %w", err)
	}
	slog.Info("Restored model from session",
		"provider", provider,
		"model", model)
	return nil
}

// overrideTuning validates the requested reasoning effort and fast mode
// against the large model in effect for this run (which may have been
// overridden by --model or restored from a continued session) and applies
// them. An empty effort keeps the model's own.
func overrideTuning(ctx context.Context, b Backend, reasoningEffort string, fast bool) error {
	cfg, err := b.Config(ctx)
	if err != nil {
		return err
	}

	selected, ok := cfg.Models[config.SelectedModelTypeLarge]
	if !ok {
		return fmt.Errorf("no large model selected; set one with the --model flag or 'model large'")
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
	slog.Info("Overriding model tuning for non-interactive run",
		"provider", selected.Provider,
		"model", selected.Model,
		"reasoning_effort", selected.ReasoningEffort,
		"service_tier", selected.ServiceTier)
	if err := b.SetModel(ctx, config.SelectedModelTypeLarge, selected); err != nil {
		return fmt.Errorf("failed to set model tuning: %w", err)
	}
	return b.UpdateAgent(ctx)
}
