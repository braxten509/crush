package cliagent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/google/uuid"
)

// Limit is one usage window of a CLI's subscription.
type Limit struct {
	Name     string    // "5h", "Weekly", or a model name
	Used     float64   // percent of the window used, 0-100
	ResetsAt time.Time // zero when unknown
	Model    string    // set when the limit applies to one model only
}

// Left is the percent of the window still available now.
func (l Limit) Left() float64 {
	if !l.ResetsAt.IsZero() && time.Now().After(l.ResetsAt) {
		return 100
	}
	return max(0, 100-l.Used)
}

// Limits are refreshed between turns at most this often; turns report
// their own updates as they happen.
const limitRefresh = 3 * time.Minute

var (
	// ErrNoUsageSource is a refresh's error for a CLI that reports no usage.
	ErrNoUsageSource = errors.New("This CLI does not report usage limits") //nolint:staticcheck
	// errRefreshPending is a forced refresh's error while another runs.
	errRefreshPending = errors.New("Usage refresh is already in progress") //nolint:staticcheck
)

// limitKey is one account of one CLI: account "" is the tool's own login,
// any other an options.accounts ID.
type limitKey struct {
	kind    catwalk.Type
	account string
}

// ponytail: in-process store, so the TUI only sees it in the default
// (non client/server) mode. The engine reads it through Snapshot and
// SetLimitsHook (engine-design.md 5.9 e).
var limitStore struct {
	sync.Mutex
	byKind  map[limitKey][]Limit
	fetched map[limitKey]time.Time
	pending map[limitKey]bool
	// readAt is when the last reading arrived. Unlike fetched, which a
	// failed refresh sets too as a backoff, it proves a reading exists.
	readAt map[limitKey]time.Time
	// lastErr is the last failed refresh since the last reading.
	lastErr map[limitKey]string
	// refills is how many free refills an account has (Codex), when read.
	refills map[limitKey]int
	// hook is told about every new reading and every failed refresh.
	hook func(kind catwalk.Type, account string)
	// replaced is set while a test's fetchers stand in for the real ones.
	replaced bool
	// providerKey answers the API key of a provider whose usage the
	// engine tracks outside the agent CLIs (Kimi). The engine sets it
	// from the workspace config; nil means no key is known.
	providerKey func(string) string
}

// SetLimitProviderKeySource sets where a provider's API key comes from for
// usage reads. nil forgets it.
func SetLimitProviderKeySource(fn func(provider string) string) {
	limitStore.Lock()
	defer limitStore.Unlock()
	limitStore.providerKey = fn
}

// ReplaceLimitProviderKeySourceForTest stands in for the engine's key
// source until restore runs.
func ReplaceLimitProviderKeySourceForTest(fn func(provider string) string) (restore func()) {
	limitStore.Lock()
	saved := limitStore.providerKey
	limitStore.providerKey = fn
	limitStore.Unlock()
	return func() {
		limitStore.Lock()
		defer limitStore.Unlock()
		limitStore.providerKey = saved
	}
}

// limitProviderKey is the API key to read a provider's usage with.
func limitProviderKey(provider string) string {
	limitStore.Lock()
	fn := limitStore.providerKey
	limitStore.Unlock()
	if fn == nil {
		return ""
	}
	return fn(provider)
}

// KindLimits is what the store knows about one account's usage limits.
type KindLimits struct {
	Kind    catwalk.Type
	Account string // "" for the tool's own login
	Limits  []Limit
	Refills int       // free refills left (Codex); 0 when none or unread
	ReadAt  time.Time // set only by setLimits
	Err     string    // last refresh error since the last reading
	Pending bool
}

