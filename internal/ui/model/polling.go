package model

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/config"
)

// hyperCreditsPollInterval is how often the Hyper credits balance is
// refreshed while no session is running.
const hyperCreditsPollInterval = 60 * time.Second

// gitBranchPollInterval is how often the workspace's checked-out branch is
// re-read. A checkout emits no event Crush can subscribe to, so the branch
// has to be polled to stay current.
const gitBranchPollInterval = 5 * time.Second

// gitBranchFetchTimeout bounds one branch read. Locally this is a file
// read; in client/server mode it is a request to the server.
const gitBranchFetchTimeout = 10 * time.Second

// updateHyperCredits refreshes the displayed Hyper balance from the most
// recent /v1/credits fetch. The balance is fetched on startup, polled
// while idle and refreshed on every response during a session, so reading
// the stored value here is enough: until the first fetch lands the
// balance is unknown and stays hidden.
func (m *UI) updateHyperCredits() {
	if !m.com.IsHyper() {
		return
	}
	m.hyperCredits = hyper.Balance()
}

// fetchHyperCredits returns a command that asynchronously fetches the
// remaining Hyper credits from the /v1/credits endpoint. An expired
// OAuth token is refreshed first so a long-running session keeps
// reporting a balance.
func (m *UI) fetchHyperCredits() tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		var (
			apiKey      string
			cfg         *config.Config
			providerCfg config.ProviderConfig
		)
		getAPIKey := func() (ok bool) {
			if cfg = ws.Config(); cfg == nil || cfg.Providers == nil {
				return false
			}
			if providerCfg, ok = cfg.Providers.Get(hyper.Name); !ok {
				return false
			}
			var err error
			apiKey, err = ws.Resolver().ResolveValue(providerCfg.APIKey)
			return err == nil && apiKey != ""
		}
		if !getAPIKey() {
			return nil
		}

		if providerCfg.OAuthToken != nil && providerCfg.OAuthToken.IsExpired() {
			ctxRefresh, cancelRefresh := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancelRefresh()
			if err := ws.RefreshOAuthToken(ctxRefresh, config.ScopeGlobal, hyper.Name); err != nil {
				slog.Warn("Hyper OAuth refresh failed before fetching credits, trying with existing token", "error", err)
			} else if !getAPIKey() {
				return nil
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		credits, err := hyper.FetchCredits(ctx, apiKey)
		if err != nil {
			slog.Warn("Failed to fetch Hyper credits", "error", err)
			return nil
		}
		return creditsUpdatedMsg{credits: credits}
	}
}

// hyperCreditsTicker schedules the next Hyper credits poll.
func (m *UI) hyperCreditsTicker() tea.Cmd {
	return tea.Tick(hyperCreditsPollInterval, func(time.Time) tea.Msg {
		return hyperCreditsPollMsg{}
	})
}

// fetchGitBranch reads the workspace's checked-out branch off the render
// path. A failed read keeps whatever the last poll reported.
func (m *UI) fetchGitBranch() tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitBranchFetchTimeout)
		defer cancel()
		branch, err := ws.GitBranch(ctx)
		if err != nil {
			slog.Warn("Failed to read the git branch", "error", err)
			return nil
		}
		return gitBranchUpdatedMsg{branch: branch}
	}
}

// gitBranchTicker schedules the next git branch poll.
func (m *UI) gitBranchTicker() tea.Cmd {
	return tea.Tick(gitBranchPollInterval, func(time.Time) tea.Msg {
		return gitBranchPollMsg{}
	})
}
