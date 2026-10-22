// Package fallback decides where chats go when an account runs out, by the
// user's fallback order (options.fallback): use blocks run chats on an
// account and model, wait blocks wait for an account to refill, refill
// blocks spend a Codex account's free refill. It only decides; the agent's
// coordinator carries the decisions out.
package fallback

import (
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
)

// Account is one sign-in: a tool and an options.accounts ID ("" for the
// tool's own login).
type Account struct {
	Kind catwalk.Type
	ID   string
}

// Reading is what is known of an account's usage now.
type Reading struct {
	// Known is false before any reading: the account counts as having room.
	Known bool
	// Used is the fullest window's percent used, 0-100.
	Used float64
	// OpenAt is when every full window has reset; zero when none is full
	// or the time is unknown.
	OpenAt time.Time
	// Refills is how many free refills the account has (Codex).
	Refills int
}

// Readings reads an account's usage.
type Readings func(Account) Reading

// Kind is what an Action does.
type Kind int

const (
	// Stay keeps the chat where it is.
	Stay Kind = iota
	// Switch moves the chat to Block.
	Switch
	// Wait pauses the chat until Until, then moves it to Block.
	Wait
	// Refill spends one of Account's free refills, then moves the chat
	// to Block.
	Refill
)

// Action is a decision.
type Action struct {
	Kind    Kind
	Block   int
	Until   time.Time
	Account Account
	// Return is set when a wait block sent the chat on meanwhile: the chat
	// comes back to Return.Block once Return.At has come and its account
	// has room.
	Return *Return
}

// Return is a planned way back to a block whose account is refilling.
type Return struct {
	Block int
	At    time.Time
}

// KindOf names the tool behind a provider ID.
type KindOf func(provider string) catwalk.Type

// BlockAccount is the account a block runs on or watches.
func BlockAccount(b config.FallbackBlock, kindOf KindOf) Account {
	kind := b.Kind
	if b.Type == config.FallbackUse {
		kind = kindOf(b.Provider)
	}
	return Account{Kind: kind, ID: b.Account}
}

// Chosen is the model a chat started on: the one the user picked, which
// the order's own switches keep (config.SelectedModel.ChosenModel).
func Chosen(m config.SelectedModel) (provider, model string) {
	if m.ChosenModel != "" {
		return m.ChosenProvider, m.ChosenModel
	}
	return m.Provider, m.Model
}

// For is the order a chat that started on provider and model follows: that
// model's own list, else the list of every other model.
func For(order config.FallbackOrder, provider, model string) config.FallbackOrder {
	out := config.FallbackOrder{On: order.On, Blocks: order.Blocks}
	for _, l := range order.Lists {
		if l.Provider == provider && l.Model == model {
			out.Blocks = l.Blocks
			break
		}
	}
	return out
}

// Position is the use block a chat on this model is at: the first use
// block with the same provider, model and account, else the first with the
// same tool and account; -1 when the order does not hold the chat.
func Position(order config.FallbackOrder, kindOf KindOf, m config.SelectedModel) int {
	if !order.On {
		return -1
	}
	kind := kindOf(m.Provider)
	loose := -1
	for i, b := range order.Blocks {
		if b.Type != config.FallbackUse || b.Account != m.Account {
			continue
		}
		if b.Provider == m.Provider && b.Model == m.Model {
			return i
		}
		if loose < 0 && kind != "" && kindOf(b.Provider) == kind {
			loose = i
		}
	}
	return loose
}

// Full reports whether an account has no room left for a block that moves
// on at moveAt percent (0: only when it runs out).
func Full(r Reading, moveAt float64) bool {
	if !r.Known {
		return false
	}
	if r.Used >= 100 {
		return true
	}
	return moveAt > 0 && r.Used >= moveAt
}

// useBlockFor is the first use block on acct, or -1.
func useBlockFor(order config.FallbackOrder, kindOf KindOf, acct Account) int {
	for i, b := range order.Blocks {
		if b.Type == config.FallbackUse && BlockAccount(b, kindOf) == acct {
			return i
		}
	}
	return -1
}

