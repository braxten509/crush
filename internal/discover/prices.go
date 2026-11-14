package discover

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Some providers' own model lists (Anthropic's, Google's) carry no prices,
// and Crush's catalog only knows the models it shipped with, so a model
// released after it was priced at nothing and its chats showed no cost.
// OpenRouter's public model list carries the providers' prices, needs no
// key, and is updated as models come out; it fills the prices a listed
// model is missing.

// publicPricesURL is OpenRouter's public model list. Tests leave it empty
// so they never reach the network.
var publicPricesURL = func() string {
	if testing.Testing() {
		return ""
	}
	return "https://openrouter.ai/api/v1/models"
}()

const (
	// publicPricesTTL is how long one read of the list is kept.
	publicPricesTTL = 24 * time.Hour
	// publicPricesRetry is how long a failed read waits before the next try.
	publicPricesRetry = 10 * time.Minute
)

var publicPrices struct {
	sync.Mutex
	byID    map[string]catwalk.Pricing
	readAt  time.Time
	retryAt time.Time
}

// fillMissingPrices gives each model without any price the price its
// provider charges for it, from OpenRouter's public list. A model with
// any price of its own, or one the list does not name, is left as it is.
func fillMissingPrices(ctx context.Context, models []catwalk.Model) []catwalk.Model {
	missing := false
	for _, m := range models {
		if m.Pricing == (catwalk.Pricing{}) {
			missing = true
			break
		}
	}
	if !missing {
		return models
	}
	prices := publicPriceList(ctx)
	if len(prices) == 0 {
		return models
	}
	for i, m := range models {
		if m.Pricing != (catwalk.Pricing{}) {
			continue
		}
		if p, ok := prices[priceKey(m.ID)]; ok {
			models[i].Pricing = p
		}
	}
	return models
}

// publicPriceList returns the prices by model key, reading the list at
// most once a day and not again for a while after a failed read.
func publicPriceList(ctx context.Context) map[string]catwalk.Pricing {
	if publicPricesURL == "" {
		return nil
	}
	publicPrices.Lock()
	defer publicPrices.Unlock()
	now := time.Now()
	if publicPrices.byID != nil && now.Sub(publicPrices.readAt) < publicPricesTTL {
		return publicPrices.byID
	}
	if now.Before(publicPrices.retryAt) {
		return publicPrices.byID
	}
	byID, err := readPublicPrices(ctx, publicPricesURL)
	if err != nil || len(byID) == 0 {
		publicPrices.retryAt = now.Add(publicPricesRetry)
		return publicPrices.byID
	}
	publicPrices.byID = byID
	publicPrices.readAt = now
	return byID
}

func readPublicPrices(ctx context.Context, url string) (map[string]catwalk.Pricing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var list struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return publicPricesByKey(list.Data), nil
}

// publicPricesByKey indexes the list's prices by priceKey. Variants such
// as "anthropic/claude-fable-5.2:batch" carry other prices and are left
// out, as are entries with no price.
func publicPricesByKey(entries []openRouterModel) map[string]catwalk.Pricing {
	byID := make(map[string]catwalk.Pricing, len(entries))
	for _, e := range entries {
		if e.ID == "" || strings.Contains(e.ID, ":") {
			continue
		}
		p := openRouterPricing(e)
		if p == (catwalk.Pricing{}) || p.Input < 0 || p.Output < 0 {
			continue
		}
		key := priceKey(e.ID)
		if _, taken := byID[key]; !taken {
			byID[key] = p
		}
	}
	return byID
}

var priceDateSuffix = regexp.MustCompile(`-\d{8}$`)

// priceKey is the form both lists' names share: no vendor in front, no
// release date at the end, lower case, and dots as dashes, so Anthropic's
// "claude-fable-5-2" meets OpenRouter's "anthropic/claude-fable-5.2".
func priceKey(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id = strings.TrimPrefix(id, "models/")
	id = priceDateSuffix.ReplaceAllString(id, "")
	return strings.ReplaceAll(id, ".", "-")
}
