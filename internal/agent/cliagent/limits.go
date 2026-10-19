package cliagent

import (
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

// ponytail: in-process store, so the TUI only sees it in the default
// (non client/server) mode. The engine reads it through Snapshot and
// SetLimitsHook (engine-design.md 5.9 e).
var limitStore struct {
	sync.Mutex
	byKind  map[catwalk.Type][]Limit
	fetched map[catwalk.Type]time.Time
	pending map[catwalk.Type]bool
	// readAt is when the last reading arrived. Unlike fetched, which a
	// failed refresh sets too as a backoff, it proves a reading exists.
	readAt map[catwalk.Type]time.Time
	// lastErr is the last failed refresh since the last reading.
	lastErr map[catwalk.Type]string
	// hook is told about every new reading and every failed refresh.
	hook func(kind catwalk.Type)
	// replaced is set while a test's fetchers stand in for the real ones.
	replaced bool
}

// KindLimits is what the store knows about one CLI's usage limits.
type KindLimits struct {
	Kind    catwalk.Type
	Limits  []Limit
	ReadAt  time.Time // set only by setLimits
	Err     string    // last refresh error since the last reading
	Pending bool
}

// Snapshot returns the store's state for every CLI with a usage fetcher,
// sorted by kind.
func Snapshot() []KindLimits {
	limitStore.Lock()
	defer limitStore.Unlock()
	kinds := slices.Sorted(maps.Keys(limitFetchers))
	out := make([]KindLimits, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, KindLimits{
			Kind:    kind,
			Limits:  slices.Clone(limitStore.byKind[kind]),
			ReadAt:  limitStore.readAt[kind],
			Err:     limitStore.lastErr[kind],
			Pending: limitStore.pending[kind],
		})
	}
	return out
}

// SetLimitsHook installs fn, called after each new reading and each failed
// refresh, with no lock held. nil uninstalls it.
func SetLimitsHook(fn func(kind catwalk.Type)) {
	limitStore.Lock()
	defer limitStore.Unlock()
	limitStore.hook = fn
}

// limitsChanged tells the hook, if one is installed, that kind changed.
// Callers hold no store lock.
func limitsChanged(kind catwalk.Type) {
	limitStore.Lock()
	fn := limitStore.hook
	limitStore.Unlock()
	if fn != nil {
		fn(kind)
	}
}

