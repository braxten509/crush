// Package noninteractive holds the parts of a `crush run` turn that are
// shared by the in-process app and the client/server mode: resolving the
// --model flags, restoring a continued session's model, applying reasoning
// effort and fast mode, and driving the spinner and terminal progress bar
// around the mode-specific output stream.
package noninteractive

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/config"
	xstrings "github.com/charmbracelet/x/exp/strings"
)

// ModelMatch is a configured model that a model string resolved to.
type ModelMatch struct {
	Provider string
	ModelID  string
}

// parseModelString splits a model string into an optional provider filter
// and a model ID. Accepted forms are "model", "provider/model" and
// "provider/vendor/model". The part before the first slash is a provider
// filter only when it names a configured provider (compared
// case-insensitively; the configured spelling is returned). Otherwise the
// whole string is the model ID, since model IDs may contain slashes (for
// example OpenRouter's "anthropic/claude-x").
func parseModelString(providers map[string]config.ProviderConfig, modelStr string) (providerFilter, modelID string) {
	prefix, rest, ok := strings.Cut(modelStr, "/")
	if !ok {
		return "", modelStr
	}
	if _, found := providers[prefix]; found {
		return prefix, rest
	}
	for name := range providers {
		if strings.EqualFold(name, prefix) {
			return name, rest
		}
	}
	return "", modelStr
}

// findModels returns every enabled configured model matching modelStr.
// Model IDs are compared case-insensitively. When the string starts with a
// provider name but that provider has no such model, the whole string is
// tried as a model ID across all providers, so a slash-containing ID whose
// vendor prefix happens to be a provider name still resolves.
func findModels(providers map[string]config.ProviderConfig, modelStr string) []ModelMatch {
	if modelStr == "" {
		return nil
	}
	providerFilter, modelID := parseModelString(providers, modelStr)
	matches := matchModels(providers, providerFilter, modelID)
	if len(matches) == 0 && providerFilter != "" {
		matches = matchModels(providers, "", modelStr)
	}
	return matches
}

func matchModels(providers map[string]config.ProviderConfig, providerFilter, modelID string) []ModelMatch {
	if modelID == "" {
		return nil
	}
	var matches []ModelMatch
	for name, provider := range providers {
		if provider.Disable {
			continue
		}
		if providerFilter != "" && name != providerFilter {
			continue
		}
		for _, m := range provider.Models {
			if strings.EqualFold(m.ID, modelID) {
				matches = append(matches, ModelMatch{Provider: name, ModelID: m.ID})
			}
		}
	}
	return matches
}

// validateMatches returns the single match, or an error naming the model
// when there is none or more than one.
func validateMatches(matches []ModelMatch, modelStr, label string) (ModelMatch, error) {
	switch {
	case len(matches) == 0:
		return ModelMatch{}, fmt.Errorf("%s model %q not found", label, modelStr)
	case len(matches) > 1:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Provider
		}
		return ModelMatch{}, fmt.Errorf(
			"%s model: model %q found in multiple providers: %s. Please specify provider using 'provider/model' format",
			label,
			modelStr,
			xstrings.EnglishJoin(names, true),
		)
	}
	return matches[0], nil
}

// ResolveModel resolves a --model style string against the configured
// providers. label ("large" or "small") prefixes error messages.
func ResolveModel(providers map[string]config.ProviderConfig, modelStr, label string) (ModelMatch, error) {
	return validateMatches(findModels(providers, modelStr), modelStr, label)
}
