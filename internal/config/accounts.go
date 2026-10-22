package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

// Account is one more sign-in of Claude Code or Codex, kept in a folder of
// its own (AccountDir). Each tool's own folder (~/.claude, ~/.codex) is
// account 1 and is never listed.
type Account struct {
	ID   string       `json:"id" jsonschema:"description=Stable ID; also the account folder's name"`
	Kind catwalk.Type `json:"kind" jsonschema:"description=The tool signed in,enum=claude-code,enum=codex-cli"`
	Name string       `json:"name,omitempty" jsonschema:"description=Shown name; defaults to the tool and the account's number"`
}

// FallbackBlockType is what one block of the fallback order does.
type FallbackBlockType string

const (
	// FallbackUse runs chats on an account and model.
	FallbackUse FallbackBlockType = "use"
	// FallbackWait waits for an account to refill.
	FallbackWait FallbackBlockType = "wait"
	// FallbackRefill spends one of a Codex account's free refills.
	FallbackRefill FallbackBlockType = "refill"
)

// FallbackMeanwhileNext makes a wait block use the blocks after it until
// the account it waits for refills.
const FallbackMeanwhileNext = "next"

// FallbackBlock is one step of the fallback order.
type FallbackBlock struct {
	Type FallbackBlockType `json:"type" jsonschema:"enum=use,enum=wait,enum=refill"`
	// Provider, Model and Effort pick a use block's model.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	// Kind is the tool a wait or refill block's account belongs to; a use
	// block's tool is its provider's.
	Kind catwalk.Type `json:"kind,omitempty"`
	// Account is the block's sign-in: an options.accounts ID, or empty for
	// the tool's own login.
	Account string `json:"account,omitempty"`
	// MoveAtPercent moves a use block on once this much of a window is
	// used; 0 waits until it runs out.
	MoveAtPercent float64 `json:"move_at_percent,omitempty" jsonschema:"minimum=0,maximum=100"`
	// RefillFirst spends a free refill (Codex) before moving on.
	RefillFirst bool `json:"refill_first,omitempty"`
	// ComeBack returns to this use block once its account refills.
	ComeBack bool `json:"come_back,omitempty"`
	// MaxWaitMinutes skips a wait block whose refill is further away; 0
	// waits however long it takes.
	MaxWaitMinutes int `json:"max_wait_minutes,omitempty" jsonschema:"minimum=0"`
	// Meanwhile is "next" to use the blocks after a wait block until its
	// account refills, else the chat just waits.
	Meanwhile string `json:"meanwhile,omitempty" jsonschema:"enum=,enum=next"`
}

// FallbackOrder is the order chats move through when an account runs out.
type FallbackOrder struct {
	On bool `json:"on"`
	// Blocks is the order of chats whose model has no list of its own.
	Blocks []FallbackBlock `json:"blocks,omitempty"`
	// Lists are the orders of chats that started on one model, effort
	// aside (owner, 2026-10-21).
	Lists []FallbackList `json:"lists,omitempty"`
}

// FallbackList is the fallback order of the chats and sub-agents that
// started on one model: the model the user picked, or the one a sub-agent
// was started on. The order's own switches keep that start.
type FallbackList struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Blocks   []FallbackBlock `json:"blocks,omitempty"`
}

// AccountKinds are the tools that can have more than one account.
var AccountKinds = []catwalk.Type{TypeClaudeCode, TypeCodexCLI}

// HasAccounts reports whether kind can have more than one account.
func HasAccounts(kind catwalk.Type) bool { return slices.Contains(AccountKinds, kind) }

// AccountsDir holds every extra account's folder.
func AccountsDir() string {
	return filepath.Join(filepath.Dir(GlobalConfigData()), "accounts")
}

// AccountDir is an account's folder, or "" for account 1.
func AccountDir(id string) string {
	if id == "" {
		return ""
	}
	return filepath.Join(AccountsDir(), id)
}

// AccountEnv points kind's CLI at an account's folder; nil for account 1.
func AccountEnv(kind catwalk.Type, id string) []string {
	dir := AccountDir(id)
	if dir == "" {
		return nil
	}
	switch kind {
	case TypeClaudeCode:
		return []string{"CLAUDE_CONFIG_DIR=" + dir}
	case TypeCodexCLI:
		return []string{"CODEX_HOME=" + dir}
	}
	return nil
}

// AccountKindName is the short name accounts of kind go by.
func AccountKindName(kind catwalk.Type) string {
	switch kind {
	case TypeClaudeCode:
		return "Claude"
	case TypeCodexCLI:
		return "Codex"
	}
	return string(kind)
}

// AccountGivenName is the name an account was given, or "": a listed
// account's own, the tool's own login's in options.own_account_names. The
// shown name falls back to the account's email, then to its number
// (accounts.Name).
func (c *Config) AccountGivenName(kind catwalk.Type, id string) string {
	if c.Options == nil {
		return ""
	}
	if id == "" {
		return strings.TrimSpace(c.Options.OwnAccountNames[string(kind)])
	}
	for _, a := range c.Options.Accounts {
		if a.Kind == kind && a.ID == id {
			return strings.TrimSpace(a.Name)
		}
	}
	return ""
}

// AccountNumberName is an account's tool and number: "Claude 1" for the
// tool's own login, "Claude 2" for its first added account.
func (c *Config) AccountNumberName(kind catwalk.Type, id string) string {
	if id == "" {
		return AccountKindName(kind) + " 1"
	}
	n := 1
	if c.Options != nil {
		for _, a := range c.Options.Accounts {
			if a.Kind != kind {
				continue
			}
			n++
			if a.ID == id {
				return AccountKindName(kind) + " " + strconv.Itoa(n)
			}
		}
	}
	return fmt.Sprintf("%s (%s)", AccountKindName(kind), id)
}

// Accounts lists kind's extra accounts in order.
func (c *Config) Accounts(kind catwalk.Type) []Account {
	if c.Options == nil {
		return nil
	}
	var out []Account
	for _, a := range c.Options.Accounts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// HasAccount reports whether id is account 1 ("") or a listed account of kind.
func (c *Config) HasAccount(kind catwalk.Type, id string) bool {
	return id == "" || slices.ContainsFunc(c.Accounts(kind), func(a Account) bool { return a.ID == id })
}
