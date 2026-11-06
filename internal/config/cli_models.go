package config

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Agent CLIs change their model lists often, so Crush asks each one what it
// offers and caches the answer. A load reads the cache, refreshing it first
// when it is stale (owner, 2026-10-22: a newly reported effort level used to
// appear one launch late); the cache file is one of the store's tracked
// files, so a background refresh reaches a running app as a config change.

var refreshOnce sync.Once

const modelCacheTTL = time.Hour

// refreshCLIModelList reloads every installed CLI's model list; tests
// replace it.
var refreshCLIModelList = refreshCLIModels

// modelRefreshWait caps how long a load waits for a stale list, so a CLI
// that hangs cannot hold the app's start.
var modelRefreshWait = 10 * time.Second

// maybeRefreshCLIModels refreshes the cache when it is missing or older
// than modelCacheTTL, at most once per process. A stale list is read before
// the load goes on, so the models and effort levels a CLI reports appear on
// the launch that first sees them; the wait is capped so a hanging CLI
// cannot hold the app's start.
func maybeRefreshCLIModels(path string) {
	refreshOnce.Do(func() {
		if fi, err := os.Stat(cliModelCachePath()); err == nil && time.Since(fi.ModTime()) < modelCacheTTL {
			return
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			refreshCLIModelList(path)
		}()
		select {
		case <-done:
		case <-time.After(modelRefreshWait):
			slog.Debug("Agent CLI model list is still refreshing; loading the list already cached")
		}
	})
}

func cliModelCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	// Earlier caches did not record Codex image capabilities.
	return filepath.Join(dir, "crush", "cli-models-v2.json")
}

func readModelCache() map[string][]catwalk.Model {
	cache := map[string][]catwalk.Model{}
	if path := cliModelCachePath(); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(migrateCLIModelCache(data), &cache)
		}
	}
	return cache
}

// migrateCLIModelCache rewrites a cache written before catwalk v3 (flat
// cost, supports_attachments and can_reason/reasoning_levels fields) onto
// the v3 model schema, so a cache that outlived the upgrade keeps its
// effort levels, image support and prices until the next refresh rewrites
// it. A cache already in the v3 shape comes back unchanged.
func migrateCLIModelCache(data []byte) []byte {
	var raw map[string][]map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return data
	}
	changed := false
	for _, models := range raw {
		for _, model := range models {
			if model != nil && migrateCatwalkV2Model(model) {
				changed = true
			}
		}
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return data
	}
	return out
}

// cliReasoning is the reasoning a CLI's structured catalogue declares for a
// model. The effort list is never nil, even when empty: refreshCLIModels
// takes a non-nil list as the CLI's actual controls, an empty one included.
func cliReasoning(levels []string, defaultEffort string) catwalk.Reasoning {
	r := catwalk.Reasoning{
		Thinking:           catwalk.ThinkingNever,
		EffortLevels:       catwalk.NewEffortLevels(levels...),
		DefaultEffortLevel: defaultEffort,
	}
	if len(levels) > 0 {
		r.Thinking = catwalk.ThinkingToggleable
	}
	return r
}

func cachedModels(providerID string) []catwalk.Model {
	return readModelCache()[providerID]
}

// RefreshCLIModels synchronously refreshes the model metadata before a caller
// loads configuration, so an explicit model-list refresh uses the new cache.
func RefreshCLIModels() {
	refreshCLIModels(os.Getenv("PATH"))
}

