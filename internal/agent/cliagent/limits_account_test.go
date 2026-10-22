package cliagent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Each account keeps its own reading and refill count, and the tool's own
// login stays what Limits reads.
func TestLimitsAreKeptPerAccount(t *testing.T) {
	kind := config.TypeCodexCLI
	restore := SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(ctx context.Context) ([]Limit, error) {
			if LimitAccount(ctx) == "codex-2" {
				ReportRefills(ctx, 2)
				return []Limit{{Name: "5h", Used: 90}}, nil
			}
			return []Limit{{Name: "5h", Used: 10}}, nil
		},
	})
	defer restore()
	ctx := context.Background()
	_, err := RefreshAccountLimits(ctx, kind, "", true)
	require.NoError(t, err)
	_, err = RefreshAccountLimits(ctx, kind, "codex-2", true)
	require.NoError(t, err)

	require.Equal(t, 10.0, Limits(kind, "m")[0].Used)
	require.Equal(t, 90.0, AccountLimits(kind, "codex-2", "m")[0].Used)
	require.Equal(t, 0, Refills(kind, ""))
	require.Equal(t, 2, Refills(kind, "codex-2"))

	var accounts []string
	for _, k := range Snapshot() {
		if k.Kind == kind {
			accounts = append(accounts, k.Account)
			if k.Account == "codex-2" {
				require.Equal(t, 2, k.Refills)
			}
		}
	}
	require.Equal(t, []string{"", "codex-2"}, accounts)

	// A turn's own update keeps the refill count it doesn't carry.
	setLimits(limitKey{kind, "codex-2"}, []Limit{{Name: "5h", Used: 95}}, -1)
	require.Equal(t, 2, Refills(kind, "codex-2"))
	require.Equal(t, 95.0, AccountLimits(kind, "codex-2", "m")[0].Used)
	require.Equal(t, 10.0, Limits(kind, "m")[0].Used)
}

func TestConsumeRefillReadsTheAccountAgain(t *testing.T) {
	kind := config.TypeCodexCLI
	refills := 1
	restore := SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		kind: func(ctx context.Context) ([]Limit, error) {
			ReportRefills(ctx, refills)
			return []Limit{{Name: "5h", Used: float64(100 * refills)}}, nil
		},
	})
	defer restore()
	var asked string
	defer SetConsumeRefillForTest(func(_ context.Context, account string) (string, error) {
		asked, refills = account, 0
		return "reset", nil
	})()
	outcome, err := ConsumeRefill(context.Background(), "codex-2")
	require.NoError(t, err)
	require.Equal(t, "reset", outcome)
	require.Equal(t, "codex-2", asked)
	require.Equal(t, 0, Refills(kind, "codex-2"))
	require.Equal(t, 0.0, AccountLimits(kind, "codex-2", "m")[0].Used)
}

// AGY lists usage per model under its own names: a model with an effort in
// its name reads the entry without it, or that model's effort-tiered entry
// (owner, 2026-10-22: gemini-3.8-flash-high reads gemini-3.8-flash-tiered).
func TestAGYModelsReadTheirEffortTieredUsage(t *testing.T) {
	kind := config.TypeAGYCLI
	require.True(t, StoreAccountLimits(kind, "", []Limit{
		{Name: "Quota", Used: 5, Model: "gemini-3.8-flash-tiered"},
		{Name: "Quota", Used: 20, Model: "gemini-3.5-flash"},
		{Name: "Quota", Used: 100, Model: "claude-sonnet-4-6"},
		{Name: "Quota", Used: 50, Model: "gemini-3.5-flash-lite"},
	}, -1))
	used := func(model string) []float64 {
		var out []float64
		for _, l := range AccountLimits(kind, "", model) {
			out = append(out, l.Used)
		}
		return out
	}
	require.Equal(t, []float64{5}, used("gemini-3.8-flash-high"))
	require.Equal(t, []float64{5}, used("gemini-3.8-flash-low"))
	require.Equal(t, []float64{20}, used("gemini-3.5-flash-medium"))
	require.Equal(t, []float64{100}, used("claude-sonnet-4-6"))
	require.Empty(t, used("gemini-3.7-flash-high"), "a model AGY doesn't list reads nothing")
}

// AGY reports no window length, so each quota is named by the time to its
// reset: its Gemini quotas reset within five hours and its Claude and
// GPT-OSS quotas within a week or more (owner, 2026-10-22: the meters show
// only the 5-hour and weekly windows).
func TestAGYWindowsAreNamedByTheirLength(t *testing.T) {
	now := time.Date(2026, 10, 22, 13, 55, 0, 0, time.UTC)
	require.Equal(t, "5h", agyWindowName(now, now.Add(4*time.Hour)))
	require.Equal(t, "5h", agyWindowName(now, now.Add(5*time.Hour+20*time.Minute)))
	require.Equal(t, "Weekly", agyWindowName(now, now.Add(4*24*time.Hour)))
	require.Equal(t, "Weekly", agyWindowName(now, now.Add(7*24*time.Hour)))
	require.Equal(t, "12d", agyWindowName(now, now.Add(12*24*time.Hour+3*time.Hour)))
	require.Equal(t, "Limit", agyWindowName(now, time.Time{}))
}

// A whole AGY reading reaches the selected Gemini model's meter: the
// tiered quota, named for the five-hour window the meters show.
func TestAGYGeminiQuotaReachesItsMeter(t *testing.T) {
	now := time.Date(2026, 10, 22, 13, 55, 0, 0, time.UTC)
	geminiReset, weeklyReset := now.Add(4*time.Hour), now.Add(5*24*time.Hour)
	require.True(t, StoreAccountLimits(config.TypeAGYCLI, "", []Limit{
		{Name: agyWindowName(now, geminiReset), Used: 12, ResetsAt: geminiReset, Model: "gemini-3.8-flash-tiered"},
		{Name: agyWindowName(now, weeklyReset), Used: 87, ResetsAt: weeklyReset, Model: "claude-opus-4-6-thinking"},
	}, -1))
	limits := AccountLimits(config.TypeAGYCLI, "", "gemini-3.8-flash-high")
	require.Len(t, limits, 1)
	require.Equal(t, "5h", limits[0].Name)
	require.InDelta(t, 88, limits[0].Left(), 0.01)
}
