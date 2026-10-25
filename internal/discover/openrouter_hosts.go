package discover

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenRouter's hosts for a model, cheapest first (owner, 2026-10-22): Crush
// sends this as the request's provider order, so a host whose input and
// output prices tie with another's wins on its cheaper cache reads, which
// OpenRouter's own price sort does not promise. The order is read from the
// public /models/{id}/endpoints list in the background and kept for an
// hour, so no request waits for it.

// openRouterHostsTTL is how long a model's host order is kept.
const openRouterHostsTTL = time.Hour

type openRouterPrices struct {
	Prompt    string `json:"prompt"`
	Output    string `json:"completion"`
	CacheRead string `json:"input_cache_read"`
}

type openRouterEndpoint struct {
	Tag     string `json:"tag"`
	Pricing struct {
		openRouterPrices
		// Overrides are time-of-day prices: utc_start to utc_end as HHMM
		// (an end of 0 is midnight), on utc_days or every day.
		Overrides []struct {
			openRouterPrices
			Start int      `json:"utc_start"`
			End   int      `json:"utc_end"`
			Days  []string `json:"utc_days"`
		} `json:"overrides"`
	} `json:"pricing"`
}

// openRouterHostOrder ranks an /endpoints answer's hosts at now: by input
// plus output price per token, then by cache-read price. Unreadable
// answers rank nothing.
func openRouterHostOrder(body []byte, now time.Time) []string {
	var answer struct {
		Data struct {
			Endpoints []openRouterEndpoint `json:"endpoints"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return nil
	}
	type host struct {
		tag              string
		price, cacheRead float64
	}
	clock := now.UTC().Hour()*100 + now.UTC().Minute()
	day := strings.ToLower(now.UTC().Weekday().String())
	var hosts []host
	for _, e := range answer.Data.Endpoints {
		if e.Tag == "" {
			continue
		}
		prices := e.Pricing.openRouterPrices
		for _, o := range e.Pricing.Overrides {
			end := o.End
			if end == 0 {
				end = 2400
			}
			if clock >= o.Start && clock < end && (len(o.Days) == 0 || slices.Contains(o.Days, day)) {
				prices = o.openRouterPrices
				break
			}
		}
		hosts = append(hosts, host{tag: e.Tag, price: perToken(prices.Prompt) + perToken(prices.Output), cacheRead: perToken(prices.CacheRead)})
	}
	slices.SortStableFunc(hosts, func(a, b host) int {
		return cmp.Or(cmp.Compare(a.price, b.price), cmp.Compare(a.cacheRead, b.cacheRead))
	})
	tags := make([]string, len(hosts))
	for i, h := range hosts {
		tags[i] = h.tag
	}
	return tags
}

func perToken(text string) float64 {
	price, err := strconv.ParseFloat(text, 64)
	if err != nil || price < 0 {
		return 0
	}
	return price
}

// openRouterHosts keeps each model's host order.
type openRouterHosts struct {
	mu   sync.Mutex
	ttl  time.Duration
	kept map[string]openRouterHostsEntry
}

type openRouterHostsEntry struct {
	order   []string
	readAt  time.Time
	reading bool
}

func newOpenRouterHosts(ttl time.Duration) *openRouterHosts {
	return &openRouterHosts{ttl: ttl, kept: map[string]openRouterHostsEntry{}}
}

var sharedOpenRouterHosts = newOpenRouterHosts(openRouterHostsTTL)

// OpenRouterHostOrder is model's host order on the OpenRouter at baseURL,
// cheapest first, as last read; nil until the first read is done. A
// missing or stale order is read again in the background.
func OpenRouterHostOrder(ctx context.Context, baseURL, model string) []string {
	return sharedOpenRouterHosts.Order(ctx, baseURL, model)
}

func (h *openRouterHosts) Order(ctx context.Context, baseURL, model string) []string {
	key := baseURL + " " + model
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.kept[key]
	if !e.reading && time.Since(e.readAt) > h.ttl {
		e.reading = true
		h.kept[key] = e
		go h.read(context.WithoutCancel(ctx), key, baseURL, model)
	}
	return slices.Clone(e.order)
}

func (h *openRouterHosts) read(ctx context.Context, key, baseURL, model string) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var order []string
	url := strings.TrimRight(baseURL, "/") + "/models/" + model + "/endpoints"
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil); err == nil {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				order = openRouterHostOrder(body, time.Now())
			}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.kept[key]
	e.reading = false
	e.readAt = time.Now()
	if order != nil {
		e.order = order
	}
	h.kept[key] = e
}
