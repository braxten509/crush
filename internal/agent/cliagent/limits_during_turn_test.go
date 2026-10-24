package cliagent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Every tool step of a running turn asks for its own fresh reading, so
// the meter moves every tool call: the store's idle refresh interval must
// not skip a reading another tool step just asked for.
func TestRefreshLimitsDuringTurnFetchesEveryCall(t *testing.T) {
	var fetches atomic.Int32
	restore := SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		config.TypeAGYCLI: func(context.Context) ([]Limit, error) {
			fetches.Add(1)
			return []Limit{{Name: "5h", Used: 42}}, nil
		},
	})
	defer restore()

	RefreshLimitsDuringTurn(context.Background(), config.TypeAGYCLI, "")
	// A tool step right behind the first gets its own reading too. The
	// pending guard shares one in-flight read, so keep asking until it
	// clears; under the idle interval the count would stay at one.
	require.Eventually(t, func() bool {
		RefreshLimitsDuringTurn(context.Background(), config.TypeAGYCLI, "")
		return fetches.Load() >= 2
	}, 5*time.Second, 50*time.Millisecond)
}
