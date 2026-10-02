package config

import (
	"bufio"
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

	"charm.land/catwalk/pkg/catwalk"
)

// Agent CLIs change their model lists often, so Crush asks each one what it
// offers and caches the answer. Startup reads the cache; a background
// refresh updates it for the next launch.
// ponytail: new models appear one launch late; refresh synchronously if
// that ever matters.

var refreshOnce sync.Once

const modelCacheTTL = time.Hour

// maybeRefreshCLIModels starts a background refresh when the cache is
// missing or older than modelCacheTTL, at most once per process.
func maybeRefreshCLIModels(path string) {
	refreshOnce.Do(func() {
		if fi, err := os.Stat(cliModelCachePath()); err == nil && time.Since(fi.ModTime()) < modelCacheTTL {
			return
		}
		go refreshCLIModels(path)
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
			_ = json.Unmarshal(data, &cache)
		}
	}
	return cache
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
			models, err := discover(ctx)
			if err != nil || len(models) == 0 {
				slog.Debug("Agent CLI model discovery failed", "cli", p.bin, "error", err)
				return
			}
			// Keep the details the built-in list knows (context size,
			// reasoning levels) for models it shares.
			for i, m := range models {
				if j := slices.IndexFunc(p.cfg.Models, func(k catwalk.Model) bool { return k.ID == m.ID }); j >= 0 {
					known := p.cfg.Models[j]
					known.Name = cmpOr(m.Name, known.Name)
					// Structured CLI catalogues declare the actual supported
					// controls, including an explicit empty list. Static fallback
					// entries must not erase or replace that information.
					if m.ReasoningLevels != nil {
						known.CanReason = m.CanReason
						known.ReasoningLevels = slices.Clone(m.ReasoningLevels)
						known.DefaultReasoningEffort = m.DefaultReasoningEffort
					}
					if p.cfg.Type == TypeGrokCLI && m.ContextWindow > 0 {
						known.ContextWindow = m.ContextWindow
					}
					if p.cfg.Type == TypeCodexCLI {
						known.SupportsImages = m.SupportsImages
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
	if os.WriteFile(file+".tmp", data, 0o600) == nil {
		_ = os.Rename(file+".tmp", file)
	}
}

var cliDiscovery = map[catwalk.Type]func(context.Context) ([]catwalk.Model, error){
	TypeCodexCLI:    discoverCodex,
	TypeGrokCLI:     discoverGrok,
	TypeAGYCLI:      listModels(regexp.MustCompile(`^(\S+)\t(.+)$`), "", "agy", "models"),
	TypeOpenCodeCLI: listModels(regexp.MustCompile(`^(opencode-go/(\S+))$`), "opencode-go/", "opencode", "models", "opencode-go"),
}

// Grok's plain `models` output only has names. Its ACP initialization response
// reports each model's supported efforts and default without starting a session
// or sending a prompt.
func discoverGrok(ctx context.Context) ([]catwalk.Model, error) {
	line, err := rpcExchange(ctx, resolveBin("grok"), []string{"agent", "--no-leader", "stdio"}, []any{
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
		model := cliModels(entry.ID, cmpOr(entry.Name, entry.ID))[0]
		if entry.Meta.ContextWindow > 0 {
			model.ContextWindow = entry.Meta.ContextWindow
		}
		model.ReasoningLevels = []string{}
		if entry.Meta.SupportsEffort {
			for _, effort := range entry.Meta.Efforts {
				value := cmpOr(effort.Value, effort.ID)
				if value == "" {
					continue
				}
				if !slices.Contains(model.ReasoningLevels, value) {
					model.ReasoningLevels = append(model.ReasoningLevels, value)
				}
				if effort.Default {
					model.DefaultReasoningEffort = value
				}
			}
			if model.DefaultReasoningEffort == "" && slices.Contains(model.ReasoningLevels, entry.Meta.CurrentEffort) {
				model.DefaultReasoningEffort = entry.Meta.CurrentEffort
			}
		}
		model.CanReason = len(model.ReasoningLevels) > 0
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
func listModels(re *regexp.Regexp, trimPrefix, bin string, args ...string) func(context.Context) ([]catwalk.Model, error) {
	return func(ctx context.Context) ([]catwalk.Model, error) {
		out, err := exec.CommandContext(ctx, resolveBin(bin), args...).Output()
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
func resolveBin(bin string) string {
	if testing.Testing() {
		return bin // Test fakes on PATH must take precedence over installed CLIs.
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".local", "bin", bin)
		if fi, err := os.Stat(path); err == nil && fi.Mode()&0o111 != 0 {
			return path
		}
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

func discoverCodex(ctx context.Context) ([]catwalk.Model, error) {
	line, err := rpcExchange(ctx, "codex", []string{"app-server"}, []any{
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
		m := catwalk.Model{ID: d.ID, Name: d.DisplayName, ContextWindow: 400_000, DefaultMaxTokens: 64_000, DefaultReasoningEffort: d.DefaultReasoningEffort}
		if d.SupportedReasoningEfforts != nil {
			m.ReasoningLevels = []string{}
		}
		for _, e := range d.SupportedReasoningEfforts {
			m.ReasoningLevels = append(m.ReasoningLevels, e.ReasoningEffort)
		}
		m.CanReason = len(m.ReasoningLevels) > 0
		// Older Codex catalogs omit modalities and support text and images.
		m.SupportsImages = d.InputModalities == nil || slices.Contains(d.InputModalities, "image")
		models = append(models, m)
	}
	return models, nil
}

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
