package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Every API model's price comes from a live source (owner, 2026-11-15):
// Crush's shipped catalog keeps the prices it was released with, so a
// price the maker changed later went on being counted at the old rate,
// and a model released after it had no price at all. OpenRouter lists,
// with no key, every endpoint that serves a model and its price, the
// maker's own API among them; that endpoint's price is what the maker
// bills. The config lays it over the shipped prices of the models a maker
// sells itself (see config's live_prices.go).
//
// The maker's own endpoint is used, not the price OpenRouter shows first:
// for an open-weight model that one is the cheapest host's (Kimi K3 at
// $0.19 in from Wafer, where Moonshot charges $3), not the maker's.
//
// Nothing here is read while Crush starts: the last read is saved in a
// file and used at once, and RefreshLivePrices reads a fresh one in the
// background, after which the config reloads.

// publicModelsURL is OpenRouter's public model list. Tests leave it empty
// so they never reach the network.
var publicModelsURL = func() string {
	if testing.Testing() {
		return ""
	}
	return "https://openrouter.ai/api/v1/models"
}()

const (
	// livePricesTTL is how long one look-up of a model's price is kept.
	livePricesTTL = 24 * time.Hour
	// livePricesRetry is how long a failed read waits before the next try.
	livePricesRetry = 10 * time.Minute
	// livePricesTimeout bounds one whole refresh.
	livePricesTimeout = time.Minute
	// livePricesParallel is how many endpoint lists are read at once.
	livePricesParallel = 4
)

// makerEndpointTags maps an OpenRouter vendor (the maker a model is filed
// under) to the tag of the maker's own endpoint in a model's endpoint
// list. A tag with a "/" after it ("openai/flex", "google-ai-studio/
// priority") is another service tier and is never taken.
var makerEndpointTags = map[string]string{
	"anthropic":  "anthropic",
	"openai":     "openai",
	"google":     "google-ai-studio",
	"x-ai":       "xai",
	"deepseek":   "deepseek",
	"moonshotai": "moonshotai",
	"mistralai":  "mistral",
	"minimax":    "minimax",
}

// LivePriceWant names a model whose maker's live price is wanted: the
// maker's OpenRouter vendor and the model's id as its provider lists it.
type LivePriceWant struct {
	Vendor string
	ID     string
}

// livePrices is the last read. Prices holds the makers' prices found;
// Looked holds when each wanted model was last looked up, found or not, so
// a model the maker does not serve on OpenRouter is not asked about on
// every start.
type livePrices struct {
	Prices map[string]catwalk.Pricing `json:"prices"`
	Looked map[string]time.Time       `json:"looked"`
}

var publicPrices struct {
	sync.Mutex
	read    livePrices
	retryAt time.Time
	// fileRead marks that the saved copy was looked at, so a missing or
	// broken file is not read again on every config load.
	fileRead bool
}

// refreshMu lets one refresh run at a time. It is apart from publicPrices'
// lock so a config load reading the prices never waits on the network.
var refreshMu sync.Mutex

// LivePrices returns the makers' prices last read, by LivePriceKey,
// without touching the network: the copy in memory, else the one saved at
// path (empty: no saved copy). Nil when nothing was read yet.
func LivePrices(path string) map[string]catwalk.Pricing {
	publicPrices.Lock()
	defer publicPrices.Unlock()
	loadSavedPrices(path)
	return publicPrices.read.Prices
}

// RefreshLivePrices looks up the makers' prices of the wanted models that
// were never looked up or were looked up a day ago or more, saves them at
// path, and reports whether any price changed. After a failed read the
// next try waits a while; the old prices stay meanwhile.
func RefreshLivePrices(ctx context.Context, path string, wants []LivePriceWant) (bool, error) {
	if publicModelsURL == "" {
		return false, nil
	}
	refreshMu.Lock()
	defer refreshMu.Unlock()

	now := time.Now()
	publicPrices.Lock()
	loadSavedPrices(path)
	due := map[string]LivePriceWant{}
	for _, want := range wants {
		if makerEndpointTags[want.Vendor] == "" {
			continue
		}
		key := LivePriceKey(want.Vendor, want.ID)
		if looked, ok := publicPrices.read.Looked[key]; !ok || now.Sub(looked) >= livePricesTTL {
			due[key] = want
		}
	}
	waiting := now.Before(publicPrices.retryAt)
	publicPrices.Unlock()
	if len(due) == 0 || waiting {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, livePricesTimeout)
	defer cancel()
	found, looked, err := lookUpMakerPrices(ctx, due)

	publicPrices.Lock()
	defer publicPrices.Unlock()
	if err != nil {
		publicPrices.retryAt = now.Add(livePricesRetry)
	}
	if len(looked) == 0 {
		return false, err
	}
	next := livePrices{
		Prices: maps.Clone(publicPrices.read.Prices),
		Looked: maps.Clone(publicPrices.read.Looked),
	}
	if next.Prices == nil {
		next.Prices = map[string]catwalk.Pricing{}
	}
	if next.Looked == nil {
		next.Looked = map[string]time.Time{}
	}
	for key := range looked {
		next.Looked[key] = now
		if p, ok := found[key]; ok {
			next.Prices[key] = p
		} else {
			delete(next.Prices, key)
		}
	}
	changed := !maps.Equal(next.Prices, publicPrices.read.Prices)
	publicPrices.read = next
	if path != "" {
		if saveErr := savePrices(path, next); saveErr != nil && err == nil {
			err = saveErr
		}
	}
	return changed, err
}

