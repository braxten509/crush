package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/charmbracelet/crush/internal/log"
)

type abacusResolver func(string) (string, error)

func (resolve abacusResolver) ResolveValue(value string) (string, error) { return resolve(value) }

func (c *coordinator) abacusHTTPClient(provider config.ProviderConfig, model catwalk.Model, selected config.SelectedModel) *http.Client {
	var transport http.RoundTripper = http.DefaultTransport
	if c.cfg.Config().Options.Debug {
		transport = log.NewHTTPClient().Transport
	}
	return &http.Client{Transport: &abacusTransport{
		transport: transport, model: model, selected: selected, currentID: model.ID,
		refresh: func(ctx context.Context) ([]catwalk.Model, error) {
			// Do not keep configured/discovered old IDs in this authoritative
			// refresh. The replacement must actually be listed by Abacus now.
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			return discover.DiscoverModels(ctx, discover.Config{
				ID: provider.ID, BaseURL: provider.BaseURL, APIKey: provider.APIKey,
				ExtraHeaders: provider.ExtraHeaders, KnownProviders: config.ModelCatalog(),
			}, abacusResolver(c.cfg.Resolve))
		},
	}}
}

// Recover a retired API identifier only when the live catalogue unambiguously
// names the same model with compatible controls. Never fall back to a different
// model, retry an accepted stream, or replay authentication/parameter failures.
type abacusTransport struct {
	transport http.RoundTripper
	model     catwalk.Model
	selected  config.SelectedModel
	refresh   func(context.Context) ([]catwalk.Model, error)
	mu        sync.Mutex
	currentID string
}

func (t *abacusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.GetBody == nil {
		return t.transport.RoundTrip(request)
	}
	input, err := request.GetBody()
	if err != nil {
		request.Body.Close()
		return nil, err
	}
	body, err := io.ReadAll(input)
	input.Close()
	if err != nil {
		request.Body.Close()
		return nil, err
	}
	var fields map[string]json.RawMessage
	var modelID string
	if json.Unmarshal(body, &fields) != nil || json.Unmarshal(fields["model"], &modelID) != nil || modelID != t.model.ID {
		return t.transport.RoundTrip(request)
	}
	request.Body.Close()
	send := func(id string) (*http.Response, error) {
		updated := body
		if id != modelID {
			fields["model"], _ = json.Marshal(id)
			updated, err = json.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
		clone := request.Clone(request.Context())
		clone.Body = io.NopCloser(bytes.NewReader(updated))
		clone.ContentLength = int64(len(updated))
		clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(updated)), nil }
		return t.transport.RoundTrip(clone)
	}
	t.mu.Lock()
	current := t.currentID
	t.mu.Unlock()
	response, err := send(current)
	if err != nil || (response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusBadRequest) {
		return response, err
	}
	// Peek at small API error bodies without losing the original response if
	// refresh fails or there is no safe replacement.
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	response.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(data), response.Body), response.Body}
	if readErr != nil || !unknownAbacusModel(data) {
		return response, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.currentID == current {
		models, refreshErr := t.refresh(request.Context())
		if refreshErr != nil {
			return response, nil
		}
		replacement := t.replacement(models, current)
		if replacement == "" {
			return response, nil
		}
		t.currentID = replacement
		slog.Info("Abacus model identifier refreshed", "previous", current, "model", replacement)
	}
	response.Body.Close()
	return send(t.currentID)
}

func unknownAbacusModel(body []byte) bool {
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &failure) == nil &&
		(failure.Error.Code == "model_not_found" || strings.Contains(strings.ToLower(failure.Error.Message), "unknown model"))
}

func (t *abacusTransport) replacement(models []catwalk.Model, current string) string {
	if t.model.Name == "" || t.model.Name == t.model.ID {
		return ""
	}
	var matches []catwalk.Model
	for _, model := range models {
		if model.ID == current {
			return "" // Still listed: this is not an identifier change.
		}
		if strings.EqualFold(strings.TrimSpace(model.Name), strings.TrimSpace(t.model.Name)) {
			matches = append(matches, model)
		}
	}
	if len(matches) != 1 {
		return ""
	}
	model := matches[0]
	if config.AbacusAPIFormat(model) != config.AbacusAPIFormat(t.model) ||
		(t.model.ContextWindow > 0 && model.ContextWindow < t.model.ContextWindow) ||
		(t.model.SupportsImages && !model.SupportsImages) ||
		(t.selected.ReasoningEffort != "" && !slices.Contains(model.ReasoningLevels, t.selected.ReasoningEffort)) ||
		(t.selected.Think && !config.SupportsThinkingToggle(config.ProviderConfig{ID: config.AbacusProviderID}, model)) ||
		(t.selected.ServiceTier == "fast" && !config.SupportsFastMode(config.ProviderConfig{ID: config.AbacusProviderID}, model)) ||
		(t.selected.MaxTokens > 0 && model.DefaultMaxTokens < t.selected.MaxTokens) {
		return ""
	}
	return model.ID
}