// Snapshot returns the store's state for every CLI with a usage fetcher
// (its own login) and every other account the store has heard of, sorted
// by kind, then account.
func Snapshot() []KindLimits {
	limitStore.Lock()
	defer limitStore.Unlock()
	keys := map[limitKey]bool{}
	for kind := range limitFetchers {
		keys[limitKey{kind: kind}] = true
		for _, m := range []map[limitKey]time.Time{limitStore.fetched, limitStore.readAt} {
			for k := range m {
				if k.kind == kind {
					keys[k] = true
				}
			}
		}
		for k := range limitStore.pending {
			if k.kind == kind {
				keys[k] = true
			}
		}
	}
	sorted := slices.SortedFunc(maps.Keys(keys), func(a, b limitKey) int {
		return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.account, b.account))
	})
	out := make([]KindLimits, 0, len(sorted))
	for _, k := range sorted {
		out = append(out, KindLimits{
			Kind:    k.kind,
			Account: k.account,
			Limits:  slices.Clone(limitStore.byKind[k]),
			Refills: limitStore.refills[k],
			ReadAt:  limitStore.readAt[k],
			Err:     limitStore.lastErr[k],
			Pending: limitStore.pending[k],
		})
	}
	return out
}

// SetLimitsHook installs fn, called after each new reading and each failed
// refresh, with no lock held. nil uninstalls it.
func SetLimitsHook(fn func(kind catwalk.Type, account string)) {
	limitStore.Lock()
	defer limitStore.Unlock()
	limitStore.hook = fn
}

// limitsChanged tells the hook, if one is installed, that an account
// changed. Callers hold no store lock.
func limitsChanged(k limitKey) {
	limitStore.Lock()
	fn := limitStore.hook
	limitStore.Unlock()
	if fn != nil {
		fn(k.kind, k.account)
	}
}

// forgetKind drops everything the store knows about kind's accounts.
// Callers hold the store lock.
func forgetKind(kind catwalk.Type) {
	of := func(k limitKey) bool { return k.kind == kind }
	maps.DeleteFunc(limitStore.byKind, func(k limitKey, _ []Limit) bool { return of(k) })
	maps.DeleteFunc(limitStore.fetched, func(k limitKey, _ time.Time) bool { return of(k) })
	maps.DeleteFunc(limitStore.readAt, func(k limitKey, _ time.Time) bool { return of(k) })
	maps.DeleteFunc(limitStore.lastErr, func(k limitKey, _ string) bool { return of(k) })
	maps.DeleteFunc(limitStore.refills, func(k limitKey, _ int) bool { return of(k) })
}

// SetLimitFetchersForTest makes fetchers the only usage fetchers, with an
// empty store for them, until restore runs. Tests use it so that no real
// CLI or account is asked. A fetcher reads the account it is asked about
// with LimitAccount.
func SetLimitFetchersForTest(fetchers map[catwalk.Type]func(context.Context) ([]Limit, error)) (restore func()) {
	limitStore.Lock()
	defer limitStore.Unlock()
	saved := limitFetchers
	limitFetchers = maps.Clone(fetchers)
	limitStore.replaced = true
	for kind := range fetchers {
		forgetKind(kind)
	}
	return func() {
		limitStore.Lock()
		defer limitStore.Unlock()
		limitFetchers = saved
		limitStore.replaced = false
		for kind := range fetchers {
			forgetKind(kind)
		}
	}
}

// LimitFetchersReplaced reports whether a test's fetchers stand in for the
// real ones (SetLimitFetchersForTest).
func LimitFetchersReplaced() bool {
	limitStore.Lock()
	defer limitStore.Unlock()
	return limitStore.replaced
}

// Limits returns the last known usage limits of a CLI's own login that
// apply to a model, or nil.
func Limits(kind catwalk.Type, model string) []Limit {
	return AccountLimits(kind, "", model)
}

// AccountLimits returns the last known usage limits of one account that
// apply to a model, or nil.
func AccountLimits(kind catwalk.Type, account, model string) []Limit {
	limitStore.Lock()
	defer limitStore.Unlock()
	stored := limitStore.byKind[limitKey{kind, account}]
	named := usageModel(stored, model)
	var limits []Limit
	for _, l := range stored {
		if l.Model == "" || l.Model == named {
			limits = append(limits, l)
		}
	}
	return limits
}

