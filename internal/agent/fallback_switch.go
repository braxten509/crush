package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
	"github.com/charmbracelet/crush/internal/message"
)

// The fallback order (options.fallback, package fallback): before each turn
// and whenever a turn runs out of usage, the coordinator asks the order
// where the chat goes and carries it out: it switches the large model to
// another account or model, waits for an account to refill, or spends a
// Codex account's free refill. Each move adds a line to the chat. Before a
// chat moves to an account, the account is checked quietly (fallbackReady)
// and skipped when it has no room (owner, 2026-10-21).

// fallbackGuessOut is how long an account that ran out without saying
// until when counts as out.
const fallbackGuessOut = 30 * time.Minute

// usageLimitPattern recognizes a usage-limit error that names no reset
// time.
var usageLimitPattern = regexp.MustCompile(`(?i)usage limit|limit reached|hit your limit|out of (extra )?usage|rate.?limit|quota (exceeded|reached)`)

// fallbackOut is an account that ran out: until when, and whether that
// time came from the provider or is a guess.
type fallbackOut struct {
	until time.Time
	known bool
}

// fallbackState is the coordinator's memory of the order between turns.
type fallbackState struct {
	mu  sync.Mutex
	out map[fallback.Account]fallbackOut
	ret *fallback.Return
	// switched tells the apps the selected model changed.
	switched func()
	// now is the clock, send continues a chat and try sends the hidden
	// request that tests an account (cliagent.TryAccount); tests replace
	// them.
	now  func() time.Time
	send func(sessionID string)
	try  func(ctx context.Context, kind catwalk.Type, account, model string) error
}

func (s *fallbackState) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// SetModelSwitchHook installs fn, run after the fallback order switched
// the selected model, so the apps refresh it.
func (c *coordinator) SetModelSwitchHook(fn func()) {
	c.fallback.mu.Lock()
	defer c.fallback.mu.Unlock()
	c.fallback.switched = fn
}

// fallbackOrder is the order a chat on cur follows (its picked model's
// list, else every other model's) when the order is on.
func (c *coordinator) fallbackOrder(cur config.SelectedModel) (config.FallbackOrder, bool) {
	cfg := c.cfg.Config()
	if cfg.Options == nil || cfg.Options.Fallback == nil || !cfg.Options.Fallback.On {
		return config.FallbackOrder{}, false
	}
	provider, model := fallback.Chosen(cur)
	return fallback.For(*cfg.Options.Fallback, provider, model), true
}

func (c *coordinator) providerKind(provider string) catwalk.Type {
	p, ok := c.cfg.Config().Providers.Get(provider)
	if !ok {
		return ""
	}
	return p.Type
}

// fallbackReadings reads accounts from the usage store, with the accounts
// that ran out, and those this decision skipped, counted as full.
func (c *coordinator) fallbackReadings(now time.Time, skip map[fallback.Account]bool) fallback.Readings {
	c.fallback.mu.Lock()
	out := make(map[fallback.Account]fallbackOut, len(c.fallback.out))
	for a, o := range c.fallback.out {
		if now.Before(o.until) {
			out[a] = o
		}
	}
	c.fallback.mu.Unlock()
	return func(a fallback.Account) fallback.Reading {
		limits := cliagent.AccountLimits(a.Kind, a.ID, "")
		r := fallback.Reading{Known: len(limits) > 0, Refills: cliagent.Refills(a.Kind, a.ID)}
		for _, l := range limits {
			r.Used = max(r.Used, 100-l.Left())
			if l.Left() <= 0 && l.ResetsAt.After(r.OpenAt) {
				r.OpenAt = l.ResetsAt
			}
		}
		if o, ok := out[a]; ok {
			r.Known, r.Used = true, 100
			if o.known && o.until.After(r.OpenAt) {
				r.OpenAt = o.until
			}
		}
		if skip[a] {
			r.Known, r.Used = true, 100
		}
		return r
	}
}

// fallbackReadyTimeout bounds the fresh usage reading of fallbackReady.
const fallbackReadyTimeout = 25 * time.Second

