package config

import "slices"

// modelEffortKey is a model's key in Config.ModelEfforts.
func modelEffortKey(provider, model string) string {
	if provider == "" || model == "" {
		return ""
	}
	return provider + ":" + model
}

// ModelEffort is the reasoning effort to use when the model is chosen
// again: the one last used with it while the model still offers it, else
// the model's default. "" for a model the config doesn't know.
func (c *Config) ModelEffort(provider, model string) string {
	m := c.GetModel(provider, model)
	if m == nil {
		return ""
	}
	if effort := c.ModelEfforts[modelEffortKey(provider, model)]; effort != "" && slices.Contains(m.ReasoningLevels, effort) {
		return effort
	}
	return m.DefaultReasoningEffort
}

// Remembered is the selection that chooses the model again: its default
// reply length and the effort last used with it.
func (c *Config) Remembered(provider, model string) SelectedModel {
	selected := SelectedModel{Provider: provider, Model: model, ReasoningEffort: c.ModelEffort(provider, model)}
	if m := c.GetModel(provider, model); m != nil {
		selected.MaxTokens = m.DefaultMaxTokens
	}
	return selected
}
