package config

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	TypeGrokCLI:     listModels(regexp.MustCompile(`^\s*[*-]\s+(\S+)`), "", "grok", "models"),
	TypeAGYCLI:      listModels(regexp.MustCompile(`^(\S+)\t(.+)$`), "", "agy", "models"),
	TypeOpenCodeCLI: listModels(regexp.MustCompile(`^(opencode-go/(\S+))$`), "opencode-go/", "opencode", "models", "opencode-go"),
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
