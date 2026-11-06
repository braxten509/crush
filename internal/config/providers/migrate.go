package providers

import (
	"bytes"
	"encoding/json"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// legacyPricingFields maps catwalk v2 model cost fields onto their v3
// pricing counterparts.
var legacyPricingFields = map[string]string{
	"cost_per_1m_in":         "input",
	"cost_per_1m_out":        "output",
	"cost_per_1m_in_cached":  "cache_create",
	"cost_per_1m_out_cached": "cache_hit",
}

// MigrateV2Model migrates a single catwalk v2 model object onto the v3
// schema in place and reports whether anything changed. In v3, flat model
// fields moved into nested structures: the cost fields became pricing,
// supports_attachments became capabilities.vision, and the can_reason/
// reasoning_levels/default_reasoning_effort trio became the reasoning
// object. Legacy fields are removed after being folded into their v3
// counterparts so they never leak back into the decoded model. A model
// already written with v3 fields is left as it is.
func MigrateV2Model(model map[string]any) bool {
	changed := false

	if _, ok := model["pricing"]; !ok {
		pricing := map[string]any{}
		for legacy, field := range legacyPricingFields {
			value, ok := model[legacy]
			if !ok {
				continue
			}
			pricing[field] = value
			delete(model, legacy)
			changed = true
		}
		if len(pricing) > 0 {
			model["pricing"] = pricing
		}
	} else {
		for legacy := range legacyPricingFields {
			if _, ok := model[legacy]; ok {
				delete(model, legacy)
				changed = true
			}
		}
	}

	if value, ok := model["supports_attachments"]; ok {
		delete(model, "supports_attachments")
		capabilities, ok := model["capabilities"].(map[string]any)
		if !ok {
			capabilities = map[string]any{}
			model["capabilities"] = capabilities
		}
		if _, ok := capabilities["vision"]; !ok {
			capabilities["vision"] = value
		}
		changed = true
	}

	canReason, hasCanReason := model["can_reason"]
	levels, hasLevels := model["reasoning_levels"]
	defaultEffort, hasDefaultEffort := model["default_reasoning_effort"]
	for _, legacy := range []string{"can_reason", "reasoning_levels", "default_reasoning_effort"} {
		if _, ok := model[legacy]; ok {
			delete(model, legacy)
			changed = true
		}
	}
	if _, ok := model["reasoning"]; !ok && (hasCanReason || hasLevels || hasDefaultEffort) {
		reasoning := map[string]any{}
		if can, ok := canReason.(bool); ok && !can {
			reasoning["thinking"] = catwalk.ThinkingNever
		} else {
			reasoning["thinking"] = catwalk.ThinkingToggleable
			if raw, ok := levels.([]any); ok && len(raw) > 0 {
				values := make([]string, 0, len(raw))
				for _, level := range raw {
					if value, ok := level.(string); ok {
						values = append(values, value)
					}
				}
				if len(values) > 0 {
					effortLevels := make([]any, 0, len(values))
					for _, level := range catwalk.NewEffortLevels(values...) {
						effortLevels = append(effortLevels, map[string]any{
							"value":   level.Value,
							"display": level.Display,
						})
					}
					reasoning["effort_levels"] = effortLevels
					if effort, ok := defaultEffort.(string); ok && effort != "" {
						reasoning["default_effort_level"] = effort
					}
				}
			}
		}
		model["reasoning"] = reasoning
	}

	return changed
}

// migrateV2Catalog rewrites a cache file written before catwalk v3 (a
// provider list, a single provider, or a discovery entry, each carrying a
// "models" list) onto the v3 model schema, so a cache that outlived the
// upgrade still decodes with its prices and reasoning controls when the
// network is down. Data already in the v3 shape, or not shaped like a
// catalog, comes back unchanged. Numbers are kept exact.
func migrateV2Catalog(data []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return data
	}
	changed := false
	migrate := func(v any) {
		entry, ok := v.(map[string]any)
		if !ok {
			return
		}
		models, ok := entry["models"].([]any)
		if !ok {
			return
		}
		for _, raw := range models {
			if model, ok := raw.(map[string]any); ok && MigrateV2Model(model) {
				changed = true
			}
		}
	}
	switch v := root.(type) {
	case []any:
		for _, entry := range v {
			migrate(entry)
		}
	case map[string]any:
		migrate(v)
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(root)
	if err != nil {
		return data
	}
	return out
}
