package proto

import "github.com/charmbracelet/crush/internal/config"

// AccountInfo is one Claude Code, Codex or Grok sign-in. ID "" is the tool's
// own login (account 1). Name is the name it was given, else its email,
// else its tool and number; Label is Name with the tool added where
// another account has the same name (accounts.Shown).
type AccountInfo struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	SignedIn bool   `json:"signed_in"`
	// Email is the email it signed in with, "" when none can be read
	// (accounts.LoginEmail): the panel's hover text.
	Email string `json:"email,omitempty"`
}

// FallbackSettings is the accounts and the fallback order.
type FallbackSettings struct {
	Accounts []AccountInfo        `json:"accounts"`
	Order    config.FallbackOrder `json:"order"`
}

// AccountAddRequest adds an account of a tool ("claude-code", "codex-cli",
// "grok-cli") or of an API provider (its provider id, for another entry).
type AccountAddRequest struct {
	Kind string `json:"kind"`
}

// AccountLogin is the command that signs an account in, for a terminal.
type AccountLogin struct {
	Account   AccountInfo `json:"account"`
	LoginArgv []string    `json:"login_argv"`
}

// AccountRenameRequest renames an account; empty goes back to its email,
// else its number.
type AccountRenameRequest struct {
	Name string `json:"name"`
}

// RefillRequest spends one of a Codex account's free refills.
type RefillRequest struct {
	Kind    string `json:"kind"`
	Account string `json:"account"`
}

// RefillResult is what Codex says happened: "reset", "nothingToReset",
// "noCredit" or "alreadyRedeemed".
type RefillResult struct {
	Outcome string `json:"outcome"`
}
