//go:build crushtest

package cmd

// The test seams (desktop design 5-6, 9.1, G-21 to G-24). Only builds with
// the crushtest tag carry this file; release builds never do. When
// CRUSH_TEST_SEAMS names a JSON file, crush reads it once at start (the
// engine and the terminal app alike) and installs each seam it names, so
// the desktop app's checks reach no network, account or real Tailscale.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/login"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/charmbracelet/crush/internal/skills"
)

// testSeamsEnv names the seams file.
const testSeamsEnv = "CRUSH_TEST_SEAMS"

// testSeams is the seams file. A seam left out keeps the real behavior.
type testSeams struct {
	// Memory replaces the readers of memwatch.Default (G-21).
	Memory *struct {
		Percent int `json:"percent"`
		Top     []struct {
			Name  string `json:"name"`
			Bytes uint64 `json:"bytes"`
		} `json:"top"`
		TotalRAMBytes uint64 `json:"total_ram_bytes"`
	} `json:"memory"`
	// CLIUpdates are the agent CLI updates the check finds; install is
	// "ok" or the error the CLI's updater fails with (G-22).
	CLIUpdates *[]struct {
		Name    string `json:"name"`
		Bin     string `json:"bin"`
		Path    string `json:"path"`
		Current string `json:"current"`
		Latest  string `json:"latest"`
		Install string `json:"install"`
	} `json:"cli_updates"`
	// CrushRelease is the fork checkout's newest release; an empty tag
	// means the build is current, an empty dir that there is no checkout
	// (G-22).
	CrushRelease *struct {
		Tag     string `json:"tag"`
		Current string `json:"current"`
		Dir     string `json:"dir"`
		Staged  string `json:"staged"`
	} `json:"crush_release"`
	// Remote stands in for Tailscale (G-23).
	Remote *struct {
		Listen string   `json:"listen"`
		Host   string   `json:"host"`
		User   string   `json:"user"`
		Phones []string `json:"phones"`
	} `json:"remote"`
	// Limits are each AI tool's usage windows by kind: a list of
	// {name, used, resets_at, model}, or the refresh's error text (G-24).
	Limits map[string]json.RawMessage `json:"limits"`
	// Auth answers the key checks ("ok" or the error) by provider and runs
	// each platform's sign-in: its URL and code, then after after_ms the
	// result, "ok" or the error. With code set, the sign-in takes a pasted
	// code (code_entry) and finishes when that code is sent; after_ms 0
	// then waits for it. Saving a sign-in fetches no model catalog (G-24).
	Auth *struct {
		VerifyKey map[string]string `json:"verify_key"`
		Flows     map[string]struct {
			URL      string `json:"url"`
			UserCode string `json:"user_code"`
			Result   string `json:"result"`
			AfterMS  int    `json:"after_ms"`
			Code     string `json:"code"`
		} `json:"flows"`
	} `json:"auth"`
	// SkillsSearch are the skills.sh results by query; other queries find
	// nothing (G-24).
	SkillsSearch map[string][]skills.DirectorySkill `json:"skills_search"`
}

// installTestSeams installs the seams CRUSH_TEST_SEAMS names, once, at
// start. A seams file that can't be read stops crush: a check must not
// silently run against the real world.
func installTestSeams() {
	path := os.Getenv(testSeamsEnv)
	if path == "" {
		return
	}
	if _, err := loadTestSeams(path); err != nil {
		fmt.Fprintf(os.Stderr, "crush: test seams %s: %v\n", path, err)
		os.Exit(1)
	}
}

