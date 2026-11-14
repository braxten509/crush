package config

import (
	"strings"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// AnthropicProgressUpdatesBeta is the beta flag the Anthropic API needs
// before it accepts the "updates" thinking display.
const AnthropicProgressUpdatesBeta = "thinking-display-updates-2026-08-18"

// AnthropicThinkingBindingBeta is the beta flag the Anthropic API needs
// before it accepts thinking.block_binding. Claude 5 binds each thinking
// block to the system prompt and tools it was written under; when a chat
// comes back after either changed (new skills, a new day), the API refused
// the whole chat. With "drop_block" it drops the stale blocks instead.
const AnthropicThinkingBindingBeta = "thinking-binding-controls-2026-08-01"

// AnthropicThinkingDisplayUpdates returns the text of the model's progress
// updates (the short notes it writes before a tool call) and leaves its
// reasoning empty. The "summarized" display returns the notes empty, so
// the user never saw them and the progress check counted the agent silent.
const AnthropicThinkingDisplayUpdates = "updates"

// progressUpdateFamilies are the Claude families that write progress
// updates (thinking docs, "Progress updates").
var progressUpdateFamilies = []string{
	"claude-opus-5",
	"claude-sonnet-5",
	"claude-fable-5",
	"claude-mythos-5",
}

// AnthropicProgressUpdates reports whether Crush asks the Anthropic API
// for the model's progress updates.
func AnthropicProgressUpdates(providerID, modelID string) bool {
	if providerID != string(catwalk.InferenceProviderAnthropic) {
		return false
	}
	model := strings.ToLower(modelID)
	for _, family := range progressUpdateFamilies {
		if strings.Contains(model, family) {
			return true
		}
	}
	return false
}