// SetLimitFetchersForTest makes fetchers the only usage fetchers, with an
// empty store for them, until restore runs. Tests use it so that no real
// CLI or account is asked.
func SetLimitFetchersForTest(fetchers map[catwalk.Type]func(context.Context) ([]Limit, error)) (restore func()) {
	limitStore.Lock()
	defer limitStore.Unlock()
	saved := limitFetchers
	limitFetchers = maps.Clone(fetchers)
	limitStore.replaced = true
	for kind := range fetchers {
		delete(limitStore.byKind, kind)
		delete(limitStore.fetched, kind)
		delete(limitStore.readAt, kind)
		delete(limitStore.lastErr, kind)
	}
	return func() {
		limitStore.Lock()
		defer limitStore.Unlock()
		limitFetchers = saved
		limitStore.replaced = false
		for kind := range fetchers {
			delete(limitStore.byKind, kind)
			delete(limitStore.fetched, kind)
			delete(limitStore.readAt, kind)
			delete(limitStore.lastErr, kind)
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

// Limits returns the last known usage limits of a CLI that apply to a
// model, or nil.
func Limits(kind catwalk.Type, model string) []Limit {
	limitStore.Lock()
	defer limitStore.Unlock()
	var limits []Limit
	for _, l := range limitStore.byKind[kind] {
		if l.Model == "" || l.Model == model {
			limits = append(limits, l)
		}
	}
	return limits
}

// StoreLimits puts limits read elsewhere (the shared engine's reading,
// engine-design.md 5.13) into the cache Limits reads. It reports whether
// anything was stored.
func StoreLimits(kind catwalk.Type, limits []Limit) bool {
	if len(limits) == 0 {
		return false
	}
	setLimits(kind, limits)
	return true
}

func setLimits(kind catwalk.Type, limits []Limit) {
	if len(limits) == 0 {
		return
	}
	limitStore.Lock()
	if limitStore.byKind == nil {
		limitStore.byKind = map[catwalk.Type][]Limit{}
	}
	if limitStore.fetched == nil {
		limitStore.fetched = map[catwalk.Type]time.Time{}
	}
	if limitStore.readAt == nil {
		limitStore.readAt = map[catwalk.Type]time.Time{}
	}
	now := time.Now()
	limitStore.byKind[kind] = limits
	limitStore.fetched[kind] = now
	limitStore.readAt[kind] = now
	delete(limitStore.lastErr, kind)
	limitStore.Unlock()
	limitsChanged(kind)
}

// RefreshLimits asks a CLI's service for its usage limits unless they are
// recent. It reports whether new limits were stored.
func RefreshLimits(ctx context.Context, kind catwalk.Type) bool {
	updated, _ := refreshLimits(ctx, kind, false)
	return updated
}

// RefreshLimitsNow bypasses the refresh interval for an explicit usage request.
// Concurrent requests still share the same in-flight guard.
func RefreshLimitsNow(ctx context.Context, kind catwalk.Type) error {
	_, err := refreshLimits(ctx, kind, true)
	return err
}

func refreshLimits(ctx context.Context, kind catwalk.Type, force bool) (bool, error) {
	limitStore.Lock()
	fetch := limitFetchers[kind]
	if fetch == nil {
		limitStore.Unlock()
		return false, errors.New("This CLI does not report usage limits")
	}
	if limitStore.pending[kind] {
		limitStore.Unlock()
		return false, errors.New("Usage refresh is already in progress")
	}
	if !force && time.Since(limitStore.fetched[kind]) < limitRefresh {
		limitStore.Unlock()
		return false, nil
	}
	if limitStore.pending == nil {
		limitStore.pending = map[catwalk.Type]bool{}
	}
	limitStore.pending[kind] = true
	limitStore.Unlock()
	defer func() {
		limitStore.Lock()
		delete(limitStore.pending, kind)
		limitStore.Unlock()
	}()
	limits, err := fetch(ctx)
	if err == nil && force && len(limits) == 0 {
		err = errors.New("No usage windows were returned by this CLI")
	}
	if err != nil {
		// Back off like a success so a broken source isn't hammered.
		limitStore.Lock()
		if limitStore.fetched == nil {
			limitStore.fetched = map[catwalk.Type]time.Time{}
		}
		if limitStore.lastErr == nil {
			limitStore.lastErr = map[catwalk.Type]string{}
		}
		limitStore.fetched[kind] = time.Now()
		limitStore.lastErr[kind] = err.Error()
		limitStore.Unlock()
		limitsChanged(kind)
		return false, err
	}
	setLimits(kind, limits)
	return true, nil
}

var limitFetchers = map[catwalk.Type]func(context.Context) ([]Limit, error){
	config.TypeClaudeCode: fetchClaudeLimits,
	config.TypeCodexCLI:   fetchCodexLimits,
	config.TypeAGYCLI:     fetchAGYLimits,
	config.TypeGrokCLI:    fetchGrokLimits,
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
	for id, m := range res.Models {
		if m.Quota == nil {
			continue
		}
		reset, _ := time.Parse(time.RFC3339, m.Quota.ResetTime)
		limits = append(limits, Limit{Name: "Quota", Used: (1 - m.Quota.Remaining) * 100, ResetsAt: reset, Model: id})
	}
	return limits, nil
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
// Claude Code's /usage) with Claude Code's own login. An expired token is
// left for Claude Code to refresh on its next run.
func fetchClaudeLimits(ctx context.Context) ([]Limit, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
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
// `codex app-server`; it makes no model request.
func fetchCodexLimits(ctx context.Context) ([]Limit, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p, err := startProc("", "codex", "app-server")
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
		}
		if err := json.Unmarshal(msg.Result, &res); err != nil {
			return nil, err
		}
		return res.RateLimits.limits(), nil
	}
	return nil, exitError("codex", p)
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