// loadTestSeams installs the seams in the file at path and returns a
// function that removes them again.
func loadTestSeams(path string) (restore func(), err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seams testSeams
	if err := json.Unmarshal(data, &seams); err != nil {
		return nil, err
	}
	var restores []func()
	restore = func() {
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
	}

	if m := seams.Memory; m != nil {
		top := make([]memwatch.Program, len(m.Top))
		for i, p := range m.Top {
			top[i] = memwatch.Program{Name: p.Name, Bytes: p.Bytes}
		}
		restores = append(restores, memwatch.SetReadingsForTest(m.Percent, top, m.TotalRAMBytes))
	}

	if list := seams.CLIUpdates; list != nil {
		updates := make([]cliupdate.Update, len(*list))
		installs := map[string]string{}
		for i, u := range *list {
			updates[i] = cliupdate.Update{Name: u.Name, Bin: u.Bin, Path: u.Path, Current: u.Current, Latest: u.Latest}
			installs[u.Bin] = u.Install
		}
		restores = append(restores, cliupdate.SetUpdatesForTest(updates, func(u cliupdate.Update) error {
			if result := installs[u.Bin]; result != "ok" && result != "" {
				return fmt.Errorf("%s: %s", u.Name, result)
			}
			return nil
		}))
	}

	if r := seams.CrushRelease; r != nil {
		release := *r
		restores = append(restores, backend.SetCrushUpdateSourceForTest(
			func() string { return release.Dir },
			func(context.Context, string) (*selfupdate.Release, error) {
				if release.Tag == "" {
					return nil, nil
				}
				return &selfupdate.Release{Tag: release.Tag, Current: release.Current, Dir: release.Dir}, nil
			},
			func(_ context.Context, _, tag string) string {
				if tag == release.Tag {
					return release.Staged
				}
				return ""
			},
		))
	}

	if r := seams.Remote; r != nil {
		listen, err := netip.ParseAddr(r.Listen)
		if err != nil {
			restore()
			return nil, fmt.Errorf("remote.listen: %w", err)
		}
		restores = append(restores, remote.SetTailnetForTest(remote.TestTailnet{Listen: listen, Host: r.Host, User: r.User, Phones: r.Phones}))
	}

	if seams.Limits != nil {
		fetchers := map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){}
		for kind, raw := range seams.Limits {
			fetch, err := seamLimits(raw)
			if err != nil {
				restore()
				return nil, fmt.Errorf("limits.%s: %w", kind, err)
			}
			fetchers[catwalk.Type(kind)] = fetch
		}
		restores = append(restores, cliagent.SetLimitFetchersForTest(fetchers))
	}

	if a := seams.Auth; a != nil {
		flows, verify := a.Flows, a.VerifyKey
		restores = append(restores, config.SkipLoginCatalogsForTest())
		restores = append(restores, backend.SetAuthForTest(
			func(platform string) (login.Flow, error) {
				f, ok := flows[platform]
				if !ok {
					return nil, fmt.Errorf("no seam sign-in for %s", platform)
				}
				flow := &seamFlow{url: f.URL, userCode: f.UserCode, result: f.Result, after: time.Duration(f.AfterMS) * time.Millisecond}
				if f.Code != "" {
					return &seamCodeFlow{seamFlow: flow, code: f.Code}, nil
				}
				return flow, nil
			},
			func(providerID, _ string) error {
				result, ok := verify[providerID]
				switch {
				case !ok:
					return fmt.Errorf("no seam key check for %s", providerID)
				case result == "ok":
					return nil
				default:
					return errors.New(result)
				}
			},
		))
	}

	if found := seams.SkillsSearch; found != nil {
		restores = append(restores, backend.SetSkillsSearchForTest(func(_ context.Context, query string) ([]skills.DirectorySkill, error) {
			return found[query], nil
		}))
	}
	return restore, nil
}

// seamLimits reads one kind's limits seam.
func seamLimits(raw json.RawMessage) (func(context.Context) ([]cliagent.Limit, error), error) {
	var failure string
	if json.Unmarshal(raw, &failure) == nil {
		return func(context.Context) ([]cliagent.Limit, error) { return nil, errors.New(failure) }, nil
	}
	var windows []struct {
		Name     string    `json:"name"`
		Used     float64   `json:"used"`
		ResetsAt time.Time `json:"resets_at"`
		Model    string    `json:"model"`
	}
	if err := json.Unmarshal(raw, &windows); err != nil {
		return nil, err
	}
	limits := make([]cliagent.Limit, len(windows))
	for i, w := range windows {
		limits[i] = cliagent.Limit{Name: w.Name, Used: w.Used, ResetsAt: w.ResetsAt, Model: w.Model}
	}
	return func(context.Context) ([]cliagent.Limit, error) { return limits, nil }, nil
}

// seamFlow is a sign-in that shows its URL and code, then after a while
// succeeds or fails as the seam says.
type seamFlow struct {
	url, userCode, result string
	after                 time.Duration
}

func (f *seamFlow) Start(context.Context) (string, string, error) { return f.url, f.userCode, nil }

func (f *seamFlow) Wait(ctx context.Context) (*oauth.Token, error) {
	select {
	case <-time.After(f.after):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.result != "ok" {
		return nil, errors.New(f.result)
	}
	return seamToken(), nil
}

func (f *seamFlow) Close() {}

// seamCodeFlow is a seamFlow that also finishes with its pasted code; with
// no after_ms it waits for the code alone.
type seamCodeFlow struct {
	*seamFlow
	code string
}

func (f *seamCodeFlow) Wait(ctx context.Context) (*oauth.Token, error) {
	if f.after == 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.seamFlow.Wait(ctx)
}

func (f *seamCodeFlow) TakesCode() bool { return true }

func (f *seamCodeFlow) CompleteWithCode(_ context.Context, input string) (*oauth.Token, error) {
	if input != f.code {
		return nil, errors.New("the code was not accepted")
	}
	return seamToken(), nil
}

// seamToken is the stand-in sign-in's token.
func seamToken() *oauth.Token {
	return &oauth.Token{
		AccessToken: "crushtest-access", RefreshToken: "crushtest-refresh",
		ExpiresIn: 3600, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
}
