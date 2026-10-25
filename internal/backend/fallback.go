package backend

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// Accounts (config.Account) and the fallback order (config.FallbackOrder).
// Both live in the global config, so every Crush follows them.

var (
	// ErrAccountNotFound is an account ID that is not in options.accounts.
	ErrAccountNotFound = errors.New("account not found")
	// ErrFallbackInvalid is a fallback order or account request that can't
	// be saved; its text says why.
	ErrFallbackInvalid = errors.New("invalid fallback settings")
)

func invalidFallback(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFallbackInvalid, fmt.Sprintf(format, args...))
}

// FallbackSettings lists every account (each tool's own login first) and
// the order.
func (b *Backend) FallbackSettings(workspaceID string) (proto.FallbackSettings, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	return fallbackSettings(ws.Cfg.Config()), nil
}

func fallbackSettings(cfg *config.Config) proto.FallbackSettings {
	out := proto.FallbackSettings{Accounts: []proto.AccountInfo{}}
	for _, a := range accounts.Every(cfg) {
		out.Accounts = append(out.Accounts, proto.AccountInfo{ID: a.ID, Kind: string(a.Kind), Name: a.Name, Label: a.Label, SignedIn: accounts.SignedIn(a.Kind, a.ID)})
	}
	if cfg.Options != nil && cfg.Options.Fallback != nil {
		out.Order = *cfg.Options.Fallback
	}
	if out.Order.Blocks == nil {
		out.Order.Blocks = []config.FallbackBlock{}
	}
	return out
}

// freshConfig is ws's config with the edit just written: an edit's own
// reload is skipped while another reload runs (the folder's config watcher
// reloads after every write; config.autoReload), and the answer would then
// be the settings from before the edit.
func freshConfig(ws *Workspace) *config.Config {
	if err := ws.Cfg.ReloadFromDisk(context.Background()); err != nil {
		slog.Warn("Failed to reload the config after an account edit", "error", err)
	}
	return ws.Cfg.Config()
}

// SetFallbackOrder checks and saves the order.
func (b *Backend) SetFallbackOrder(workspaceID string, order config.FallbackOrder) (proto.FallbackSettings, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg := ws.Cfg.Config()
	if err := checkFallbackOrder(cfg, order); err != nil {
		return proto.FallbackSettings{}, err
	}
	if err := ws.Cfg.SetConfigField(config.ScopeGlobal, "options.fallback", order); err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg = freshConfig(ws)
	sendConfigChanged(ws)
	return fallbackSettings(cfg), nil
}

func checkFallbackOrder(cfg *config.Config, order config.FallbackOrder) error {
	if err := checkFallbackBlocks(cfg, order.Blocks, ""); err != nil {
		return err
	}
	seen := map[[2]string]bool{}
	for _, l := range order.Lists {
		p, ok := cfg.Providers.Get(l.Provider)
		if !ok || l.Model == "" {
			return invalidFallback("a list for an unknown model %q of %q", l.Model, l.Provider)
		}
		if seen[[2]string{l.Provider, l.Model}] {
			return invalidFallback("%s has two lists", l.Model)
		}
		seen[[2]string{l.Provider, l.Model}] = true
		if err := checkFallbackBlocks(cfg, l.Blocks, fmt.Sprintf("%s %s list, ", p.Name, l.Model)); err != nil {
			return err
		}
	}
	return nil
}

// checkFallbackBlocks checks one list's blocks; where names the list in
// errors ("" for every other model's).
func checkFallbackBlocks(cfg *config.Config, blocks []config.FallbackBlock, where string) error {
	invalidFallback := func(format string, args ...any) error {
		return invalidFallback(where+format, args...)
	}
	for i, blk := range blocks {
		n := i + 1
		kind := blk.Kind
		switch blk.Type {
		case config.FallbackUse:
			p, ok := cfg.Providers.Get(blk.Provider)
			if !ok {
				return invalidFallback("block %d: unknown provider %q", n, blk.Provider)
			}
			if blk.Model == "" {
				return invalidFallback("block %d: pick a model", n)
			}
			kind = p.Type
			if blk.Account != "" && !config.HasAccounts(kind) {
				return invalidFallback("block %d: %s has no other accounts", n, p.Name)
			}
		case config.FallbackWait, config.FallbackRefill:
			if !config.HasAccounts(kind) {
				return invalidFallback("block %d: pick a Claude or Codex account", n)
			}
			if blk.Type == config.FallbackRefill && kind != config.TypeCodexCLI {
				return invalidFallback("block %d: only Codex has free refills", n)
			}
		default:
			return invalidFallback("block %d: unknown block type %q", n, blk.Type)
		}
		if !cfg.HasAccount(kind, blk.Account) {
			return invalidFallback("block %d: unknown account %q", n, blk.Account)
		}
		if blk.MoveAtPercent < 0 || blk.MoveAtPercent > 100 || blk.MaxWaitMinutes < 0 {
			return invalidFallback("block %d: limits must be 0-100%% and a wait of 0 or more minutes", n)
		}
		if blk.Meanwhile != "" && blk.Meanwhile != config.FallbackMeanwhileNext {
			return invalidFallback("block %d: unknown meanwhile %q", n, blk.Meanwhile)
		}
	}
	return nil
}

