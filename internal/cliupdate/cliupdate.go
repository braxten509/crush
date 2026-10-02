// Package cliupdate checks the agent CLIs Crush drives (Claude Code, Codex,
// Grok, OpenCode, AGY) for new releases and runs each CLI's own updater.
package cliupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CLI describes how to version-check and update one agent CLI.
type CLI struct {
	Name string
	Bin  string
	// Latest returns the newest released version; bin is the resolved path.
	Latest func(ctx context.Context, bin string) (string, error)
	// UpdateArgs runs the CLI's own updater.
	UpdateArgs []string
}

// Update is a CLI with a newer release than the installed one.
type Update struct {
	Name    string
	Bin     string
	Path    string
	Current string
	Latest  string
	args    []string
}

// CLIs lists every CLI Crush can update, in display order.
var CLIs = []CLI{
	{Name: "Claude Code", Bin: "claude", Latest: claudeLatest, UpdateArgs: []string{"update"}},
	{Name: "Codex", Bin: "codex", Latest: githubLatest("openai/codex", "rust-v"), UpdateArgs: []string{"update"}},
	{Name: "Grok", Bin: "grok", Latest: grokLatest, UpdateArgs: []string{"update"}},
	{Name: "OpenCode", Bin: "opencode", Latest: githubLatest("anomalyco/opencode", "v"), UpdateArgs: []string{"upgrade"}},
	{Name: "AGY", Bin: "agy", Latest: agyLatest, UpdateArgs: []string{"update"}},
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?`)

// Check looks up every installed CLI in parallel and returns those with a
// newer release, in [CLIs] order. Errors are per CLI and don't stop the rest.
func Check(ctx context.Context) ([]Update, []error) {
	type result struct {
		update *Update
		err    error
	}
	results := make([]result, len(CLIs))
	var wg sync.WaitGroup
	for i, c := range CLIs {
		path := resolve(c.Bin)
		if path == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := check(ctx, c, path)
			results[i] = result{u, err}
		}()
	}
	wg.Wait()
	var updates []Update
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
		} else if r.update != nil {
			updates = append(updates, *r.update)
		}
	}
	return updates, errs
}

func check(ctx context.Context, c CLI, path string) (*Update, error) {
	out, err := run(ctx, 15*time.Second, path, "--version")
	current := versionRe.FindString(out)
	if current == "" {
		return nil, fmt.Errorf("%s: can't read version: %v %s", c.Name, err, strings.TrimSpace(out))
	}
	latest, err := c.Latest(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Name, err)
	}
	if !Newer(latest, current) {
		return nil, nil
	}
	return &Update{Name: c.Name, Bin: c.Bin, Path: path, Current: current, Latest: latest, args: c.UpdateArgs}, nil
}

// Install runs the CLI's own updater and returns its output on failure.
func Install(ctx context.Context, u Update) error {
	out, err := run(ctx, 10*time.Minute, u.Path, u.args...)
	if err != nil {
		return fmt.Errorf("%s: %w: %s", u.Name, err, lastLine(out))
	}
	return nil
}

// Newer reports whether version a is newer than b. A release beats a
// pre-release of the same version.
func Newer(a, b string) bool {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := range max(len(an), len(bn)) {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			return x > y
		}
	}
	return apre == "" && bpre != ""
}

func splitVersion(v string) ([]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, pre, _ := strings.Cut(v, "-")
	var nums []int
	for p := range strings.SplitSeq(v, ".") {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	return nums, pre
}

// resolve finds a CLI binary. AGY is looked up in ~/.local/bin first, like
// the AGY driver, since shells often wrap it in a function.
func resolve(bin string) string {
	if bin == "agy" {
		if home, err := os.UserHomeDir(); err == nil {
			p := filepath.Join(home, ".local", "bin", "agy")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p
			}
		}
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	return p
}

func run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "crush/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	return body, nil
}

// claudeLatest reads the release channel Claude's own updater follows
// (autoUpdatesChannel in ~/.claude/settings.json, "latest" by default).
func claudeLatest(ctx context.Context, _ string) (string, error) {
	channel := "latest"
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err == nil {
			var s struct {
				Channel string `json:"autoUpdatesChannel"`
			}
			if json.Unmarshal(data, &s) == nil && s.Channel != "" {
				channel = s.Channel
			}
		}
	}
	body, err := get(ctx, "https://downloads.claude.ai/claude-code-releases/"+channel)
	if err != nil {
		return "", err
	}
	return parseVersion(string(body))
}

func githubLatest(repo, tagPrefix string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, _ string) (string, error) {
		body, err := get(ctx, "https://api.github.com/repos/"+repo+"/releases/latest")
		if err != nil {
			return "", err
		}
		var r struct {
			Tag string `json:"tag_name"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return "", err
		}
		return parseVersion(strings.TrimPrefix(r.Tag, tagPrefix))
	}
}

func grokLatest(ctx context.Context, bin string) (string, error) {
	out, err := run(ctx, 30*time.Second, bin, "update", "--check", "--json")
	var r struct {
		Latest string  `json:"latestVersion"`
		Error  *string `json:"error"`
	}
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); jerr != nil {
		return "", errors.Join(err, jerr)
	}
	if r.Error != nil && *r.Error != "" {
		return "", errors.New(*r.Error)
	}
	return parseVersion(r.Latest)
}

var agyStableRe = regexp.MustCompile(`Stable Version:\s*(\d+\.\d+\.\d+)`)

// agyLatest reads the version AGY's own auto-updater announces on its
// status page.
func agyLatest(ctx context.Context, _ string) (string, error) {
	body, err := get(ctx, "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app")
	if err != nil {
		return "", err
	}
	m := agyStableRe.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("no version on AGY's update page")
	}
	return string(m[1]), nil
}

func parseVersion(s string) (string, error) {
	v := versionRe.FindString(s)
	if v == "" {
		return "", fmt.Errorf("no version in %q", strings.TrimSpace(s))
	}
	return v, nil
}
