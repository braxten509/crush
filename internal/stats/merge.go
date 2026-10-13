package stats

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

// Merge combines stats from multiple projects into a single Stats struct.
func Merge(projectStats []ProjectStats) *Stats {
	m := newMerger()
	for _, ps := range projectStats {
		m.add(ps.Stats)
	}
	return m.result()
}

// merger accumulates per-project stats keyed by their natural identity
// (day, model and provider, hour, ...) so equal entries are summed.
type merger struct {
	merged *Stats

	dailyUsage     map[string]DailyUsage
	modelUsage     map[string]ModelUsage
	hourlyUsage    map[int]HourlyUsage
	dayOfWeek      map[int]DayOfWeekUsage
	recentActivity map[string]DailyActivity
	toolUsage      map[string]ToolUsage
	heatmap        map[string]HourDayHeatmapPt // Keyed by "day-hour".

	totalResponseTimeMs float64
	responseTimeCount   int64
}

func newMerger() *merger {
	return &merger{
		merged: &Stats{
			GeneratedAt: time.Now().UTC(),
		},
		dailyUsage:     make(map[string]DailyUsage),
		modelUsage:     make(map[string]ModelUsage),
		hourlyUsage:    make(map[int]HourlyUsage),
		dayOfWeek:      make(map[int]DayOfWeekUsage),
		recentActivity: make(map[string]DailyActivity),
		toolUsage:      make(map[string]ToolUsage),
		heatmap:        make(map[string]HourDayHeatmapPt),
	}
}

// add folds one project's stats into the running totals.
func (m *merger) add(s *Stats) {
	total := &m.merged.Total
	total.TotalSessions += s.Total.TotalSessions
	total.TotalPromptTokens += s.Total.TotalPromptTokens
	total.TotalCompletionTokens += s.Total.TotalCompletionTokens
	total.TotalTokens += s.Total.TotalTokens
	total.TotalCost += s.Total.TotalCost
	total.TotalMessages += s.Total.TotalMessages

	m.addUsage(s)
	m.addActivity(s)

	// Accumulate response time for averaging.
	if s.AvgResponseTimeMs > 0 {
		m.totalResponseTimeMs += s.AvgResponseTimeMs * float64(s.Total.TotalMessages)
		m.responseTimeCount += s.Total.TotalMessages
	}
}

// addUsage folds in usage by day, model, hour and day of week.
func (m *merger) addUsage(s *Stats) {
	for _, d := range s.UsageByDay {
		existing := m.dailyUsage[d.Day]
		existing.Day = d.Day
		existing.PromptTokens += d.PromptTokens
		existing.CompletionTokens += d.CompletionTokens
		existing.TotalTokens += d.TotalTokens
		existing.Cost += d.Cost
		existing.SessionCount += d.SessionCount
		m.dailyUsage[d.Day] = existing
	}

	for _, u := range s.UsageByModel {
		key := u.Model + "|" + u.Provider
		existing := m.modelUsage[key]
		existing.Model = u.Model
		existing.Provider = u.Provider
		existing.MessageCount += u.MessageCount
		m.modelUsage[key] = existing
	}

	for _, h := range s.UsageByHour {
		existing := m.hourlyUsage[h.Hour]
		existing.Hour = h.Hour
		existing.SessionCount += h.SessionCount
		m.hourlyUsage[h.Hour] = existing
	}

	for _, d := range s.UsageByDayOfWeek {
		existing := m.dayOfWeek[d.DayOfWeek]
		existing.DayOfWeek = d.DayOfWeek
		existing.DayName = d.DayName
		existing.SessionCount += d.SessionCount
		existing.PromptTokens += d.PromptTokens
		existing.CompletionTokens += d.CompletionTokens
		m.dayOfWeek[d.DayOfWeek] = existing
	}
}

// addActivity folds in recent activity, tool usage and the heatmap.
func (m *merger) addActivity(s *Stats) {
	for _, r := range s.RecentActivity {
		existing := m.recentActivity[r.Day]
		existing.Day = r.Day
		existing.SessionCount += r.SessionCount
		existing.TotalTokens += r.TotalTokens
		existing.Cost += r.Cost
		m.recentActivity[r.Day] = existing
	}

	// Tool names are normalized to lowercase for consistency.
	for _, t := range s.ToolUsage {
		toolName := strings.ToLower(t.ToolName)
		existing := m.toolUsage[toolName]
		existing.ToolName = toolName
		existing.CallCount += t.CallCount
		m.toolUsage[toolName] = existing
	}

	for _, h := range s.HourDayHeatmap {
		key := fmt.Sprintf("%d-%d", h.DayOfWeek, h.Hour)
		existing := m.heatmap[key]
		existing.DayOfWeek = h.DayOfWeek
		existing.Hour = h.Hour
		existing.SessionCount += h.SessionCount
		m.heatmap[key] = existing
	}
}

// result computes the averages and returns the merged stats, with model
// and tool usage sorted by count (descending).
func (m *merger) result() *Stats {
	merged := m.merged
	if merged.Total.TotalSessions > 0 {
		merged.Total.AvgTokensPerSession = float64(merged.Total.TotalTokens) / float64(merged.Total.TotalSessions)
		merged.Total.AvgMessagesPerSession = float64(merged.Total.TotalMessages) / float64(merged.Total.TotalSessions)
	}
	if m.responseTimeCount > 0 {
		merged.AvgResponseTimeMs = m.totalResponseTimeMs / float64(m.responseTimeCount)
	}

	merged.UsageByDay = slices.AppendSeq(merged.UsageByDay, maps.Values(m.dailyUsage))
	merged.UsageByModel = slices.AppendSeq(merged.UsageByModel, maps.Values(m.modelUsage))
	merged.UsageByHour = slices.AppendSeq(merged.UsageByHour, maps.Values(m.hourlyUsage))
	merged.UsageByDayOfWeek = slices.AppendSeq(merged.UsageByDayOfWeek, maps.Values(m.dayOfWeek))
	merged.RecentActivity = slices.AppendSeq(merged.RecentActivity, maps.Values(m.recentActivity))
	merged.ToolUsage = slices.AppendSeq(merged.ToolUsage, maps.Values(m.toolUsage))
	merged.HourDayHeatmap = slices.AppendSeq(merged.HourDayHeatmap, maps.Values(m.heatmap))

	sort.Slice(merged.UsageByModel, func(i, j int) bool {
		return merged.UsageByModel[i].MessageCount > merged.UsageByModel[j].MessageCount
	})
	sort.Slice(merged.ToolUsage, func(i, j int) bool {
		return merged.ToolUsage[i].CallCount > merged.ToolUsage[j].CallCount
	})

	return merged
}
