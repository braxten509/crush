package providers

import "charm.land/catwalk/v3/pkg/catwalk"

// legacyTypes maps provider type values from the catwalk v2 schema onto the
// v3 endpoint types that describe the same API format, so configs written
// before the migration keep working. Custom provider types with registered
// enrichers (ollama, litellm, ...) and the agent CLI types are not listed
// here and pass through unchanged.
var legacyTypes = map[catwalk.Type]catwalk.Type{
	"openai":        catwalk.TypeResponses,
	"openai-compat": catwalk.TypeCompletions,
	"openrouter":    catwalk.TypeCompletions,
	"vercel":        catwalk.TypeCompletions,
	"anthropic":     catwalk.TypeMessages,
	"azure":         catwalk.TypeResponses,
	"bedrock":       catwalk.TypeMessages,
	"google":        catwalk.TypeCompletions,
	"google-vertex": catwalk.TypeCompletions,
}

// NormalizeType maps a catwalk v2 provider type onto the v3 endpoint type
// describing the same API format. Types that are already v3 or are not
// legacy names pass through unchanged. catwalk's v3 data still carries v2
// names for some providers (OpenAI, MiniMax, ...).
func NormalizeType(t catwalk.Type) catwalk.Type {
	if mapped, ok := legacyTypes[t]; ok {
		return mapped
	}
	return t
}

// legacySDKTypes are the catwalk v2 provider types that named an SDK of
// their own rather than only an API format. A custom provider keeps one of
// these as written, so the provider builder can still pick the SDK it
// named; NormalizeType gives the endpoint format it speaks.
var legacySDKTypes = map[catwalk.Type]bool{
	"azure":         true,
	"bedrock":       true,
	"google":        true,
	"google-vertex": true,
	"openrouter":    true,
	"vercel":        true,
}

// IsLegacySDKType reports whether t is a catwalk v2 type that named its own
// SDK (see legacySDKTypes).
func IsLegacySDKType(t catwalk.Type) bool {
	return legacySDKTypes[t]
}
