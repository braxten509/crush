package cliagent

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
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

// agyUsageAnswer is the answer of `agy -p /usage --output-format json` as
// read on 2026-11-12, shortened; %[1]s is every part's reset time.
const agyUsageAnswer = `{"status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[
{"name":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[
 {"id":"gemini-weekly","window":"weekly","remaining_fraction":0.9,"reset_time":"%[1]s"},
 {"id":"gemini-5h","window":"5h","remaining_fraction":1,"reset_time":"%[1]s"}]},
{"name":"Claude and GPT models","description":"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS","buckets":[
 {"id":"3p-weekly","window":"weekly","remaining_fraction":0.7,"reset_time":"%[1]s"},
 {"id":"3p-5h","window":"5h","remaining_fraction":0.8,"reset_time":"%[1]s"}]},
{"name":"Gemini 4 Argon","description":"For paid Pro plans, Argon quota is separate.","buckets":[
 {"id":"gemini-argon-wtu-weekly","window":"weekly","remaining_fraction":0.6,"reset_time":"%[1]s"}]}]}}}`

// AGY's usage report names limit groups, not models: each chat model reads
// the group it belongs to. Argon has its own weekly limit, every other
// Gemini model (Hydrogen too) the general Gemini one, and Claude and GPT
// theirs (owner, 2026-11-12).
func TestAGYUsageGroupsReachTheirModelsMeters(t *testing.T) {
	limits, turns, err := agyUsage(fmt.Appendf(nil, agyUsageAnswer, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))
	require.NoError(t, err)
	require.Zero(t, turns)
	require.True(t, StoreAccountLimits(config.TypeAGYCLI, "", limits, -1))
	left := func(model string) map[string]int {
		out := map[string]int{}
		for _, l := range AccountLimits(config.TypeAGYCLI, "", model) {
			out[l.Name] = int(math.Round(l.Left()))
		}
		return out
	}
	require.Equal(t, map[string]int{"Weekly": 60}, left("gemini-4-argon-high"))
	require.Equal(t, map[string]int{"Weekly": 90, "5h": 100}, left("gemini-4-hydrogen-xhigh"))
	require.Equal(t, map[string]int{"Weekly": 90, "5h": 100}, left("gemini-3.8-flash-low"))
	require.Equal(t, map[string]int{"Weekly": 70, "5h": 80}, left("claude-opus-5-5-low"))
	require.Equal(t, map[string]int{"Weekly": 70, "5h": 80}, left("gpt-oss-120b-medium"))
	require.Empty(t, left("some-other-model"), "a model of no group reads nothing")
}

// An agy too old for the usage command answers /usage with a model turn:
// the read fails and says so, so it is never asked again.
func TestAGYUsageAnsweredByAModelIsNoReading(t *testing.T) {
	limits, turns, err := agyUsage([]byte(`{"status":"SUCCESS","response":"I can't show usage.","num_turns":1}`))
	require.Error(t, err)
	require.Empty(t, limits)
	require.Equal(t, 1, turns)
}
