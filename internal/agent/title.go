package agent

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
)

//go:embed templates/title.md
var titlePrompt []byte

// Used to remove <think> tags from generated titles.
var (
	thinkTagRegex       = regexp.MustCompile(`(?s)<think>.*?</think>`)
	orphanThinkTagRegex = regexp.MustCompile(`</?think>`)
)

// GenerateTitle generates a session title based on the initial prompt.
func (a *sessionAgent) GenerateTitle(ctx context.Context, sessionID string, userPrompt string) {
	a.titles.begin()
	defer a.titles.end()
	a.generateTitle(ctx, sessionID, userPrompt)
}

// generateTitle is GenerateTitle without tracking, for callers that
// register with a.titles themselves.
func (a *sessionAgent) generateTitle(ctx context.Context, sessionID string, userPrompt string) {
	if userPrompt == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	// A branch keeps the title it was made with: neither a generated title
	// nor the fallback name replaces it. A failed lookup generates one, as
	// before.
	if bo, ok := a.messages.(message.BranchOrigins); ok {
		if _, isBranch, err := bo.BranchOrigin(ctx, sessionID); err == nil && isBranch {
			return
		}
	}

	// Ensure the session always gets a title even if every path below
	// fails or the context is cancelled before we finish: the start of the
	// prompt, as for an empty reply, so a chat whose title models all
	// failed (out of usage, too slow) is never left "Untitled".
	var titleSaved bool
	defer func() {
		if !titleSaved {
			// WithoutCancel also drops the title timeout's deadline.
			fallbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := a.sessions.Rename(fallbackCtx, sessionID, cleanTitle("", userPrompt)); err != nil {
				slog.Error("Failed to save fallback session title", "error", err)
			}
		}
	}()

	resp, model, success := a.streamTitle(ctx, sessionID, userPrompt)
	if !success {
		// The deferred fallback names the chat from the prompt.
		return
	}

	title := cleanTitle(resp.Response.Content.Text(), userPrompt)

	// Calculate usage and cost, at the models Prism routed a Hyper title to.
	overrideCost := a.stepsOpenrouterCost(resp.Steps)
	if hyperCost := a.hyperStepsCost(model, resp.Steps); hyperCost != nil {
		overrideCost = hyperCost
	}
	usage := ownUsage(model, resp.TotalUsage)
	cost := titleCost(model, usage, overrideCost)
	promptTokens := contextTokens(usage)
	completionTokens := usage.OutputTokens

	// Atomically update only title and usage fields to avoid overriding other
	// concurrent session updates.
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, promptTokens, completionTokens, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
		return
	}
	titleSaved = true
}

