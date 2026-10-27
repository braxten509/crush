package config

import (
	"cmp"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

// OrcaRouterProviderID is OrcaRouter, an OpenAI-compatible router billed
// per use (owner, 2026-10-27). Like the Codex API it is always listed, key
// or not, so Settings → Accounts can add the key. Like OpenRouter its
// models are the ones saved from its catalog with the app's "+ Add model".
const OrcaRouterProviderID = "orcarouter"

const orcaRouterBaseURL = "https://api.orcarouter.ai/v1"

// listOrcaRouter is off in tests, so their provider lists hold only what
// they set up.
var listOrcaRouter = !testing.Testing()

// IsOrcaRouterProvider reports whether pc is an OrcaRouter account: the
// OrcaRouter entry, another account copied from it ("orcarouter-2"), or any
// entry that talks to orcarouter.ai.
func IsOrcaRouterProvider(pc ProviderConfig) bool {
	if pc.ID == OrcaRouterProviderID {
		return true
	}
	if n, ok := strings.CutPrefix(pc.ID, OrcaRouterProviderID+"-"); ok {
		if _, err := strconv.Atoi(n); err == nil {
			return true
		}
	}
	u, err := url.Parse(pc.BaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "orcarouter.ai" || strings.HasSuffix(host, ".orcarouter.ai")
}

// keepsWithoutModels reports whether a provider stays with no saved models:
// OpenRouter and OrcaRouter take theirs from a catalog in the app, which
// shows its "+" there.
func keepsWithoutModels(pc ProviderConfig) bool {
	return pc.Type == catwalk.TypeOpenRouter || IsOrcaRouterProvider(pc)
}

// addOrcaRouterProviders lists the OrcaRouter provider, keeping the saved
// key, name, models and other settings, and fills in every OrcaRouter
// account's endpoint. A disabled entry is left alone.
func (c *Config) addOrcaRouterProviders() {
	if cfg, _ := c.Providers.Get(OrcaRouterProviderID); !cfg.Disable {
		cfg.Name = cmp.Or(cfg.Name, "OrcaRouter")
		c.Providers.Set(OrcaRouterProviderID, cfg)
	}
	off := false
	for id, pc := range c.Providers.Seq2() {
		pc.ID = id
		if pc.Disable || !IsOrcaRouterProvider(pc) {
			continue
		}
		pc.Type = catwalk.TypeOpenAICompat
		pc.BaseURL = cmp.Or(pc.BaseURL, orcaRouterBaseURL)
		pc.AutoDiscoverModels = &off
		c.Providers.Set(id, pc)
	}
}