// effortWords are the effort levels a CLI's model name may end in.
var effortWords = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// usageModel is the per-model name model's usage is stored under: its own,
// else its name without the effort word, else that name's effort-tiered
// entry (AGY lists gemini-3.8-flash-high's usage as
// gemini-3.8-flash-tiered; owner, 2026-10-22). "" when none is stored.
func usageModel(limits []Limit, model string) string {
	has := func(name string) bool {
		return name != "" && slices.ContainsFunc(limits, func(l Limit) bool { return l.Model == name })
	}
	if has(model) {
		return model
	}
	for _, word := range effortWords {
		base, ok := strings.CutSuffix(model, "-"+word)
		if !ok {
			continue
		}
		for _, name := range []string{base, base + "-tiered"} {
			if has(name) {
				return name
			}
		}
	}
	return ""
}

// Refills is how many free refills an account has left, as last read.
func Refills(kind catwalk.Type, account string) int {
	limitStore.Lock()
	defer limitStore.Unlock()
	return limitStore.refills[limitKey{kind, account}]
}

// StoreLimits puts limits read elsewhere (the shared engine's reading,
// engine-design.md 5.13) into the cache Limits reads. It reports whether
// anything was stored.
func StoreLimits(kind catwalk.Type, limits []Limit) bool {
	return StoreAccountLimits(kind, "", limits, -1)
}

// StoreAccountLimits is StoreLimits for any account, with its refill
// count (negative leaves the count as it is).
func StoreAccountLimits(kind catwalk.Type, account string, limits []Limit, refills int) bool {
	if len(limits) == 0 {
		return false
	}
	setLimits(limitKey{kind, account}, limits, refills)
	return true
}

// setLimits stores a reading; refills < 0 keeps the known refill count
// (a turn's own updates carry none).
func setLimits(k limitKey, limits []Limit, refills int) {
	if len(limits) == 0 {
		return
	}
	limitStore.Lock()
	if limitStore.byKind == nil {
		limitStore.byKind = map[limitKey][]Limit{}
	}
	if limitStore.fetched == nil {
		limitStore.fetched = map[limitKey]time.Time{}
	}
	if limitStore.readAt == nil {
		limitStore.readAt = map[limitKey]time.Time{}
	}
	if limitStore.refills == nil {
		limitStore.refills = map[limitKey]int{}
	}
	now := time.Now()
	limitStore.byKind[k] = limits
	limitStore.fetched[k] = now
	limitStore.readAt[k] = now
	if refills >= 0 {
		limitStore.refills[k] = refills
	}
	delete(limitStore.lastErr, k)
	limitStore.Unlock()
	limitsChanged(k)
}

// limitAccountKey carries the account a fetch is for.
type limitAccountKey struct{}

// refillSinkKey carries where a fetch reports the account's refills.
type refillSinkKey struct{}

// LimitAccount is the account a usage fetch is for ("" for the tool's own
// login).
func LimitAccount(ctx context.Context) string {
	id, _ := ctx.Value(limitAccountKey{}).(string)
	return id
}

// ReportRefills tells the store how many free refills the account being
// fetched has. Fetchers that know call it.
func ReportRefills(ctx context.Context, n int) {
	if sink, ok := ctx.Value(refillSinkKey{}).(*int); ok {
		*sink = n
	}
}

// RefreshLimits asks a CLI's service for its own login's usage limits
// unless they are recent. It reports whether new limits were stored.
func RefreshLimits(ctx context.Context, kind catwalk.Type) bool {
	updated, _ := refreshLimits(ctx, limitKey{kind: kind}, false)
	return updated
}