// refreshCLIModels asks every installed CLI for its models and rewrites
// the cache. Failures keep the previous entry.
func refreshCLIModels(path string) {
	cache := readModelCache()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range cliProviders {
		discover := cliDiscovery[p.cfg.Type]
		if discover == nil || !onPath(path, p.bin) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			models, err := discover(ctx, resolveBin(path, p.bin))
			if err != nil || len(models) == 0 {
				slog.Debug("Agent CLI model discovery failed", "cli", p.bin, "error", err)
				return
			}
			// Keep the details the built-in list knows (context size,
			// reasoning levels) for models it shares.
			for i, m := range models {
				if j := slices.IndexFunc(p.cfg.Models, func(k catwalk.Model) bool { return k.ID == m.ID }); j >= 0 {
					known := p.cfg.Models[j]
					known.Name = cmp.Or(m.Name, known.Name)
					// Structured CLI catalogues declare the actual supported
					// controls, including an explicit empty list. Static fallback
					// entries must not erase or replace that information.
					if m.Reasoning.EffortLevels != nil {
						known.Reasoning = m.Reasoning
						known.Reasoning.EffortLevels = slices.Clone(m.Reasoning.EffortLevels)
					}
					if cliReportsContext(p.cfg.Type) && m.ContextWindow > 0 {
						known.ContextWindow = m.ContextWindow
					}
					if p.cfg.Type == TypeCodexCLI {
						known.Capabilities.Vision = m.Capabilities.Vision
					}
					models[i] = known
				}
			}
			mu.Lock()
			cache[p.cfg.ID] = models
			mu.Unlock()
		}()
	}
	wg.Wait()

	data, err := json.Marshal(cache)
	file := cliModelCachePath()
	if err != nil || file == "" || os.MkdirAll(filepath.Dir(file), 0o700) != nil {
		return
	}
	if err := atomicWriteFile(file, data, 0o600); err != nil {
		slog.Debug("Write CLI model cache failed", "error", err)
	}
}

var cliDiscovery = map[catwalk.Type]func(context.Context, string) ([]catwalk.Model, error){
	TypeCodexCLI:       discoverCodex,
	TypeGrokCLI:        discoverGrok,
	TypeAGYCLI:         listModels(regexp.MustCompile(`^(\S+)\t(.+)$`), "", "models"),
	TypeOpenCodeCLI:    openCodeModels("opencode-go", false),
	TypeOpenCodeZenCLI: openCodeModels("opencode", true),
}

// cliReportsContext lists the agent CLIs whose model catalogue carries real
// context sizes, so discovery replaces Crush's hand-written defaults.
func cliReportsContext(t catwalk.Type) bool {
	return t == TypeGrokCLI || t == TypeOpenCodeCLI || t == TypeOpenCodeZenCLI
}

// openCodeModels asks the OpenCode CLI for one provider's catalogue, with
// the metadata (name, context size, price) only --verbose prints. Zen's
// free tier keeps the models that cost nothing; Go bills by plan and keeps
// all of them.
func openCodeModels(provider string, freeOnly bool) func(context.Context, string) ([]catwalk.Model, error) {
	return func(ctx context.Context, bin string) ([]catwalk.Model, error) {
		out, err := exec.CommandContext(ctx, bin, "models", provider, "--verbose").Output()
		if err != nil {
			return nil, err
		}
		var models []catwalk.Model
		for _, entry := range openCodeCatalogue(string(out)) {
			if freeOnly && (entry.Cost == nil || entry.Cost.Input != 0 || entry.Cost.Output != 0) {
				continue
			}
			if entry.ID == "" || entry.Status == "deprecated" {
				continue
			}
			model := catwalk.Model{
				ID:               provider + "/" + entry.ID,
				Name:             cmp.Or(entry.Name, entry.ID),
				ContextWindow:    entry.Limit.Context,
				DefaultMaxTokens: entry.Limit.Output,
			}
			if model.ContextWindow <= 0 {
				model.ContextWindow = 400_000
			}
			if model.DefaultMaxTokens <= 0 {
				model.DefaultMaxTokens = 32_000
			}
			if len(entry.Variants) > 0 {
				levels := make([]string, 0, len(entry.Variants))
				for level := range entry.Variants {
					levels = append(levels, level)
				}
				slices.SortFunc(levels, func(a, b string) int {
					return cmp.Compare(reasoningRank(a), reasoningRank(b))
				})
				defaultEffort := levels[0]
				if slices.Contains(levels, "low") {
					defaultEffort = "low"
				}
				model.Reasoning = cliReasoning(levels, defaultEffort)
			}
			models = append(models, model)
		}
		return models, nil
	}
}

type openCodeEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Variants map[string]struct {
		ReasoningEffort string `json:"reasoningEffort"`
	} `json:"variants"`
	Cost *struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
	Limit struct {
		Context int64 `json:"context"`
		Output  int64 `json:"output"`
	} `json:"limit"`
}

// openCodeCatalogue reads the model ids and the JSON objects --verbose
// prints after each one.
func openCodeCatalogue(out string) []openCodeEntry {
	var entries []openCodeEntry
	var block []string
	depth := 0
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case depth == 0 && strings.HasPrefix(line, "{"):
			block, depth = []string{line}, strings.Count(line, "{")-strings.Count(line, "}")
		case depth > 0:
			block = append(block, line)
			depth += strings.Count(line, "{") - strings.Count(line, "}")
		}
		if depth > 0 || len(block) == 0 {
			continue
		}
		var entry openCodeEntry
		if json.Unmarshal([]byte(strings.Join(block, "\n")), &entry) == nil {
			entries = append(entries, entry)
		}
		block = nil
	}
	return entries
}

// Grok's plain `models` output only has names. Its ACP initialization response
// reports each model's supported efforts and default without starting a session
// or sending a prompt.
func discoverGrok(ctx context.Context, bin string) ([]catwalk.Model, error) {
	line, err := rpcExchange(ctx, bin, []string{"agent", "--no-leader", "stdio"}, []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
			"protocolVersion": 1, "clientCapabilities": map[string]any{},
		}},
	}, func(data []byte) bool {
		var response struct {
			ID json.RawMessage `json:"id"`
		}
		return json.Unmarshal(data, &response) == nil && strings.Trim(string(response.ID), `"`) == "1"
	})
	if err != nil {
		return nil, err
	}
	return grokModelMetadata(line)
}

