package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
)

// Sub-agents follow the fallback order too (owner, 2026-10-21): the model a
// sub-agent was started on picks the list, as the model the user picked does
// for a chat. When its run ends on a usage limit, the list says where it
// goes; it then continues its own child session there.

// subAgentFallbackMoves bounds the moves of one sub-agent run.
const subAgentFallbackMoves = 6

// subAgentNext is where a sub-agent goes after its account ran out: a new
// agent on the block's model, or ok false when the order does not take it.
type subAgentNext struct {
	sub      SessionAgent
	model    Model
	provider config.ProviderConfig
}

// subAgentFallback asks the order of start (the model the sub-agent was
// started on) where a sub-agent on cur goes after err, and makes the agent
// there. It waits for a refill when the order says so.
func (h *taskHub) subAgentFallback(ctx context.Context, t *Task, start, cur config.SelectedModel, err error) (subAgentNext, bool) {
	c := h.c
	if !isUsageLimit(err) {
		return subAgentNext{}, false
	}
	// A parent that already moved accounts has made this decision for its
	// same-provider workers too. Keep the worker's own model and effort.
	if provider, ok := c.cfg.Config().Providers.Get(cur.Provider); ok {
		account := h.taskAccount(t.SessionID, provider, cur.Account)
		if account != cur.Account {
			return h.subAgentOn(ctx, t, config.FallbackBlock{
				Provider: cur.Provider, Model: cur.Model, Account: account, Effort: cur.ReasoningEffort,
			}, fmt.Sprintf("Following the parent's account: %s.", accounts.Label(c.cfg.Config(), provider.Type, account)))
		}
	}
	cfg := c.cfg.Config()
	if cfg.Options == nil || cfg.Options.Fallback == nil || !cfg.Options.Fallback.On {
		return subAgentNext{}, false
	}
	order := fallback.For(*cfg.Options.Fallback, start.Provider, start.Model)
	at := fallback.Position(order, c.providerKind, cur)
	if at < 0 {
		return subAgentNext{}, false
	}
	from := fallback.BlockAccount(order.Blocks[at], c.providerKind)
	now := c.fallback.clock()
	c.markOut(from, err, now)
	skip, noRefill := map[fallback.Account]bool{}, map[fallback.Account]bool{}
	for range len(order.Blocks) + 2 {
		act := fallback.OnOut(order, c.providerKind, at, c.fallbackReadings(now, skip), now, noRefill)
		switch act.Kind {
		case fallback.Switch:
			b := order.Blocks[act.Block]
			to := fallback.BlockAccount(b, c.providerKind)
			if !c.fallbackReady(ctx, b, to) {
				skip[to] = true
				continue
			}
			return h.subAgentOn(ctx, t, b, fmt.Sprintf("Switched to %s · %s: %s ran out.", accounts.Label(cfg, to.Kind, to.ID), c.modelName(b.Provider, b.Model), accounts.Label(cfg, from.Kind, from.ID)))
		case fallback.Refill:
			outcome, rerr := cliagent.ConsumeRefill(ctx, act.Account.ID)
			if rerr != nil || (outcome != "reset" && outcome != "alreadyRedeemed") {
				noRefill[act.Account] = true
				continue
			}
			c.fallback.mu.Lock()
			delete(c.fallback.out, act.Account)
			c.fallback.mu.Unlock()
			c.saveFallbackMemory()
			return h.subAgentOn(ctx, t, order.Blocks[act.Block], fmt.Sprintf("Used a free refill on %s.", accounts.Label(cfg, act.Account.Kind, act.Account.ID)))
		case fallback.Wait:
			b := order.Blocks[act.Block]
			to := fallback.BlockAccount(b, c.providerKind)
			c.fallbackNotice(ctx, t.ChildID, fmt.Sprintf("%s ran out. Waiting for %s to refill at %s.", accounts.Label(cfg, from.Kind, from.ID), accounts.Label(cfg, to.Kind, to.ID), act.Until.Local().Format("3:04 PM")))
			timer := time.NewTimer(act.Until.Add(limitRetryDelay).Sub(c.fallback.clock()))
			select {
			case <-ctx.Done():
				timer.Stop()
				return subAgentNext{}, false
			case <-timer.C:
			}
			return h.subAgentOn(ctx, t, b, "")
		}
		return subAgentNext{}, false
	}
	return subAgentNext{}, false
}

// subAgentOn makes the agent a sub-agent continues on at block b, notes
// line in its chat and shows the new model on the task.
func (h *taskHub) subAgentOn(ctx context.Context, t *Task, b config.FallbackBlock, line string) (subAgentNext, bool) {
	provider, ok := h.c.cfg.Config().Providers.Get(b.Provider)
	if !ok {
		return subAgentNext{}, false
	}
	i := slices.IndexFunc(provider.Models, func(m catwalk.Model) bool { return m.ID == b.Model })
	if i < 0 {
		return subAgentNext{}, false
	}
	model := provider.Models[i]
	selected := fallback.Model(b)
	if selected.ReasoningEffort == "" {
		selected.ReasoningEffort = model.DefaultReasoningEffort
	}
	sub, m, err := h.subAgent(ctx, provider, model, selected, t.ReadOnly, t.MCP)
	if err != nil {
		slog.Warn("The fallback order could not move a sub-agent", "task", t.ID, "error", err)
		return subAgentNext{}, false
	}
	if line != "" {
		h.c.fallbackNotice(ctx, t.ChildID, line)
	}
	// The worker's chat shows the model it runs on now.
	if choice, err := json.Marshal(selected); err == nil {
		if _, err := h.c.sessions.SetModel(context.WithoutCancel(ctx), t.ChildID, choice); err != nil {
			slog.Warn("Could not save a moved sub-agent's model", "task", t.ID, "error", err)
		}
	}
	h.mu.Lock()
	t.CLI, t.Model, t.Effort = taskProviderName(provider), model.ID, selected.ReasoningEffort
	t.Account = selected.Account
	snapshot := *t
	h.mu.Unlock()
	h.publish(snapshot)
	return subAgentNext{sub: sub, model: m, provider: provider}, true
}