// fallbackReady reports whether acct has room for block b right now, asked
// just before a chat moves there (owner, 2026-10-21: "ping it ... and skip
// it if it doesn't"). An added account that is not signed in has none. The
// account's usage is read fresh, the same free reading the usage bars use;
// when it can't be read, one tiny hidden request on the account says
// whether it answers ("try it silently not in the chat"). Nothing of this
// shows in any chat. A hidden request that hits a usage limit counts the
// account as out, as a turn that ran out would.
func (c *coordinator) fallbackReady(ctx context.Context, b config.FallbackBlock, acct fallback.Account) bool {
	if !accounts.SignedIn(acct.Kind, acct.ID) {
		return false
	}
	readCtx, cancel := context.WithTimeout(ctx, fallbackReadyTimeout)
	err := cliagent.ReadAccountLimitsNow(readCtx, acct.Kind, acct.ID)
	cancel()
	now := c.fallback.clock()
	switch {
	case err == nil:
		return !fallback.Full(c.fallbackReadings(now, nil)(acct), b.MoveAtPercent)
	case errors.Is(err, cliagent.ErrNoUsageSource):
		return true // nothing to read: taken as it is
	}
	c.fallback.mu.Lock()
	try := c.fallback.try
	c.fallback.mu.Unlock()
	if try == nil {
		try = cliagent.TryAccount
	}
	tryErr := try(ctx, acct.Kind, acct.ID, b.Model)
	if tryErr == nil {
		return true
	}
	slog.Info("The fallback order skips an account that did not answer", "kind", acct.Kind, "account", acct.ID, "reading", err, "try", tryErr)
	if isUsageLimit(tryErr) {
		c.markOut(acct, tryErr, now)
	}
	return false
}

// markOut counts acct as out until the reset time err names, else for
// fallbackGuessOut.
func (c *coordinator) markOut(acct fallback.Account, err error, now time.Time) {
	o := fallbackOut{until: now.Add(fallbackGuessOut)}
	if reset, ok := resetTime(err.Error(), now); ok && reset.After(now) {
		o = fallbackOut{until: reset, known: true}
	}
	c.fallback.mu.Lock()
	if c.fallback.out == nil {
		c.fallback.out = map[fallback.Account]fallbackOut{}
	}
	c.fallback.out[acct] = o
	c.fallback.mu.Unlock()
}

// redecide asks the order again once the account it picked could not take
// the chat: as after running out when resume is set, else as before a turn.
func (c *coordinator) redecide(order config.FallbackOrder, at int, resume bool, noRefill, skip map[fallback.Account]bool) fallback.Action {
	now := c.fallback.clock()
	read := c.fallbackReadings(now, skip)
	if resume {
		return fallback.OnOut(order, c.providerKind, at, read, now, noRefill)
	}
	c.fallback.mu.Lock()
	ret := c.fallback.ret
	c.fallback.mu.Unlock()
	return fallback.BeforeTurn(order, c.providerKind, at, ret, read, now)
}

// isUsageLimit reports whether a turn failed because its account ran out.
func isUsageLimit(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if _, ok := resetTime(err.Error(), time.Now()); ok {
		return true
	}
	return usageLimitPattern.MatchString(err.Error())
}

// fallbackHolds reports whether the fallback order steers a chat on this
// model, so the order, not the plain reset retry, decides what follows a
// usage limit.
func fallbackHolds(store *config.ConfigStore, m config.SelectedModel) bool {
	if store == nil {
		return false
	}
	cfg := store.Config()
	if cfg == nil || cfg.Options == nil || cfg.Options.Fallback == nil {
		return false
	}
	kindOf := func(provider string) catwalk.Type {
		p, ok := cfg.Providers.Get(provider)
		if !ok {
			return ""
		}
		return p.Type
	}
	provider, model := fallback.Chosen(m)
	return fallback.Position(fallback.For(*cfg.Options.Fallback, provider, model), kindOf, m) >= 0
}

// fallbackBeforeTurn moves the chat first when the order says so: back to
// an account that refilled, or on from one past its block's own limit.
func (c *coordinator) fallbackBeforeTurn(ctx context.Context, sessionID string) {
	cur := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	order, ok := c.fallbackOrder(cur)
	if !ok || !c.interactiveTurn(ctx) {
		return
	}
	at := fallback.Position(order, c.providerKind, cur)
	if at < 0 {
		return
	}
	now := c.fallback.clock()
	c.fallback.mu.Lock()
	ret := c.fallback.ret
	c.fallback.mu.Unlock()
	act := fallback.BeforeTurn(order, c.providerKind, at, ret, c.fallbackReadings(now, nil), now)
	c.applyFallback(ctx, sessionID, order, at, act, false, nil, map[fallback.Account]bool{})
}

