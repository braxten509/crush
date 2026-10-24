package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// kimiUsageURL is Moonshot AI's plan usage address, the one Kimi's own
// CLI reads for its plan panel. Tests replace it.
var kimiUsageURL = "https://api.kimi.com/coding/v1/usages"

const kimiProvider = string(catwalk.InferenceKimiCoding)

// fetchKimiLimits reads the plan's 5-hour and monthly windows with the
// provider's API key. The old weekly window is ignored: the new plans do
// not have one (owner, 2026-10-24).
func fetchKimiLimits(ctx context.Context) ([]Limit, error) {
	key := limitProviderKey(kimiProvider)
	if key == "" {
		return nil, errors.New("Moonshot AI has no API key")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("moonshot usage: %s", resp.Status)
	}

	var payload struct {
		Usages struct {
			FiveHour *kimiWindow `json:"limit_5h"`
			Weekly   *kimiWindow `json:"limit_7d"`
			Monthly  *kimiWindow `json:"limit_month_total"`
		} `json:"usages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	var limits []Limit
	for _, w := range []struct {
		name   string
		window *kimiWindow
	}{{"5h", payload.Usages.FiveHour}, {"Monthly", payload.Usages.Monthly}} {
		if w.window == nil {
			continue
		}
		limit := Limit{Name: w.name, Used: w.window.UsedRatio * 100}
		if at, ok := parseKimiReset(w.window.ResetTime); ok {
			limit.ResetsAt = at
		}
		limits = append(limits, limit)
	}
	return limits, nil
}

// kimiWindow is one usage window: a 0..1 used ratio and its reset time.
type kimiWindow struct {
	UsedRatio float64 `json:"used_ratio"`
	ResetTime string  `json:"reset_time"`
}

// parseKimiReset reads a window's reset time, tolerating what the API
// leaves out.
func parseKimiReset(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return at.UTC(), true
}