func grokModelMetadata(data []byte) ([]catwalk.Model, error) {
	var response struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Meta struct {
				ModelState struct {
					Models []struct {
						ID   string `json:"modelId"`
						Name string `json:"name"`
						Meta struct {
							ContextWindow  int64  `json:"totalContextTokens"`
							SupportsEffort bool   `json:"supportsReasoningEffort"`
							CurrentEffort  string `json:"reasoningEffort"`
							Efforts        []struct {
								ID      string `json:"id"`
								Value   string `json:"value"`
								Default bool   `json:"default"`
							} `json:"reasoningEfforts"`
						} `json:"_meta"`
					} `json:"availableModels"`
				} `json:"modelState"`
			} `json:"_meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("read Grok model metadata: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("read Grok model metadata: %s", response.Error.Message)
	}
	var models []catwalk.Model
	for _, entry := range response.Result.Meta.ModelState.Models {
		if entry.ID == "" {
			continue
		}
		model := cliModels(entry.ID, cmp.Or(entry.Name, entry.ID))[0]
		if entry.Meta.ContextWindow > 0 {
			model.ContextWindow = entry.Meta.ContextWindow
		}
		levels := []string{}
		defaultEffort := ""
		if entry.Meta.SupportsEffort {
			for _, effort := range entry.Meta.Efforts {
				value := cmp.Or(effort.Value, effort.ID)
				if value == "" {
					continue
				}
				if !slices.Contains(levels, value) {
					levels = append(levels, value)
				}
				if effort.Default {
					defaultEffort = value
				}
			}
			if defaultEffort == "" && slices.Contains(levels, entry.Meta.CurrentEffort) {
				defaultEffort = entry.Meta.CurrentEffort
			}
		}
		model.Reasoning = cliReasoning(levels, defaultEffort)
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("Grok returned no model capability metadata")
	}
	return models, nil
}

// listModels runs a CLI's model listing command and takes a model from each
// line re matches: group 1 is the ID, group 2 (if any) the display name.
// The ID with trimPrefix removed stands in for a missing name.
func listModels(re *regexp.Regexp, trimPrefix string, args ...string) func(context.Context, string) ([]catwalk.Model, error) {
	return func(ctx context.Context, bin string) ([]catwalk.Model, error) {
		out, err := exec.CommandContext(ctx, bin, args...).Output()
		if err != nil {
			return nil, err
		}
		var models []catwalk.Model
		for line := range strings.SplitSeq(string(out), "\n") {
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := strings.TrimPrefix(m[1], trimPrefix)
			if len(m) > 2 && m[2] != "" && trimPrefix == "" {
				name = m[2]
			}
			models = append(models, cliModels(m[1], name)...)
		}
		return models, nil
	}
}

// resolveBin skips shell wrappers by preferring ~/.local/bin, where these
// CLIs install themselves.
func resolveBin(path, bin string) string {
	if !testing.Testing() {
		if home, err := os.UserHomeDir(); err == nil {
			if executable := findCLI(filepath.Join(home, ".local", "bin"), bin); executable != "" {
				return executable
			}
		}
	}
	if executable := findCLI(path, bin); executable != "" {
		return executable
	}
	return bin
}

// rpcExchange starts a CLI, writes requests as JSON lines, and returns the
// first output line for which done reports true.
func rpcExchange(ctx context.Context, bin string, args []string, requests []any, done func([]byte) bool) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	for _, r := range requests {
		data, _ := json.Marshal(r)
		if _, err := stdin.Write(append(data, '\n')); err != nil {
			return nil, err
		}
	}
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for lines.Scan() {
		if done(lines.Bytes()) {
			return lines.Bytes(), nil
		}
	}
	return nil, ctx.Err()
}

func discoverCodex(ctx context.Context, bin string) ([]catwalk.Model, error) {
	line, err := rpcExchange(ctx, bin, []string{"app-server"}, []any{
		map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "crush", "version": "0"}}},
		map[string]any{"method": "initialized"},
		map[string]any{"id": 2, "method": "model/list", "params": map[string]any{}},
	}, func(b []byte) bool {
		var msg struct {
			ID json.RawMessage `json:"id"`
		}
		return json.Unmarshal(b, &msg) == nil && string(msg.ID) == "2"
	})
	if err != nil || line == nil {
		return nil, err
	}
	var res struct {
		Result struct {
			Data []struct {
				ID                        string   `json:"id"`
				DisplayName               string   `json:"displayName"`
				Hidden                    bool     `json:"hidden"`
				InputModalities           []string `json:"inputModalities"`
				DefaultReasoningEffort    string   `json:"defaultReasoningEffort"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(line, &res); err != nil {
		return nil, err
	}
	var models []catwalk.Model
	for _, d := range res.Result.Data {
		if d.Hidden {
			continue
		}
		m := catwalk.Model{ID: d.ID, Name: d.DisplayName, ContextWindow: 400_000, DefaultMaxTokens: 64_000}
		// A catalog that omits the efforts says nothing about them: the
		// built-in entry's levels stay, only the default is recorded.
		m.Reasoning.DefaultEffortLevel = d.DefaultReasoningEffort
		if d.SupportedReasoningEfforts != nil {
			levels := make([]string, 0, len(d.SupportedReasoningEfforts))
			for _, e := range d.SupportedReasoningEfforts {
				levels = append(levels, e.ReasoningEffort)
			}
			m.Reasoning = cliReasoning(levels, d.DefaultReasoningEffort)
		}
		// Older Codex catalogs omit modalities and support text and images.
		m.Capabilities.Vision = d.InputModalities == nil || slices.Contains(d.InputModalities, "image")
		models = append(models, m)
	}
	return models, nil
}

// reasoningRank orders the effort levels an agent CLI reports in the
// conventional progression rather than alphabetically. Unknown levels sort
// last so a new one still appears without displacing the known ones.
func reasoningRank(level string) int {
	switch level {
	case "minimal":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "xhigh":
		return 4
	case "max":
		return 5
	default:
		return 6
	}
}