// RefreshLimitsNow bypasses the refresh interval for an explicit usage request.
// Concurrent requests still share the same in-flight guard.
func RefreshLimitsNow(ctx context.Context, kind catwalk.Type) error {
	_, err := refreshLimits(ctx, limitKey{kind: kind}, true)
	return err
}

// RefreshAccountLimits refreshes one account's usage limits; force skips
// the refresh interval. It reports whether new limits were stored.
func RefreshAccountLimits(ctx context.Context, kind catwalk.Type, account string, force bool) (bool, error) {
	return refreshLimits(ctx, limitKey{kind, account}, force)
}

// RefreshLimitsDuringTurn starts a usage reading for a turn that is still
// working, so its meter moves mid-turn instead of only when the turn ends.
// read is the caller's once-per-turn flag, owned by the turn's own
// goroutine: the first call forces a fresh read (an idle poll may have
// just read), and later calls keep the refresh interval, so a long turn
// asks the service at most once per [limitRefresh].
func RefreshLimitsDuringTurn(ctx context.Context, kind catwalk.Type, account string, read *bool) {
	force := !*read
	*read = true
	ctx = context.WithoutCancel(ctx)
	go func() {
		_, _ = RefreshAccountLimits(ctx, kind, account, force)
	}()
}

// ReadAccountLimitsNow reads one account's usage limits fresh into the
// store, waiting out a refresh already on its way rather than failing. It
// returns the reading's error; ErrNoUsageSource for a CLI without one.
func ReadAccountLimitsNow(ctx context.Context, kind catwalk.Type, account string) error {
	for {
		_, err := refreshLimits(ctx, limitKey{kind, account}, true)
		if !errors.Is(err, errRefreshPending) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func refreshLimits(ctx context.Context, k limitKey, force bool) (bool, error) {
	limitStore.Lock()
	fetch := limitFetchers[k.kind]
	if fetch == nil {
		limitStore.Unlock()
		return false, ErrNoUsageSource
	}
	if limitStore.pending[k] {
		limitStore.Unlock()
		return false, errRefreshPending
	}
	if !force && time.Since(limitStore.fetched[k]) < limitRefresh {
		limitStore.Unlock()
		return false, nil
	}
	if limitStore.pending == nil {
		limitStore.pending = map[limitKey]bool{}
	}
	limitStore.pending[k] = true
	limitStore.Unlock()
	defer func() {
		limitStore.Lock()
		delete(limitStore.pending, k)
		limitStore.Unlock()
	}()
	refills := -1
	ctx = context.WithValue(context.WithValue(ctx, limitAccountKey{}, k.account), refillSinkKey{}, &refills)
	limits, err := fetch(ctx)
	if err == nil && force && len(limits) == 0 {
		err = errors.New("No usage windows were returned by this CLI")
	}
	if err != nil {
		// Back off like a success so a broken source isn't hammered.
		limitStore.Lock()
		if limitStore.fetched == nil {
			limitStore.fetched = map[limitKey]time.Time{}
		}
		if limitStore.lastErr == nil {
			limitStore.lastErr = map[limitKey]string{}
		}
		limitStore.fetched[k] = time.Now()
		limitStore.lastErr[k] = err.Error()
		limitStore.Unlock()
		limitsChanged(k)
		return false, err
	}
	setLimits(k, limits, refills)
	return true, nil
}

var limitFetchers = map[catwalk.Type]func(context.Context) ([]Limit, error){
	config.TypeClaudeCode:                     fetchClaudeLimits,
	config.TypeCodexCLI:                       fetchCodexLimits,
	config.TypeAGYCLI:                         fetchAGYLimits,
	config.TypeGrokCLI:                        fetchGrokLimits,
	config.TypeOpenCodeCLI:                    fetchOpenCodeLimits,
	catwalk.Type(catwalk.InferenceKimiCoding): fetchKimiLimits,
}

// fetchGrokLimits asks `grok agent stdio` for the billing config behind
// Grok's /usage view, through its ACP extension method x.ai/billing
// (extension methods go on the wire with a leading underscore).
func fetchGrokLimits(ctx context.Context) ([]Limit, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p, err := startProc("", "grok", "agent", "--no-leader", "stdio")
	if err != nil {
		return nil, err
	}
	defer func() {
		p.kill()
		p.finish() // Reap the short-lived usage probe as well as stopping it.
	}()
	defer p.watchCancel(ctx, p.closeInput)()
	_ = p.send(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "initialize", "params": map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}})
	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil {
			continue
		}
		switch strings.Trim(string(msg.ID), `"`) {
		case "1":
			_ = p.send(map[string]any{"jsonrpc": "2.0", "id": "2", "method": "_x.ai/billing", "params": map[string]any{}})
		case "2":
			if msg.Error != nil {
				return nil, errors.New(msg.Error.Message)
			}
			return grokBilling(msg.Result)
		}
	}
	return nil, exitError("grok", p)
}