// fallbackOnOut handles a turn that ran out of usage on a model the order
// steers. It reports whether the order took the chat; when it did not, the
// plain reset retry applies.
func (c *coordinator) fallbackOnOut(ctx context.Context, sessionID string, err error) bool {
	if !c.interactiveTurn(ctx) || !isUsageLimit(err) {
		return false
	}
	cur := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	order, ok := c.fallbackOrder(cur)
	if !ok {
		return false
	}
	at := fallback.Position(order, c.providerKind, cur)
	if at < 0 {
		return false
	}
	now := c.fallback.clock()
	c.markOut(fallback.BlockAccount(order.Blocks[at], c.providerKind), err, now)
	act := fallback.OnOut(order, c.providerKind, at, c.fallbackReadings(now, nil), now, nil)
	return c.applyFallback(ctx, sessionID, order, at, act, true, map[fallback.Account]bool{}, map[fallback.Account]bool{})
}

// applyFallback carries out a decision for a chat at block `at`. resume
// continues the chat afterwards (its turn was cut short). noRefill and skip
// are this decision's accounts whose refill failed and that could not take
// the chat. It reports whether the chat was taken care of.
func (c *coordinator) applyFallback(ctx context.Context, sessionID string, order config.FallbackOrder, at int, act fallback.Action, resume bool, noRefill, skip map[fallback.Account]bool) bool {
	cfg := c.cfg.Config()
	now := c.fallback.clock()
	from := fallback.BlockAccount(order.Blocks[at], c.providerKind)
	why := "it ran out"
	if !resume {
		why = "by the fallback order"
	}
	switch act.Kind {
	case fallback.Switch:
		b := order.Blocks[act.Block]
		to := fallback.BlockAccount(b, c.providerKind)
		if !c.fallbackReady(ctx, b, to) {
			skip[to] = true
			return c.applyFallback(ctx, sessionID, order, at, c.redecide(order, at, resume, noRefill, skip), resume, noRefill, skip)
		}
		c.fallback.mu.Lock()
		switch {
		case act.Return != nil:
			c.fallback.ret = act.Return
		case c.fallback.ret != nil && c.fallback.ret.Block == act.Block:
			c.fallback.ret = nil
		}
		c.fallback.mu.Unlock()
		if err := c.switchModel(fallback.Model(b)); err != nil {
			slog.Warn("The fallback order could not switch the model", "error", err)
			return false
		}
		line := fmt.Sprintf("Switched to %s · %s: %s %s.", accounts.Label(cfg, to.Kind, to.ID), c.modelName(b.Provider, b.Model), accounts.Label(cfg, from.Kind, from.ID), why)
		if !resume {
			line = fmt.Sprintf("Switched to %s · %s by the fallback order.", accounts.Label(cfg, to.Kind, to.ID), c.modelName(b.Provider, b.Model))
		}
		if act.Return != nil {
			line += fmt.Sprintf(" Back to %s once it refills at %s.", accounts.Label(cfg, from.Kind, from.ID), act.Return.At.Local().Format("3:04 PM"))
		}
		c.fallbackNotice(ctx, sessionID, line)
		if resume {
			c.continueChat(sessionID, now)
		}
		return true
	case fallback.Wait:
		b := order.Blocks[act.Block]
		to := fallback.BlockAccount(b, c.providerKind)
		c.fallbackNotice(ctx, sessionID, fmt.Sprintf("%s %s. Waiting for %s to refill at %s.", accounts.Label(cfg, from.Kind, from.ID), why, accounts.Label(cfg, to.Kind, to.ID), act.Until.Local().Format("3:04 PM")))
		c.limitRetries.plan(sessionID, act.Until.Add(limitRetryDelay), func() {
			// Still no room at the refill time: the order decides again,
			// quietly; with nowhere else to go the chat tries it anyway.
			if ctx := context.Background(); !c.fallbackReady(ctx, b, to) {
				skip := map[fallback.Account]bool{to: true}
				if next := c.redecide(order, at, true, nil, skip); next.Kind != fallback.Stay && c.applyFallback(ctx, sessionID, order, at, next, true, nil, skip) {
					return
				}
			}
			if err := c.switchModel(fallback.Model(b)); err != nil {
				slog.Warn("The fallback order could not switch the model", "error", err)
			}
			c.sendContinue(sessionID)
		})
		return true
	case fallback.Refill:
		if act.Account.Kind != config.TypeCodexCLI {
			return false
		}
		outcome, err := cliagent.ConsumeRefill(ctx, act.Account.ID)
		if err != nil || (outcome != "reset" && outcome != "alreadyRedeemed") {
			slog.Info("A free refill was not used", "account", act.Account.ID, "outcome", outcome, "error", err)
			if noRefill == nil {
				noRefill = map[fallback.Account]bool{}
			}
			noRefill[act.Account] = true
			next := fallback.OnOut(order, c.providerKind, at, c.fallbackReadings(now, skip), now, noRefill)
			return c.applyFallback(ctx, sessionID, order, at, next, resume, noRefill, skip)
		}
		c.fallback.mu.Lock()
		delete(c.fallback.out, act.Account)
		c.fallback.mu.Unlock()
		line := fmt.Sprintf("Used a free refill on %s.", accounts.Label(cfg, act.Account.Kind, act.Account.ID))
		if act.Block != at {
			b := order.Blocks[act.Block]
			if err := c.switchModel(fallback.Model(b)); err != nil {
				return false
			}
			line = fmt.Sprintf("Used a free refill on %s and switched to it.", accounts.Label(cfg, act.Account.Kind, act.Account.ID))
		}
		c.fallbackNotice(ctx, sessionID, line)
		if resume {
			c.continueChat(sessionID, now)
		}
		return true
	}
	return false
}

