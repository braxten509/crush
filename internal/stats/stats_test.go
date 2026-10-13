package stats

import (
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestMerge(t *testing.T) {
	t.Parallel()

	a := &Stats{
		Total: TotalStats{
			TotalSessions: 2, TotalPromptTokens: 100, TotalCompletionTokens: 50,
			TotalTokens: 150, TotalCost: 1.5, TotalMessages: 10,
		},
		UsageByDay: []DailyUsage{
			{Day: "2026-01-01", PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, Cost: 1.5, SessionCount: 2},
		},
		UsageByModel: []ModelUsage{
			{Model: "gpt-4o", Provider: "openai", MessageCount: 3},
			{Model: "claude", Provider: "anthropic", MessageCount: 1},
		},
		UsageByHour:       []HourlyUsage{{Hour: 9, SessionCount: 2}},
		UsageByDayOfWeek:  []DayOfWeekUsage{{DayOfWeek: 4, DayName: "Thursday", SessionCount: 2, PromptTokens: 100, CompletionTokens: 50}},
		RecentActivity:    []DailyActivity{{Day: "2026-01-01", SessionCount: 2, TotalTokens: 150, Cost: 1.5}},
		AvgResponseTimeMs: 200,
		ToolUsage:         []ToolUsage{{ToolName: "Bash", CallCount: 2}, {ToolName: "view", CallCount: 1}},
		HourDayHeatmap:    []HourDayHeatmapPt{{DayOfWeek: 4, Hour: 9, SessionCount: 2}},
	}
	b := &Stats{
		Total: TotalStats{
			TotalSessions: 1, TotalPromptTokens: 10, TotalCompletionTokens: 5,
			TotalTokens: 15, TotalCost: 0.5, TotalMessages: 30,
		},
		UsageByDay: []DailyUsage{
			{Day: "2026-01-01", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.5, SessionCount: 1},
			{Day: "2026-01-02", PromptTokens: 1, TotalTokens: 1, SessionCount: 1},
		},
		UsageByModel: []ModelUsage{
			{Model: "claude", Provider: "anthropic", MessageCount: 5},
			{Model: "gpt-4o", Provider: "azure", MessageCount: 2},
		},
		UsageByHour:       []HourlyUsage{{Hour: 9, SessionCount: 1}, {Hour: 23, SessionCount: 1}},
		UsageByDayOfWeek:  []DayOfWeekUsage{{DayOfWeek: 4, DayName: "Thursday", SessionCount: 1, PromptTokens: 10, CompletionTokens: 5}},
		RecentActivity:    []DailyActivity{{Day: "2026-01-01", SessionCount: 1, TotalTokens: 15, Cost: 0.5}},
		AvgResponseTimeMs: 400,
		ToolUsage:         []ToolUsage{{ToolName: "bash", CallCount: 5}},
		HourDayHeatmap:    []HourDayHeatmapPt{{DayOfWeek: 4, Hour: 9, SessionCount: 1}, {DayOfWeek: 0, Hour: 23, SessionCount: 1}},
	}
	// A project with no response time does not dilute the average.
	c := &Stats{Total: TotalStats{TotalSessions: 1, TotalMessages: 100}}

	merged := Merge([]ProjectStats{{ProjectPath: "a", Stats: a}, {ProjectPath: "b", Stats: b}, {ProjectPath: "c", Stats: c}})

	require.False(t, merged.GeneratedAt.IsZero())
	require.Equal(t, TotalStats{
		TotalSessions: 4, TotalPromptTokens: 110, TotalCompletionTokens: 55,
		TotalTokens: 165, TotalCost: 2, TotalMessages: 140,
		AvgTokensPerSession: 165.0 / 4, AvgMessagesPerSession: 140.0 / 4,
	}, merged.Total)
	require.InDelta(t, (200.0*10+400.0*30)/40, merged.AvgResponseTimeMs, 1e-9)

	require.ElementsMatch(t, []DailyUsage{
		{Day: "2026-01-01", PromptTokens: 110, CompletionTokens: 55, TotalTokens: 165, Cost: 2, SessionCount: 3},
		{Day: "2026-01-02", PromptTokens: 1, TotalTokens: 1, SessionCount: 1},
	}, merged.UsageByDay)
	// Model usage is keyed by model and provider, sorted by count.
	require.Equal(t, []ModelUsage{
		{Model: "claude", Provider: "anthropic", MessageCount: 6},
		{Model: "gpt-4o", Provider: "openai", MessageCount: 3},
		{Model: "gpt-4o", Provider: "azure", MessageCount: 2},
	}, merged.UsageByModel)
	require.ElementsMatch(t, []HourlyUsage{{Hour: 9, SessionCount: 3}, {Hour: 23, SessionCount: 1}}, merged.UsageByHour)
	require.Equal(t, []DayOfWeekUsage{{DayOfWeek: 4, DayName: "Thursday", SessionCount: 3, PromptTokens: 110, CompletionTokens: 55}}, merged.UsageByDayOfWeek)
	require.Equal(t, []DailyActivity{{Day: "2026-01-01", SessionCount: 3, TotalTokens: 165, Cost: 2}}, merged.RecentActivity)
	// Tool names are folded to lower case, sorted by count.
	require.Equal(t, []ToolUsage{{ToolName: "bash", CallCount: 7}, {ToolName: "view", CallCount: 1}}, merged.ToolUsage)
	require.ElementsMatch(t, []HourDayHeatmapPt{
		{DayOfWeek: 4, Hour: 9, SessionCount: 3},
		{DayOfWeek: 0, Hour: 23, SessionCount: 1},
	}, merged.HourDayHeatmap)
}

func TestMerge_Empty(t *testing.T) {
	t.Parallel()

	merged := Merge(nil)
	require.Zero(t, merged.Total)
	require.Zero(t, merged.AvgResponseTimeMs)
	require.Nil(t, merged.UsageByDay)
	require.Nil(t, merged.UsageByModel)
	require.Nil(t, merged.ToolUsage)
}

func TestShouldSkipDir(t *testing.T) {
	t.Parallel()

	for _, name := range []string{".git", "node_modules", "vendor", "Library"} {
		require.True(t, shouldSkipDir(name), name)
	}
	for _, name := range []string{".crush", "src", "internal"} {
		require.False(t, shouldSkipDir(name), name)
	}
}

func TestGather(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := db.Connect(ctx, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	empty, err := Gather(ctx, conn)
	require.NoError(t, err)
	require.Zero(t, empty.Total.TotalSessions)

	sessions := session.NewService(db.New(conn), conn)
	_, err = sessions.Create(ctx, "one")
	require.NoError(t, err)
	_, err = sessions.Create(ctx, "two")
	require.NoError(t, err)

	got, err := Gather(ctx, conn)
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Total.TotalSessions)
	require.False(t, got.GeneratedAt.IsZero())
}