func grokBilling(raw json.RawMessage) ([]Limit, error) {
	var res struct {
		Config struct {
			Period struct {
				Type string    `json:"type"`
				End  time.Time `json:"end"`
			} `json:"currentPeriod"`
			// A plain number or {"val": n}, like Grok's other amounts;
			// absent until something is used.
			UsagePercent json.RawMessage `json:"creditUsagePercent"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	c := res.Config
	if c.Period.Type == "" {
		return nil, errors.New("grok reported no usage period")
	}
	var used float64
	if json.Unmarshal(c.UsagePercent, &used) != nil {
		var v struct{ Val float64 }
		_ = json.Unmarshal(c.UsagePercent, &v)
		used = v.Val
	}
	name := strings.ToLower(strings.TrimPrefix(c.Period.Type, "USAGE_PERIOD_TYPE_"))
	if name == "" {
		return nil, errors.New("grok reported an empty usage period")
	}
	name = strings.ToUpper(name[:1]) + name[1:]
	return []Limit{{Name: name, Used: used, ResetsAt: c.Period.End}}, nil
}

// fetchAGYLimits reads each model's quota from the Cloud Code endpoint the
// Antigravity app uses. AGY keeps its login in the Secret Service keyring
// and refreshes it on any network command, so an expired token is renewed
// by running `agy models`.
func fetchAGYLimits(ctx context.Context) ([]Limit, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, err := agyToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	// ponytail: the endpoint rejects requests without an AGY user agent;
	// the version isn't checked.
	req.Header.Set("User-Agent", "Antigravity CLI (agy)/1.2.11")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agy quota: %s", resp.Status)
	}
	var res struct {
		Models map[string]struct {
			Quota *struct {
				Remaining float64 `json:"remainingFraction"`
				ResetTime string  `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var limits []Limit
	now := time.Now()
	for id, m := range res.Models {
		if m.Quota == nil {
			continue
		}
		reset, _ := time.Parse(time.RFC3339, m.Quota.ResetTime)
		limits = append(limits, Limit{Name: agyWindowName(now, reset), Used: (1 - m.Quota.Remaining) * 100, ResetsAt: reset, Model: id})
	}
	return limits, nil
}

// agyWindowName names a quota window by what it has left. Antigravity
// reports no window length: its Gemini quotas reset every five hours and
// its Claude and GPT-OSS quotas every week, so the time to the reset says
// which window it is. A weekly window in its last hours reads as the
// five-hour one.
func agyWindowName(now, reset time.Time) string {
	if reset.IsZero() {
		return "Limit"
	}
	left := reset.Sub(now)
	switch {
	case left <= 5*time.Hour+30*time.Minute:
		return "5h"
	case left <= 8*24*time.Hour:
		return "Weekly"
	}
	return fmt.Sprintf("%dd", max(int(left.Round(24*time.Hour)/(24*time.Hour)), 1))
}

func agyToken(ctx context.Context) (string, error) {
	command := func(name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, args...)
		ownGroup(cmd)
		cmd.Cancel = func() error {
			(&proc{cmd: cmd}).kill()
			return nil
		}
		cmd.WaitDelay = time.Second // A helper may inherit stdout past cancellation.
		return cmd
	}
	read := func() (string, time.Time) {
		out, err := command("secret-tool", "lookup", "service", "gemini", "username", "antigravity").Output()
		if err != nil {
			return "", time.Time{}
		}
		var v struct {
			Token struct {
				AccessToken string    `json:"access_token"`
				Expiry      time.Time `json:"expiry"`
			} `json:"token"`
		}
		_ = json.Unmarshal(out, &v)
		return v.Token.AccessToken, v.Token.Expiry
	}
	token, expiry := read()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if token == "" || time.Until(expiry) < time.Minute {
		_ = command(agyBin(), "models").Run()
		token, expiry = read()
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if token == "" || time.Until(expiry) < 0 {
		return "", errors.New("no AGY login in the keyring")
	}
	return token, nil
}

// windowName names a usage window by its length.
func windowName(minutes int) string {
	switch {
	case minutes == 7*24*60:
		return "Weekly"
	case minutes == 30*24*60:
		return "Monthly"
	case minutes >= 24*60 && minutes%(24*60) == 0:
		return fmt.Sprintf("%dd", minutes/(24*60))
	case minutes >= 60 && minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	}
	return "Limit"
}

// Claude windows by the key Anthropic reports them under.
var claudeWindows = []struct{ key, name string }{
	{"five_hour", "5h"},
	{"seven_day", "Weekly"},
	{"seven_day_opus", "Weekly Opus"},
	{"seven_day_sonnet", "Weekly Sonnet"},
}

// claudeRateLimit reads the rate_limit_event Claude Code emits each turn.
// Its utilization is a fraction; resetsAt is Unix seconds.
func claudeRateLimit(raw json.RawMessage) []Limit {
	var ev struct {
		Info struct {
			Windows map[string]*struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return nil
	}
	var limits []Limit
	for _, w := range claudeWindows {
		if v := ev.Info.Windows[w.key]; v != nil {
			limits = append(limits, Limit{Name: w.name, Used: v.Utilization * 100, ResetsAt: unixTime(v.ResetsAt)})
		}
	}
	return limits
}

// fetchClaudeLimits asks Anthropic's usage endpoint (the one behind
// Claude Code's /usage) with the account's Claude Code login. An expired
// token is left for Claude Code to refresh on its next run.
func fetchClaudeLimits(ctx context.Context) ([]Limit, error) {
	dir := config.AccountDir(LimitAccount(ctx))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		return nil, err
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.OAuth.AccessToken == "" || time.Now().After(time.UnixMilli(creds.OAuth.ExpiresAt)) {
		return nil, errors.New("claude login expired")
	}
	var usage map[string]json.RawMessage
	err = getJSON(ctx, "https://api.anthropic.com/api/oauth/usage", map[string]string{
		"Authorization":  "Bearer " + creds.OAuth.AccessToken,
		"anthropic-beta": "oauth-2025-04-20",
	}, &usage)
	if err != nil {
		return nil, err
	}
	var limits []Limit
	for _, w := range claudeWindows {
		var v *struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}
		if json.Unmarshal(usage[w.key], &v) == nil && v != nil {
			reset, _ := time.Parse(time.RFC3339Nano, v.ResetsAt)
			limits = append(limits, Limit{Name: w.name, Used: v.Utilization, ResetsAt: reset})
		}
	}
	return limits, nil
}

// codexSnapshot is Codex's RateLimitSnapshot.
type codexSnapshot struct {
	LimitID   *string      `json:"limitId"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

type codexWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	WindowMins  int     `json:"windowDurationMins"`
	ResetsAt    int64   `json:"resetsAt"`
}

// limits turns the snapshot into limits, or nil for another quota bucket
// or a sparse update without windows.
func (s codexSnapshot) limits() []Limit {
	if s.LimitID != nil && *s.LimitID != "codex" {
		return nil
	}
	var limits []Limit
	for _, w := range []*codexWindow{s.Primary, s.Secondary} {
		if w != nil {
			limits = append(limits, Limit{Name: windowName(w.WindowMins), Used: w.UsedPercent, ResetsAt: unixTime(w.ResetsAt)})
		}
	}
	return limits
}

// fetchCodexLimits reads account/rateLimits/read from a short-lived
// `codex app-server` on the account; it makes no model request. The
// account's free refills come with it.
func fetchCodexLimits(ctx context.Context) ([]Limit, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p, err := startProcEnv("", config.AccountEnv(config.TypeCodexCLI, LimitAccount(ctx)), "codex", "app-server")
	if err != nil {
		return nil, err
	}
	defer func() {
		p.kill()
		p.finish() // Reap the short-lived usage probe as well as stopping it.
	}()
	defer p.watchCancel(ctx, p.closeInput)()
	_ = p.send(map[string]any{"id": "1", "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": "0"}}})
	_ = p.send(map[string]any{"method": "initialized"})
	_ = p.send(map[string]any{"id": "2", "method": "account/rateLimits/read"})
	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil || strings.Trim(string(msg.ID), `"`) != "2" {
			continue
		}
		if msg.Error != nil {
			return nil, errors.New(msg.Error.Message)
		}
		var res struct {
			RateLimits codexSnapshot `json:"rateLimits"`
			Refills    *struct {
				Available int `json:"availableCount"`
			} `json:"rateLimitResetCredits"`
		}
		if err := json.Unmarshal(msg.Result, &res); err != nil {
			return nil, err
		}
		refills := 0
		if res.Refills != nil {
			refills = res.Refills.Available
		}
		ReportRefills(ctx, refills)
		return res.RateLimits.limits(), nil
	}
	return nil, exitError("codex", p)
}