// switchModel makes m the selected large model and tells the apps.
func (c *coordinator) switchModel(m config.SelectedModel) error {
	cur := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	if cur.Provider == m.Provider && cur.Model == m.Model && cur.Account == m.Account && (m.ReasoningEffort == "" || cur.ReasoningEffort == m.ReasoningEffort) {
		return nil
	}
	next := cur
	// The picked model stays the chat's start, so its list still applies.
	next.ChosenProvider, next.ChosenModel = fallback.Chosen(cur)
	next.Provider, next.Model, next.Account = m.Provider, m.Model, m.Account
	if next.ChosenProvider == next.Provider && next.ChosenModel == next.Model {
		next.ChosenProvider, next.ChosenModel = "", ""
	}
	if m.ReasoningEffort != "" {
		next.ReasoningEffort = m.ReasoningEffort
	}
	if err := c.cfg.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, next); err != nil {
		return err
	}
	c.fallback.mu.Lock()
	switched := c.fallback.switched
	c.fallback.mu.Unlock()
	if switched != nil {
		switched()
	}
	return nil
}

func (c *coordinator) modelName(provider, model string) string {
	if m := c.cfg.Config().GetModel(provider, model); m != nil && m.Name != "" {
		return m.Name
	}
	return model
}

// fallbackNotice adds a line to the chat that never reaches the model.
func (c *coordinator) fallbackNotice(ctx context.Context, sessionID, text string) {
	if c.messages == nil {
		return
	}
	_, _ = c.messages.Create(context.WithoutCancel(ctx), sessionID, message.CreateMessageParams{
		Role:  message.System,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
}

// continueChat sends the chat "continue" once its cut-short turn is over.
func (c *coordinator) continueChat(sessionID string, now time.Time) {
	c.limitRetries.plan(sessionID, now, func() { c.sendContinue(sessionID) })
}

func (c *coordinator) sendContinue(sessionID string) {
	c.fallback.mu.Lock()
	send := c.fallback.send
	c.fallback.mu.Unlock()
	if send != nil {
		send(sessionID)
		return
	}
	if c.tasks != nil && c.tasks.isClosing() {
		return
	}
	if _, err := c.Run(context.Background(), sessionID, limitRetryPrompt); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("Continuing a chat after a fallback move failed", "session_id", sessionID, "error", err)
	}
}
