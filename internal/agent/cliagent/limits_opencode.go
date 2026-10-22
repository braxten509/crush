package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// openCodeUsageURL is OpenCode Go's usage address (unlisted; it answers
// with the 5-hour, weekly and monthly windows). Tests replace it.
var openCodeUsageURL = "https://opencode.ai/zen/go/v1/usage"

// fetchOpenCodeLimits reads OpenCode Go's usage with the Go key OpenCode
// itself saved (owner, 2026-10-22), so OpenCode's chats and the meter use
// one key. The key is never logged or shown.
func fetchOpenCodeLimits(ctx context.Context) ([]Limit, error) {
	key, err := openCodeGoKey()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode go usage: %s", resp.Status)
	}
	type window struct {
		Percent  float64   `json:"percent"`
		ResetsAt time.Time `json:"resetsAt"`
	}
	var res struct {
		Usage struct {
			Rolling *window `json:"rolling"`
			Weekly  *window `json:"weekly"`
			Monthly *window `json:"monthly"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var limits []Limit
	for _, w := range []struct {
		name string
		at   *window
	}{{"5h", res.Usage.Rolling}, {"Weekly", res.Usage.Weekly}, {"Monthly", res.Usage.Monthly}} {
		if w.at != nil {
			limits = append(limits, Limit{Name: w.name, Used: w.at.Percent, ResetsAt: w.at.ResetsAt.UTC()})
		}
	}
	return limits, nil
}

// openCodeGoKey is the opencode-go key in OpenCode's auth.json, in its
// data folder ($XDG_DATA_HOME/opencode, else ~/.local/share/opencode).
func openCodeGoKey() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "opencode", "auth.json"))
	if err != nil {
		return "", errors.New("OpenCode Go is not signed in")
	}
	var auth map[string]struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(raw, &auth) != nil || auth["opencode-go"].Key == "" {
		return "", errors.New("OpenCode Go is not signed in")
	}
	return auth["opencode-go"].Key, nil
}