// AddAccount makes a new account of kind and its folder, and returns the
// command that signs it in. For an API provider it makes another provider
// entry instead, a copy without its key (addProviderAccount).
func (b *Backend) AddAccount(workspaceID, kind string) (proto.AccountLogin, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AccountLogin{}, err
	}
	k := catwalk.Type(kind)
	if !config.HasAccounts(k) {
		return b.addProviderAccount(ws, kind)
	}
	cfg := ws.Cfg.Config()
	var list []config.Account
	if cfg.Options != nil {
		list = slices.Clone(cfg.Options.Accounts)
	}
	a := config.Account{ID: accounts.NewID(k, list), Kind: k}
	if _, err := accounts.Prepare(k, a.ID); err != nil {
		return proto.AccountLogin{}, err
	}
	list = append(list, a)
	if err := ws.Cfg.SetConfigField(config.ScopeGlobal, "options.accounts", list); err != nil {
		_ = accounts.Remove(a.ID)
		return proto.AccountLogin{}, err
	}
	cfg = freshConfig(ws)
	sendConfigChanged(ws)
	return accountLogin(cfg, a)
}

// addProviderAccount adds another account of an API provider: a provider
// entry of its own, with the same type, endpoint, headers and models, a name
// that says which account it is, and no key. The app then opens the key
// sheet for the new provider (owner, 2026-10-24: every provider the model
// picker offers can have more than one account).
func (b *Backend) addProviderAccount(ws *Workspace, id string) (proto.AccountLogin, error) {
	cfg := ws.Cfg.Config()
	original, ok := cfg.Providers.Get(id)
	if !ok {
		return proto.AccountLogin{}, invalidFallback("unknown provider %q", id)
	}
	if config.IsCLIProviderType(original.Type) {
		return proto.AccountLogin{}, invalidFallback("%s signs in with its own CLI", id)
	}
	n := 2
	for {
		if _, taken := cfg.Providers.Get(fmt.Sprintf("%s-%d", id, n)); !taken {
			break
		}
		n++
	}
	next := original
	next.ID = fmt.Sprintf("%s-%d", id, n)
	next.Name = fmt.Sprintf("%s (%d)", cmp.Or(original.Name, id), n)
	next.APIKey = ""
	next.APIKeyTemplate = ""
	next.OAuthToken = nil
	if err := ws.Cfg.SetConfigField(config.ScopeGlobal, "providers."+next.ID, next); err != nil {
		return proto.AccountLogin{}, err
	}
	sendConfigChanged(ws)
	info := proto.AccountInfo{ID: next.ID, Kind: id, Name: next.Name, Label: next.Name}
	return proto.AccountLogin{Account: info}, nil
}

func accountLogin(cfg *config.Config, a config.Account) (proto.AccountLogin, error) {
	argv, err := accounts.LoginArgv(a.Kind, a.ID)
	if err != nil {
		return proto.AccountLogin{}, err
	}
	info := proto.AccountInfo{ID: a.ID, Kind: string(a.Kind), Name: accounts.Name(cfg, a.Kind, a.ID), Label: accounts.Label(cfg, a.Kind, a.ID), SignedIn: accounts.SignedIn(a.Kind, a.ID)}
	return proto.AccountLogin{Account: info, LoginArgv: argv}, nil
}

func findAccount(cfg *config.Config, id string) (config.Account, int, error) {
	if cfg.Options != nil {
		for i, a := range cfg.Options.Accounts {
			if a.ID == id {
				return a, i, nil
			}
		}
	}
	return config.Account{}, -1, ErrAccountNotFound
}

