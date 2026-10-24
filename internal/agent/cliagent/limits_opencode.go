package cliagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// openCodeUsageURL is OpenCode Go's usage address (unlisted; it answers
// with the 5-hour, weekly and monthly windows). Tests replace it.
var openCodeUsageURL = "https://opencode.ai/zen/go/v1/usage"

// fetchOpenCodeLimits reads OpenCode Go's usage with the active Console
// sign-in OpenCode itself uses (owner, 2026-10-23), so OpenCode's page and
// the meter show the same account. The token is never logged or shown.
func fetchOpenCodeLimits(ctx context.Context) ([]Limit, error) {
	key, err := openCodeConsoleToken()
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

// openCodeConsoleToken is the access token of the active Console sign-in in
// OpenCode's database, in its data folder ($XDG_DATA_HOME/opencode, else
// ~/.local/share/opencode). The legacy opencode-go key in auth.json is not
// read: it can belong to another account, whose windows are not the ones
// the page shows (owner, 2026-10-23).
func openCodeConsoleToken() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	dbPath := filepath.Join(dir, "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", errors.New("OpenCode Console is not signed in")
	}
	params := url.Values{}
	params.Set("mode", "ro")
	params.Set("_pragma", "busy_timeout(5000)")
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: params.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", fmt.Errorf("open OpenCode database: %w", err)
	}
	defer db.Close()
	var raw string
	err = db.QueryRow(`SELECT value FROM credential WHERE integration_id = 'opencode' AND active = 1 LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("OpenCode Console is not signed in")
	}
	if err != nil {
		return "", fmt.Errorf("read OpenCode Console sign-in: %w", err)
	}
	var credential struct {
		Access string `json:"access"`
	}
	if json.Unmarshal([]byte(raw), &credential) != nil || credential.Access == "" {
		return "", errors.New("OpenCode Console is not signed in")
	}
	return credential.Access, nil
}
