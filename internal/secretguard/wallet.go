package secretguard

// KDE Wallet reads (owner, 2026-11-24): secure entry can save a secret into
// the user's wallet, and helper programs read it from there. An agent's own
// tool call must not read a wallet entry, through kwallet-query's read flag
// or the wallet's D-Bus read calls. Like the folder rule, this stops
// accidental or prompted reads; it is not a sandbox.

import (
	"encoding/json"
	"regexp"
	"strings"
)

// WalletReason is what the agent sees when a wallet read is blocked.
const WalletReason = "Blocked: this would read a KDE Wallet entry. Use a helper program that reads the secret itself and never prints it; never read secrets through agent tools."

var (
	walletQuery = regexp.MustCompile(`(?i)kwallet-query`)
	// kwallet-query's read flag, alone or in a cluster like -vr.
	walletReadFlag = regexp.MustCompile(`(^|\s)(-[a-qs-z]*r[a-z]*|--read-password)(\s|=|$)`)
	walletBus      = regexp.MustCompile(`(?i)kwallet`)
	walletBusRead  = regexp.MustCompile(`(?i)read(password|entry|map)`)
)

// BlocksWalletRead reports whether a tool call with this JSON input would
// read a KDE Wallet entry.
func BlocksWalletRead(tool string, input []byte) bool {
	if writesOnly(tool) {
		return false
	}
	var value any
	if json.Unmarshal(input, &value) != nil {
		value = string(input)
	}
	text := strings.Join(stringsIn(value, nil), "\n")
	if walletQuery.MatchString(text) && walletReadFlag.MatchString(text) {
		return true
	}
	return walletBus.MatchString(text) && walletBusRead.MatchString(text)
}
