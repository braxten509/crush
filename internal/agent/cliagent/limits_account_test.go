package cliagent

import (
	"context"
	"testing"

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
