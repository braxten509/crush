package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Claude's summary query uses local usage estimates, without making model
// requests. Its threshold includes native output and compaction headroom.
func claudeContextBudget(raw json.RawMessage) (Event, bool) {
	var response struct {
		RequestID string `json:"request_id"`
		Subtype   string `json:"subtype"`
		Response  struct {
			Threshold int64 `json:"autoCompactThreshold"`
			Enabled   bool  `json:"isAutoCompactEnabled"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &response) != nil || response.RequestID != "crush-context" || response.Subtype != "success" {
		return Event{}, false
	}
	limit := int64(0)
	if response.Response.Enabled {
		limit = max(0, response.Response.Threshold)
	}
	return Event{Type: EventContextBudget, ContextLimit: limit}, true
}

type codexContextSettings struct {
	Window int64 `json:"model_context_window"`
	Limit  int64 `json:"model_auto_compact_token_limit"`
}

// Read only model metadata from Codex's existing catalogue. Prompt templates
// and other catalogue fields are neither used nor exposed to the UI.
func codexCatalogContext(model string) (window, limit, effectivePercent int64) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return
	}
	var catalog struct {
		Models []struct {
			Slug             string `json:"slug"`
			Window           int64  `json:"context_window"`
			MaxWindow        int64  `json:"max_context_window"`
			Limit            int64  `json:"auto_compact_token_limit"`
			EffectivePercent int64  `json:"effective_context_window_percent"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		return
	}
	for _, entry := range catalog.Models {
		if entry.Slug != model {
			continue
		}
		window = entry.Window
		if window <= 0 {
			window = entry.MaxWindow
		}
		return window, entry.Limit, entry.EffectivePercent
	}
	return
}

// Codex bounds its configured compaction limit to 90% of the model window.
// Mark this as estimated: the native protocol does not expose that resolved
// threshold, and installed versions may change their headroom policy.
func codexContextBudget(settings codexContextSettings, catalogWindow, catalogLimit, reportedWindow, effectivePercent int64) Event {
	window, limit := catalogWindow, catalogLimit
	if settings.Window > 0 {
		window = settings.Window
	}
	if reportedWindow > 0 && effectivePercent > 0 && effectivePercent <= 100 {
		window = reportedWindow * 100 / effectivePercent
	}
	if settings.Limit > 0 {
		limit = settings.Limit
	}
	if window > 0 {
		boundary := window * 9 / 10
		if limit <= 0 || limit > boundary {
			limit = boundary
		}
	}
	return Event{Type: EventContextBudget, ContextLimit: max(0, limit), ContextEstimated: true}
}
