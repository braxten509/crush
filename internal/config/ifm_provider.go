package config

import (
	"slices"

	"charm.land/catwalk/pkg/catwalk"
)

const IFMProviderID = "ifm"

// withIFMProvider supplies IFM until it is included in Catwalk's catalog.
// Once Catwalk carries it, its maintained entry takes precedence.
func withIFMProvider(list []catwalk.Provider) []catwalk.Provider {
	if slices.ContainsFunc(list, func(p catwalk.Provider) bool { return p.ID == IFMProviderID }) {
		return list
	}
	const modelID = "IFM/K2-Horizon-375B-A23B"
	return append(list, catwalk.Provider{
		ID: IFMProviderID, Name: "IFM", Type: catwalk.TypeOpenAICompat,
		APIEndpoint: "https://api.ifm.ai/v1", APIKey: "$IFM_API_KEY",
		DefaultLargeModelID: modelID, DefaultSmallModelID: modelID,
		Models: []catwalk.Model{{
			ID: modelID, Name: "K2 Horizon 375B A23B",
			ContextWindow: 524288, DefaultMaxTokens: 32768,
			CanReason: true, ReasoningLevels: []string{"low", "medium", "high"},
			DefaultReasoningEffort: "high",
		}},
	})
}
