package agent

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
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
	if userPrompt == "" {
		return
	}
	// A branch keeps the title it was made with: neither a generated title
	// nor the fallback name replaces it. A failed lookup generates one, as
	// before.
	if bo, ok := a.messages.(message.BranchOrigins); ok {
		if _, isBranch, err := bo.BranchOrigin(ctx, sessionID); err == nil && isBranch {
			return
		}
	}

	// Ensure the session always gets a title even if every path below
	// fails or the context is cancelled before we finish.
	var titleSaved bool
	defer func() {
		if !titleSaved {
			fallbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := a.sessions.Rename(fallbackCtx, sessionID, DefaultSessionName); err != nil {
				slog.Error("Failed to save fallback session title", "error", err)
			}
		}
	}()

	resp, model, success := a.streamTitle(ctx, sessionID, userPrompt)
	if !success {
		// The deferred fallback will save the default session name.
		return
	}

	title := cleanTitle(resp.Response.Content.Text(), userPrompt)

	// Calculate usage and cost.
	cost := titleCost(model, resp.TotalUsage, a.stepsOpenrouterCost(resp.Steps))
	promptTokens := contextTokens(resp.TotalUsage)
	completionTokens := resp.TotalUsage.OutputTokens

	// Atomically update only title and usage fields to avoid overriding other
	// concurrent session updates.
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, promptTokens, completionTokens, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
		return
	}
	titleSaved = true
}

// streamTitle asks the helper model, then the chat's own model (owner's
// D2a), for a title. It returns the first complete response and the model
// that produced it, or false when every model failed or ran out of tokens.
func (a *sessionAgent) streamTitle(ctx context.Context, sessionID, userPrompt string) (*fantasy.AgentResult, Model, bool) {
	newAgent := func(m fantasy.LanguageModel, p []byte, tok int64) fantasy.Agent {
		return fantasy.NewAgent(
			m,
			fantasy.WithSystemPrompt(string(p)+"\n /no_think"),
			fantasy.WithMaxOutputTokens(tok),
			fantasy.WithUserAgent(userAgent),
		)
	}

	// Each model gets the system prompt prefix its own provider asks for.
	streamCall := func(systemPromptPrefix string) fantasy.AgentStreamCall {
		return fantasy.AgentStreamCall{
			Prompt:  fmt.Sprintf("Generate a concise title for the following content:\n\n%s\n <think>\n\n</think>", userPrompt),
			Headers: sessionHeaders(sessionID),
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
		{"helper", func() (Model, error) { return a.helperModel.Get(), nil }},
		{"chat's own", func() (Model, error) { return a.ChatModel(ctx, sessionID) }},
	}

	for _, attempt := range attempts {
		model, err := attempt.model()
		if err != nil || model.Model == nil {
			slog.Error("No "+attempt.name+" model for the title; trying next", "err", err)
			continue
		}
		tok := int64(40)
		if model.CatwalkCfg.CanReason {
			tok = model.CatwalkCfg.DefaultMaxTokens
		}
		agent := newAgent(model.Model, titlePrompt, tok)
		resp, err := agent.Stream(ctx, streamCall(a.promptPrefixFor(model)))
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
// cost OpenRouter reported. Flat-rate models cost nothing.
func titleCost(model Model, usage fantasy.Usage, openrouterCost *float64) float64 {
	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	// Use override cost if available (e.g., from OpenRouter).
	if openrouterCost != nil {
		cost = *openrouterCost
	}

	// Skip cost accumulation
	if model.FlatRate {
		cost = 0
	}
	return cost
}