// lookUpMakerPrices reads OpenRouter's model list for the models' ids
// there, then each due model's endpoint list for its maker's price. It
// returns the prices found and every key it got an answer for; a model
// OpenRouter does not list, or whose maker does not serve it there, is
// answered with no price. err is the first failed read.
func lookUpMakerPrices(ctx context.Context, due map[string]LivePriceWant) (map[string]catwalk.Pricing, map[string]bool, error) {
	ids, err := readPublicModelIDs(ctx, publicModelsURL)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	found := map[string]catwalk.Pricing{}
	looked := map[string]bool{}
	var firstErr error
	slots := make(chan struct{}, livePricesParallel)
	for key, want := range due {
		id, ok := ids[key]
		if !ok {
			// Lookups already started write the same maps.
			mu.Lock()
			looked[key] = true
			mu.Unlock()
			continue
		}
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			price, ok, err := readMakerPrice(ctx, id, makerEndpointTags[want.Vendor])
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			looked[key] = true
			if ok {
				found[key] = price
			}
		})
	}
	wg.Wait()
	return found, looked, firstErr
}

// readPublicModelIDs maps LivePriceKey to OpenRouter's id for every model
// in its public list. Variants such as "anthropic/claude-fable-5.2:batch"
// carry other prices and are left out.
func readPublicModelIDs(ctx context.Context, listURL string) (map[string]string, error) {
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, listURL, &list); err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(list.Data))
	for _, e := range list.Data {
		vendor, _, ok := strings.Cut(e.ID, "/")
		if !ok || vendor == "" || strings.Contains(e.ID, ":") {
			continue
		}
		key := LivePriceKey(vendor, e.ID)
		if _, taken := ids[key]; !taken {
			ids[key] = e.ID
		}
	}
	return ids, nil
}

// readMakerPrice is the price of the model's endpoint tagged tag (the
// maker's own), and whether the model has one with a price.
func readMakerPrice(ctx context.Context, id, tag string) (catwalk.Pricing, bool, error) {
	endpointsURL := publicModelsURL + "/" + (&url.URL{Path: id}).EscapedPath() + "/endpoints"
	var list struct {
		Data struct {
			Endpoints []struct {
				Tag     string `json:"tag"`
				Pricing struct {
					Prompt          string `json:"prompt"`
					Completion      string `json:"completion"`
					InputCacheRead  string `json:"input_cache_read"`
					InputCacheWrite string `json:"input_cache_write"`
				} `json:"pricing"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := getJSON(ctx, endpointsURL, &list); err != nil {
		return catwalk.Pricing{}, false, err
	}
	for _, e := range list.Data.Endpoints {
		if e.Tag != tag {
			continue
		}
		p := catwalk.Pricing{
			Input:       perMillion(e.Pricing.Prompt),
			Output:      perMillion(e.Pricing.Completion),
			CacheHit:    perMillion(e.Pricing.InputCacheRead),
			CacheCreate: perMillion(e.Pricing.InputCacheWrite),
		}
		if p.Input <= 0 && p.Output <= 0 {
			return catwalk.Pricing{}, false, nil
		}
		return p, true, nil
	}
	return catwalk.Pricing{}, false, nil
}

func getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reading %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// loadSavedPrices fills the memory copy from the file at path once. The
// caller holds publicPrices' lock.
func loadSavedPrices(path string) {
	if publicPrices.read.Looked != nil || publicPrices.fileRead || path == "" {
		return
	}
	publicPrices.fileRead = true
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved livePrices
	if json.Unmarshal(data, &saved) != nil || saved.Looked == nil {
		return
	}
	publicPrices.read = saved
}

// savePrices writes the file whole, through a temporary file, so a reader
// never sees half of it.
func savePrices(path string, saved livePrices) error {
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".live_prices-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var priceDateSuffix = regexp.MustCompile(`-\d{8}$`)

// LivePriceKey is the key of a model's price: the vendor OpenRouter files
// the model under (its maker, "anthropic", "google", "openai", ...) and
// the model's name in the form both lists share: no vendor in front, no
// release date at the end, lower case, and dots as dashes, so Anthropic's
// "claude-fable-5-2" meets OpenRouter's "anthropic/claude-fable-5.2". The
// vendor is part of the key so a model is only ever priced at its own
// maker's price.
func LivePriceKey(vendor, id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id = priceDateSuffix.ReplaceAllString(id, "")
	return strings.ToLower(vendor) + "/" + strings.ReplaceAll(id, ".", "-")
}
