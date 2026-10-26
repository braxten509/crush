package config

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

// TinfoilProviderID is Tinfoil's private inference API, billed per use
// (owner, 2026-10-26). Like the Codex API it is always listed, key or not,
// so Settings → Accounts can add the key. Its models come from Tinfoil's
// own list each time the config loads, and every request goes only to a
// server that proves it is Tinfoil's secure server (providers.tinfoil.go).
const TinfoilProviderID = "tinfoil"

const (
	tinfoilBaseURL          = "https://inference.tinfoil.sh/v1"
	tinfoilDefaultModelsURL = tinfoilBaseURL + "/models"
)

var (
	// listTinfoil is off in tests, so their provider lists hold only what
	// they set up.
	listTinfoil      = !testing.Testing()
	tinfoilModelsURL = tinfoilDefaultModelsURL
)

// TinfoilReasoning is how one Tinfoil model's thinking is switched, as
// Tinfoil's model list gives it: the request body fields that turn it on
// (with "$EFFORT" standing for the effort) and off, and the effort Tinfoil
// sends for each of Crush's levels.
type TinfoilReasoning struct {
	EffortMap map[string]string
	Enable    map[string]any
	Disable   map[string]any
}

var tinfoilReasoning struct {
	sync.RWMutex
	byModel map[string]TinfoilReasoning
}

// TinfoilThinking is how the Tinfoil model id switches its thinking, if it
// has a switch.
func TinfoilThinking(modelID string) (TinfoilReasoning, bool) {
	tinfoilReasoning.RLock()
	defer tinfoilReasoning.RUnlock()
	r, ok := tinfoilReasoning.byModel[modelID]
	return r, ok
}

// IsTinfoilProvider reports whether pc is a Tinfoil account: the Tinfoil
// entry, another account copied from it ("tinfoil-2"), or any entry that
// talks to tinfoil.sh.
func IsTinfoilProvider(pc ProviderConfig) bool {
	if pc.ID == TinfoilProviderID {
		return true
	}
	if n, ok := strings.CutPrefix(pc.ID, TinfoilProviderID+"-"); ok {
		if _, err := strconv.Atoi(n); err == nil {
			return true
		}
	}
	u, err := url.Parse(pc.BaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "tinfoil.sh" || strings.HasSuffix(host, ".tinfoil.sh")
}

// tinfoilModel is one entry of Tinfoil's /v1/models.
type tinfoilModel struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Type          string             `json:"type"`
	ContextWindow int64              `json:"context_window"`
	Endpoints     []string           `json:"endpoints"`
	Multimodal    bool               `json:"multimodal"`
	Reasoning     bool               `json:"reasoning"`
	ToolCalling   bool               `json:"tool_calling"`
	Intelligence  map[string]float64 `json:"intelligence"`
	Pricing       struct {
		In     float64 `json:"inputTokenPricePer1M"`
		Out    float64 `json:"outputTokenPricePer1M"`
		Cached float64 `json:"cachedInputTokenPricePer1M"`
	} `json:"pricing"`
	ReasoningParams *struct {
		EffortMap map[string]string `json:"effort_map"`
		Params    map[string]struct {
			Enable  map[string]any `json:"enable"`
			Disable map[string]any `json:"disable"`
		} `json:"params"`
	} `json:"reasoning_params"`
}

// tinfoilLevels are the effort levels Tinfoil's list may name, in order.
var tinfoilLevels = []string{"low", "medium", "high", "xhigh", "max"}

// LoadTinfoilModels reads Tinfoil's /v1/models body into the models Crush
// can run (chat models that use tools over chat completions) and keeps
// each one's thinking switch for TinfoilThinking.
func LoadTinfoilModels(body []byte) ([]catwalk.Model, error) {
	var list struct {
		Data []tinfoilModel `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("reading Tinfoil's model list: %w", err)
	}
	var models []catwalk.Model
	reasoning := make(map[string]TinfoilReasoning)
	for _, e := range list.Data {
		if e.Type != "chat" || !e.ToolCalling || !slices.Contains(e.Endpoints, "/v1/chat/completions") {
			continue
		}
		m := catwalk.Model{
			ID: e.ID, Name: e.Name,
			CostPer1MIn: e.Pricing.In, CostPer1MOut: e.Pricing.Out, CostPer1MOutCached: e.Pricing.Cached,
			ContextWindow: e.ContextWindow, DefaultMaxTokens: min(32768, e.ContextWindow/4),
			CanReason: e.Reasoning, SupportsImages: e.Multimodal,
		}
		if e.Reasoning {
			for _, level := range tinfoilLevels {
				if _, ok := e.Intelligence[level]; ok {
					m.ReasoningLevels = append(m.ReasoningLevels, level)
				}
			}
			if slices.Contains(m.ReasoningLevels, "medium") {
				m.DefaultReasoningEffort = "medium"
			}
		}
		if p := e.ReasoningParams; e.Reasoning && p != nil {
			if chat, ok := p.Params["/v1/chat/completions"]; ok {
				reasoning[e.ID] = TinfoilReasoning{EffortMap: p.EffortMap, Enable: chat.Enable, Disable: chat.Disable}
			}
		}
		models = append(models, m)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("Tinfoil's model list has no chat models")
	}
	tinfoilReasoning.Lock()
	tinfoilReasoning.byModel = reasoning
	tinfoilReasoning.Unlock()
	return models, nil
}

func tinfoilCachePath() string {
	return filepath.Join(GlobalCacheDir(), "tinfoil-models.json")
}

// tinfoilModels reads Tinfoil's model list, saving it for the next load;
// when Tinfoil can't be reached it uses the last list saved.
func tinfoilModels(ctx context.Context) []catwalk.Model {
	body, err := fetchTinfoilModels(ctx)
	if err == nil {
		models, err := LoadTinfoilModels(body)
		if err == nil {
			path := tinfoilCachePath()
			if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
				_ = os.WriteFile(path, body, 0o644)
			}
			return models
		}
	}
	slog.Warn("Tinfoil's model list could not be read; using the saved one", "error", err)
	saved, readErr := os.ReadFile(tinfoilCachePath())
	if readErr != nil {
		return nil
	}
	models, _ := LoadTinfoilModels(saved)
	return models
}

func fetchTinfoilModels(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tinfoilModelsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tinfoil's model list: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// addTinfoilProviders lists the Tinfoil provider, keeping the saved key,
// name and other settings, and gives every Tinfoil account the current
// models. A disabled entry is left alone.
func (c *Config) addTinfoilProviders(ctx context.Context) {
	if cfg, _ := c.Providers.Get(TinfoilProviderID); !cfg.Disable {
		cfg.Name = cmp.Or(cfg.Name, "Tinfoil")
		c.Providers.Set(TinfoilProviderID, cfg)
	}
	var accounts []ProviderConfig
	for id, pc := range c.Providers.Seq2() {
		pc.ID = id
		if !pc.Disable && IsTinfoilProvider(pc) {
			accounts = append(accounts, pc)
		}
	}
	if len(accounts) == 0 {
		return
	}
	models := tinfoilModels(ctx)
	off := false
	for _, pc := range accounts {
		pc.Type = catwalk.TypeOpenAICompat
		pc.BaseURL = cmp.Or(pc.BaseURL, tinfoilBaseURL)
		pc.AutoDiscoverModels = &off
		if len(models) > 0 {
			pc.Models = models
		}
		c.Providers.Set(pc.ID, pc)
	}
}
