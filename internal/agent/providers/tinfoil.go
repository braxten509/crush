package providers

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"sync"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/tinfoilsh/tinfoil-go"
)

// tinfoilClients are the verified HTTP clients, one per Tinfoil server, so
// every chat on a server shares one check of its proof.
var tinfoilClients sync.Map

// tinfoilHTTPClient is the client for a Tinfoil base URL: before it sends
// anything, it checks the server's proof that it runs Tinfoil's published
// code in a secure enclave, and it encrypts each request body so only that
// enclave can read it (owner, 2026-10-26). It refuses any other server.
// The check waits for the first request, so building a model never waits
// on the network.
func tinfoilHTTPClient(baseURL string) (*http.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("tinfoil: bad base URL %q", baseURL)
	}
	c, _ := tinfoilClients.LoadOrStore(u.Host, &http.Client{Transport: &tinfoilTransport{host: u.Host}})
	return c.(*http.Client), nil
}

// tinfoilTransport makes the verified client on the first request, and
// again on a later one if making it failed.
type tinfoilTransport struct {
	host  string
	mu    sync.Mutex
	inner http.RoundTripper
}

func (t *tinfoilTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	if t.inner == nil {
		client, err := tinfoil.NewClientWithOptions(tinfoil.WithEnclave(t.host))
		if err != nil {
			t.mu.Unlock()
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, fmt.Errorf("tinfoil: the server could not prove it is Tinfoil's secure server: %w", err)
		}
		t.inner = client.HTTPClient().Transport
	}
	inner := t.inner
	t.mu.Unlock()
	return inner.RoundTrip(req)
}

// tinfoilQuirk sends each Tinfoil model's thinking switch the way Tinfoil's
// model list gives it (config.TinfoilThinking), on every Tinfoil account.
var tinfoilQuirk = compatQuirk{
	effort: func(c *optionsCall, extraBody map[string]any) {
		tinfoilThinking(c, extraBody, true)
	},
	thinking: func(c *optionsCall, extraBody map[string]any) {
		if !c.setEffort {
			tinfoilThinking(c, extraBody, c.model.ModelCfg.Think)
		}
	},
}

// tinfoilThinking keeps the user's request body and adds the model's
// fields that turn thinking on, at the call's effort, or off, without
// replacing any the user set.
func tinfoilThinking(c *optionsCall, extraBody map[string]any, on bool) {
	if existing, ok := c.merged["extra_body"].(map[string]any); ok {
		for k, v := range existing {
			extraBody[k] = cloneValue(v)
		}
	}
	r, ok := config.TinfoilThinking(c.model.CatwalkCfg.ID)
	if !ok {
		return
	}
	body := r.Disable
	if on {
		body = r.Enable
	}
	effort := c.effort
	if mapped, ok := r.EffortMap[effort]; ok {
		effort = mapped
	}
	addMissing(extraBody, body, effort)
}

// addMissing copies src's fields into dst where dst has none, going into
// nested objects, with "$EFFORT" read as effort (left out when empty).
func addMissing(dst, src map[string]any, effort string) {
	for k, v := range src {
		switch v := v.(type) {
		case map[string]any:
			inner, ok := dst[k].(map[string]any)
			if _, taken := dst[k]; taken && !ok {
				continue
			}
			if inner == nil {
				inner = map[string]any{}
			}
			addMissing(inner, v, effort)
			dst[k] = inner
		case string:
			if _, taken := dst[k]; taken {
				continue
			}
			if v == "$EFFORT" {
				if effort == "" {
					continue
				}
				v = effort
			}
			dst[k] = v
		default:
			if _, taken := dst[k]; !taken {
				dst[k] = v
			}
		}
	}
}

func cloneValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		out := maps.Clone(m)
		for k, inner := range out {
			out[k] = cloneValue(inner)
		}
		return out
	}
	return v
}