// OnOut decides where a chat at block `at` goes once its account ran out
// (or passed the block's own limit). noRefill lists accounts whose refill
// already failed this time, so they are not tried again.
func OnOut(order config.FallbackOrder, kindOf KindOf, at int, read Readings, now time.Time, noRefill map[Account]bool) Action {
	if at < 0 || at >= len(order.Blocks) {
		return Action{Kind: Stay}
	}
	if b := order.Blocks[at]; b.Type == config.FallbackUse && b.RefillFirst {
		acct := BlockAccount(b, kindOf)
		if !noRefill[acct] && read(acct).Refills > 0 {
			return Action{Kind: Refill, Account: acct, Block: at}
		}
	}
	var ret *Return
	for j := at + 1; j < len(order.Blocks); j++ {
		b := order.Blocks[j]
		acct := BlockAccount(b, kindOf)
		switch b.Type {
		case config.FallbackUse:
			if !Full(read(acct), b.MoveAtPercent) {
				return Action{Kind: Switch, Block: j, Return: ret}
			}
		case config.FallbackRefill:
			target := useBlockFor(order, kindOf, acct)
			if target >= 0 && !noRefill[acct] && read(acct).Refills > 0 {
				return Action{Kind: Refill, Account: acct, Block: target, Return: ret}
			}
		case config.FallbackWait:
			target := useBlockFor(order, kindOf, acct)
			if target < 0 || ret != nil {
				continue // nothing to wait for, or already waiting for another
			}
			r := read(acct)
			if !Full(r, 0) {
				return Action{Kind: Switch, Block: target}
			}
			if r.OpenAt.IsZero() {
				continue // no known refill time to wait for
			}
			if b.MaxWaitMinutes > 0 && r.OpenAt.Sub(now) > time.Duration(b.MaxWaitMinutes)*time.Minute {
				continue
			}
			if b.Meanwhile == config.FallbackMeanwhileNext {
				ret = &Return{Block: target, At: r.OpenAt}
				continue
			}
			return Action{Kind: Wait, Block: target, Until: r.OpenAt}
		}
	}
	if ret != nil {
		// Nothing after the wait block has room: wait after all.
		return Action{Kind: Wait, Block: ret.Block, Until: ret.At}
	}
	return soonestComeBack(order, kindOf, at, read)
}

// soonestComeBack is the wait for the earlier "come back" block whose
// account refills first, once nothing has room (owner, 2026-10-22). When
// the chat's own account refills sooner, or no time is known, it stays:
// the plain reset retry continues it there.
func soonestComeBack(order config.FallbackOrder, kindOf KindOf, at int, read Readings) Action {
	act := Action{Kind: Stay}
	if own := read(BlockAccount(order.Blocks[at], kindOf)); !own.OpenAt.IsZero() {
		act.Until = own.OpenAt
	}
	for k := range at {
		b := order.Blocks[k]
		if b.Type != config.FallbackUse || !b.ComeBack {
			continue
		}
		r := read(BlockAccount(b, kindOf))
		if r.OpenAt.IsZero() || (!act.Until.IsZero() && !r.OpenAt.Before(act.Until)) {
			continue
		}
		act = Action{Kind: Wait, Block: k, Until: r.OpenAt}
	}
	if act.Kind == Stay {
		return Action{Kind: Stay}
	}
	return act
}

// BeforeTurn decides, as a turn starts on block `at`, whether the chat
// should move first: back along a planned return, back to an earlier block
// marked "come back" whose account has room again, or on from a block whose
// own limit has passed.
func BeforeTurn(order config.FallbackOrder, kindOf KindOf, at int, ret *Return, read Readings, now time.Time) Action {
	if at < 0 || at >= len(order.Blocks) {
		return Action{Kind: Stay}
	}
	if ret != nil && ret.Block != at && ret.Block >= 0 && ret.Block < len(order.Blocks) && !now.Before(ret.At) {
		b := order.Blocks[ret.Block]
		if !Full(read(BlockAccount(b, kindOf)), b.MoveAtPercent) {
			return Action{Kind: Switch, Block: ret.Block}
		}
	}
	for k := range at {
		b := order.Blocks[k]
		if b.Type == config.FallbackUse && b.ComeBack && !Full(read(BlockAccount(b, kindOf)), b.MoveAtPercent) {
			return Action{Kind: Switch, Block: k}
		}
	}
	b := order.Blocks[at]
	if b.Type == config.FallbackUse && b.MoveAtPercent > 0 && Full(read(BlockAccount(b, kindOf)), b.MoveAtPercent) {
		if act := OnOut(order, kindOf, at, read, now, nil); act.Kind == Switch || act.Kind == Refill {
			return act
		}
	}
	return Action{Kind: Stay}
}

// Model is the model a use block runs.
func Model(b config.FallbackBlock) config.SelectedModel {
	return config.SelectedModel{Provider: b.Provider, Model: b.Model, ReasoningEffort: b.Effort, Account: b.Account}
}
