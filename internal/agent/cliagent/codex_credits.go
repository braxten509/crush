package cliagent

import "time"

type codexTokenCounts struct {
	InputTokens       int64 `json:"inputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	ReasoningTokens   int64 `json:"reasoningOutputTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// CodexUsage is the latest cumulative credit estimate for one native turn.
type CodexUsage struct {
	ThreadID string
	TurnID   string
	Credits  float64
}

type codexTurnCredits struct {
	selectedModel string
	tier          string
	turnID        string
	model         string
	previous      *codexTokenCounts
	total         float64
}

func (c *codexTurnCredits) start(turnID string) {
	if c.turnID != turnID {
		c.turnID, c.model = turnID, c.selectedModel
		c.previous, c.total = nil, 0
	}
}

func (r *codexRun) recordCreditUsage(params codexParams) {
	usage := params.TokenUsage
	active := r.active.Load()
	if r.t.OnCodexUsage == nil || usage == nil || usage.Total == nil || active == nil ||
		params.ThreadID != active[0] || params.TurnID == "" || params.TurnID != active[1] {
		return
	}

	credits := &r.credits
	credits.start(params.TurnID)
	counts := credits.newTokens(usage.Total, usage.Last)
	added := codexCreditEstimate(credits.model, credits.tier, counts, time.Now())
	if added > 0 {
		credits.total += added
		r.t.OnCodexUsage(CodexUsage{ThreadID: params.ThreadID, TurnID: params.TurnID, Credits: credits.total})
	}
}

// newTokens uses the latest request for the first update. The thread's
// lifetime total may include old or inherited history. Subsequent updates
// use the increase in each counter, so repeats and stale updates add nothing.
func (c *codexTurnCredits) newTokens(total *codexTokenCounts, last codexTokenCounts) codexTokenCounts {
	if c.previous == nil {
		previous := *total
		c.previous = &previous
		return last
	}
	previous := c.previous
	delta := codexTokenCounts{
		InputTokens:       max(0, total.InputTokens-previous.InputTokens),
		CachedInputTokens: max(0, total.CachedInputTokens-previous.CachedInputTokens),
		OutputTokens:      max(0, total.OutputTokens-previous.OutputTokens),
	}
	previous.InputTokens = max(previous.InputTokens, total.InputTokens)
	previous.CachedInputTokens = max(previous.CachedInputTokens, total.CachedInputTokens)
	previous.OutputTokens = max(previous.OutputTokens, total.OutputTokens)
	return delta
}

type codexCreditRate struct {
	input, cached, output float64
}

// Standard Codex credit prices per million tokens, from
// https://learn.chatgpt.com/docs/pricing#token-rates. API-key pricing is separate.
var codexCreditRates = map[string]codexCreditRate{
	"gpt-6.2-astra":           {200, 10, 1000},
	"gpt-6.2-astra-law":       {200, 10, 1000},
	"gpt-6.2-sol":             {50, 2.5, 250},
	"gpt-6.1-sol":             {50, 2.5, 250},
	"gpt-6.1-terra":           {25, 1.25, 125},
	"gpt-6-astra":             {250, 25, 1250},
	"gpt-6-astra-law":         {250, 25, 1250},
	"gpt-6-sol":               {50, 5, 250},
	"gpt-6-luna":              {2.5, 0.25, 12.5},
	"gpt-5.6-sol":             {100, 10, 500},
	"gpt-5.6-terra":           {50, 5, 300},
	"gpt-5.6-luna":            {5, 0.5, 30},
	"gpt-6-rosalind-research": {125, 12.5, 750},
}

func codexCreditEstimate(model, tier string, counts codexTokenCounts, at time.Time) float64 {
	rate := codexCreditRates[model]
	cached := min(max(0, counts.CachedInputTokens), max(0, counts.InputTokens))
	uncached := max(0, counts.InputTokens-cached)
	output := max(0, counts.OutputTokens) // includes reasoning tokens
	credits := (float64(uncached)*rate.input + float64(cached)*rate.cached + float64(output)*rate.output) / 1_000_000

	switch tier {
	case "fast", "priority":
		return credits * 2
	case "ultrafast":
		// The 3x purchased-credit promotion ends at midnight Pacific on March 1.
		if (model == "gpt-6.2-astra" || model == "gpt-6.2-sol") && at.Before(time.Date(2027, time.March, 1, 8, 0, 0, 0, time.UTC)) {
			return credits * 3
		}
		return credits * 6
	default:
		return credits
	}
}