// SignInAccount brings an account's folder up to date and returns the
// command that signs it in again.
func (b *Backend) SignInAccount(workspaceID, id string) (proto.AccountLogin, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AccountLogin{}, err
	}
	cfg := ws.Cfg.Config()
	a, _, err := findAccount(cfg, id)
	if err != nil {
		return proto.AccountLogin{}, err
	}
	if _, err := accounts.Prepare(a.Kind, a.ID); err != nil {
		return proto.AccountLogin{}, err
	}
	return accountLogin(cfg, a)
}

// RenameAccount names an account: id is an options.accounts ID, or a tool
// (claude-code, codex-cli) for that tool's own login, whose name is kept in
// options.own_account_names. An empty name goes back to the account's
// email, else its number.
func (b *Backend) RenameAccount(workspaceID, id, name string) (proto.FallbackSettings, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg := ws.Cfg.Config()
	name = strings.TrimSpace(name)
	if config.HasAccounts(catwalk.Type(id)) {
		names := map[string]string{}
		if cfg.Options != nil {
			maps.Copy(names, cfg.Options.OwnAccountNames)
		}
		if name == "" {
			delete(names, id)
		} else {
			names[id] = name
		}
		if len(names) == 0 {
			err = ws.Cfg.RemoveConfigField(config.ScopeGlobal, "options.own_account_names")
		} else {
			err = ws.Cfg.SetConfigField(config.ScopeGlobal, "options.own_account_names", names)
		}
		if err != nil {
			return proto.FallbackSettings{}, err
		}
		cfg = freshConfig(ws)
		sendConfigChanged(ws)
		return fallbackSettings(cfg), nil
	}
	_, i, err := findAccount(cfg, id)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	list := slices.Clone(cfg.Options.Accounts)
	list[i].Name = name
	if err := ws.Cfg.SetConfigField(config.ScopeGlobal, "options.accounts", list); err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg = freshConfig(ws)
	sendConfigChanged(ws)
	return fallbackSettings(cfg), nil
}

// RemoveAccount deletes an account: its folder (its login and links; what
// they link to stays), its entry, and the order's blocks on it. A selected
// model on it goes back to the tool's own login.
func (b *Backend) RemoveAccount(workspaceID, id string) (proto.FallbackSettings, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg := ws.Cfg.Config()
	a, i, err := findAccount(cfg, id)
	if err != nil {
		return proto.FallbackSettings{}, err
	}
	fields := map[string]any{"options.accounts": slices.Delete(slices.Clone(cfg.Options.Accounts), i, i+1)}
	if cfg.Options.Fallback != nil {
		order := *cfg.Options.Fallback
		onAccount := func(blk config.FallbackBlock) bool {
			kind := blk.Kind
			if blk.Type == config.FallbackUse {
				if p, ok := cfg.Providers.Get(blk.Provider); ok {
					kind = p.Type
				}
			}
			return kind == a.Kind && blk.Account == id
		}
		order.Blocks = slices.DeleteFunc(slices.Clone(order.Blocks), onAccount)
		order.Lists = slices.Clone(order.Lists)
		for i := range order.Lists {
			order.Lists[i].Blocks = slices.DeleteFunc(slices.Clone(order.Lists[i].Blocks), onAccount)
		}
		fields["options.fallback"] = order
	}
	if err := ws.Cfg.SetConfigFields(config.ScopeGlobal, fields); err != nil {
		return proto.FallbackSettings{}, err
	}
	// The helper on the removed account goes back to the tool's own login;
	// a chat's own model on it does so when it runs (knownAccount).
	if m, ok := ws.Cfg.Config().Models[config.SelectedModelTypeSmall]; ok && m.Account == id {
		m.Account = ""
		if err := ws.Cfg.SetHelperModel(config.ScopeGlobal, m); err != nil {
			return proto.FallbackSettings{}, err
		}
	}
	if err := accounts.Remove(id); err != nil {
		return proto.FallbackSettings{}, err
	}
	cfg = freshConfig(ws)
	sendConfigChanged(ws)
	return fallbackSettings(cfg), nil
}

// UseRefill spends one of a Codex account's free refills.
func (b *Backend) UseRefill(ctx context.Context, kind, account string) (proto.RefillResult, error) {
	if catwalk.Type(kind) != config.TypeCodexCLI {
		return proto.RefillResult{}, invalidFallback("only Codex has free refills")
	}
	outcome, err := cliagent.ConsumeRefill(ctx, account)
	return proto.RefillResult{Outcome: outcome}, err
}