// ConsumeRefill spends one of a Codex account's free refills (OpenAI's
// rate-limit reset credits), then reads its limits again. It reports what
// Codex says happened: "reset", "nothingToReset", "noCredit" or
// "alreadyRedeemed".
func ConsumeRefill(ctx context.Context, account string) (string, error) {
	outcome, err := consumeRefill(ctx, account)
	if err == nil {
		_, _ = RefreshAccountLimits(ctx, config.TypeCodexCLI, account, true)
	}
	return outcome, err
}

// consumeRefill is the refill request; tests replace it.
var consumeRefill = func(ctx context.Context, account string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := startProcEnv("", config.AccountEnv(config.TypeCodexCLI, account), "codex", "app-server")
	if err != nil {
		return "", err
	}
	defer func() {
		p.kill()
		p.finish()
	}()
	defer p.watchCancel(ctx, p.closeInput)()
	_ = p.send(map[string]any{"id": "1", "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": "0"}}})
	_ = p.send(map[string]any{"method": "initialized"})
	_ = p.send(map[string]any{"id": "2", "method": "account/rateLimitResetCredit/consume", "params": map[string]any{"idempotencyKey": uuid.NewString()}})
	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil || strings.Trim(string(msg.ID), `"`) != "2" {
			continue
		}
		if msg.Error != nil {
			return "", errors.New(msg.Error.Message)
		}
		var res struct {
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(msg.Result, &res); err != nil {
			return "", err
		}
		return res.Outcome, nil
	}
	return "", exitError("codex", p)
}

// SetConsumeRefillForTest replaces the refill request until restore runs.
func SetConsumeRefillForTest(fn func(ctx context.Context, account string) (string, error)) (restore func()) {
	saved := consumeRefill
	consumeRefill = fn
	return func() { consumeRefill = saved }
}

func getJSON(ctx context.Context, url string, headers map[string]string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