// streamTitle asks the chat's own model for a title, using its lowest
// reasoning effort so titles arrive quickly. It falls back to helperModel
// only when the chat has no built model (for unit tests with a fixed helper).
func (a *sessionAgent) streamTitle(ctx context.Context, sessionID, userPrompt string) (*fantasy.AgentResult, Model, bool) {
	newAgent := func(m fantasy.LanguageModel, p []byte, tok int64) fantasy.Agent {
		return fantasy.NewAgent(
			m,
			fantasy.WithSystemPrompt(string(p)+"\n /no_think"),
			fantasy.WithMaxOutputTokens(tok),
			fantasy.WithUserAgent(userAgent),
		)
	}

	// Each model gets the system prompt prefix its own provider asks for,
	// with low/minimal reasoning so titles don't stall on deep thinking.
	streamCall := func(model Model, systemPromptPrefix string) fantasy.AgentStreamCall {
		return fantasy.AgentStreamCall{
			Prompt:          fmt.Sprintf("Generate a concise title for the following content:\n\n%s\n <think>\n\n</think>", userPrompt),
			Headers:         sessionHeaders(sessionID),
			ProviderOptions: a.titleProviderOptions(model),
			PrepareStep: func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
				prepared.Messages = opts.Messages
				if systemPromptPrefix != "" {
					prepared.Messages = append([]fantasy.Message{
						fantasy.NewSystemMessage(systemPromptPrefix),
					}, prepared.Messages...)
				}
				return callCtx, prepared, nil
			},
		}
	}

	type modelAttempt struct {
		name  string
		model func() (Model, error)
	}
	attempts := []modelAttempt{
		{"chat's own", func() (Model, error) { return a.ChatModel(ctx, sessionID) }},
		{"helper", func() (Model, error) { return a.helperModel.Get(), nil }},
	}

	var triedKey string
	for _, attempt := range attempts {
		model, err := attempt.model()
		if err != nil || model.Model == nil {
			continue
		}
		key := model.ModelCfg.Provider + "\n" + model.ModelCfg.Model + "\n" + model.ModelCfg.Account
		if key != "\n\n" && key == triedKey {
			continue
		}
		triedKey = key
		tok := int64(80)
		if model.CatwalkCfg.CanReason() {
			tok = min(model.CatwalkCfg.DefaultMaxTokens, 512)
			if tok < 128 {
				tok = 512
			}
		}
		agent := newAgent(model.Model, titlePrompt, tok)
		resp, err := agent.Stream(ctx, streamCall(model, a.promptPrefixFor(model)))
		if err == nil && resp.Response.FinishReason != fantasy.FinishReasonLength {
			slog.Debug("Generated title with the " + attempt.name + " model")
			return resp, model, true
		}
		if err != nil {
			slog.Error("Error generating title with "+attempt.name+" model; trying next", "err", err)
		} else {
			slog.Error("Title generation hit token limit with " + attempt.name + " model; trying next")
		}
	}
	return nil, Model{}, false
}

// titleProviderOptions requests minimal/low reasoning for title calls so
// reasoning models return a short title in seconds rather than thinking long.
func (a *sessionAgent) titleProviderOptions(model Model) fantasy.ProviderOptions {
	if a.cfg == nil {
		return nil
	}
	providerCfg, ok := a.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return nil
	}
	low := model
	low.ModelCfg.Think = false
	if levels := model.CatwalkCfg.ReasoningEffortLevels(); len(levels) > 0 {
		switch {
		case slices.Contains(levels, "minimal"):
			low.ModelCfg.ReasoningEffort = "minimal"
		case slices.Contains(levels, "low"):
			low.ModelCfg.ReasoningEffort = "low"
		default:
			low.ModelCfg.ReasoningEffort = levels[0]
		}
	}
	return getProviderOptions(low, providerCfg)
}

// cleanTitle turns a model's reply into a one-line title, falling back to
// the start of the user's prompt when the reply is empty.
func cleanTitle(text, userPrompt string) string {
	// Clean up title.
	title := strings.ReplaceAll(text, "\n", " ")

	// Remove thinking tags if present.
	title = thinkTagRegex.ReplaceAllString(title, "")
	title = orphanThinkTagRegex.ReplaceAllString(title, "")

	title = strings.TrimSpace(title)
	if title == "" {
		// LLM returned empty content. Use the prompt itself as a
		// fallback title, truncated to 50 chars, before resorting to
		// the generic default.
		fallback := strings.ReplaceAll(userPrompt, "\n", " ")
		fallback = strings.TrimSpace(fallback)
		if len(fallback) > 50 {
			fallback = ansi.Truncate(fallback, 50, "…")
		}
		title = cmp.Or(fallback, DefaultSessionName)
	}
	return title
}

// titleCost prices a title request from the model's rates, preferring the
// cost OpenRouter reported or the Prism-routed price of a Hyper title.
// Flat-rate models cost nothing.
func titleCost(model Model, usage fantasy.Usage, overrideCost *float64) float64 {
	cost := requestCost(model.CatwalkCfg, usage)

	// Use override cost if available (e.g., from OpenRouter or Hyper Prism).
	if overrideCost != nil {
		cost = *overrideCost
	}

	// Skip cost accumulation
	if model.FlatRate {
		cost = 0
	}
	return cost
}
