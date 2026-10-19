package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/stretchr/testify/require"
)

// TestSelfUpdateRunJSON: `crush self-update run --tag --json` runs the
// terminal's whole background update on a fake fork checkout (whose
// upstream is a local repository): the release clashes, the fixing agent
// (a stand-in, no real AI tool) resolves it and builds, and the result
// comes back as JSON; `install --json` reports in the same shape (G-19).
func TestSelfUpdateRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(name, value)
	}
	// A stand-in systemd-run runs the wrapped command here, so the test run
	// starts no transient unit.
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "systemd-run"),
		[]byte("#!/bin/sh\nwhile [ \"$1\" != go ]; do shift; done\nexec \"$@\"\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	checkout := selfUpdateFixture(t, filepath.Join(home, "dev", "crush"))
	require.Equal(t, checkout, selfupdate.Dir())

	var agentArgs []string
	t.Cleanup(selfupdate.SetFixAgentForTest(func(ctx context.Context, dir string, log io.Writer, args []string) error {
		agentArgs = args
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.txt"), []byte("one\nfork and upstream two\nthree\n"), 0o644))
		selfUpdateGit(t, dir, "add", "app.txt")
		selfUpdateGit(t, dir, "commit", "-q", "--no-edit")
		return selfupdate.Build(ctx, dir, "v1.1.0", log)
	}))

	result := runSelfUpdateJSON(t, "run", "v1.1.0")
	require.True(t, result.OK, result.FirstLine)
	require.Empty(t, result.FirstLine)
	require.Equal(t, []string{"app.txt"}, result.Fixed)
	require.True(t, strings.HasPrefix(result.Version, "v1.1.0-local."), result.Version)
	require.Equal(t, filepath.Join(state, "crush", "update", "update.log"), result.LogPath)
	log, err := os.ReadFile(result.LogPath)
	require.NoError(t, err)
	require.Contains(t, string(log), "Clashes in app.txt; an AI agent is fixing them.")
	require.Contains(t, agentArgs, "--model")
	require.Contains(t, agentArgs[len(agentArgs)-1], "v1.1.0")
	require.Equal(t, result.Version, selfupdate.Staged(t.Context(), checkout, "v1.1.0"), "staged for install")

	failed := runSelfUpdateJSON(t, "run", "v9.9.9")
	require.False(t, failed.OK)
	require.True(t, strings.HasPrefix(failed.FirstLine, "cannot download v9.9.9"), failed.FirstLine)
	require.Empty(t, failed.Version)

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	install := runSelfUpdateJSON(t, "install", "")
	require.Equal(t, selfUpdateResult{FirstLine: "there is no tested build to install", LogPath: selfupdate.LogPath()}, install)
}

// runSelfUpdateJSON runs `crush self-update <step> --json` and decodes its
// output.
func runSelfUpdateJSON(t *testing.T, step, tag string) selfUpdateResult {
	t.Helper()
	var out bytes.Buffer
	selfUpdateCmd.SetOut(&out)
	t.Cleanup(func() {
		selfUpdateCmd.SetOut(nil)
		_ = selfUpdateCmd.Flags().Set("json", "false")
		_ = selfUpdateCmd.Flags().Set("tag", "")
	})
	require.NoError(t, selfUpdateCmd.Flags().Set("json", "true"))
	require.NoError(t, selfUpdateCmd.Flags().Set("tag", tag))
	selfUpdateCmd.SetContext(t.Context())
	require.NoError(t, selfUpdateCmd.RunE(selfUpdateCmd, []string{step}))
	var result selfUpdateResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result), out.String())
	return result
}

// selfUpdateFixture makes a local "upstream" with releases v1.0.0 and
// v1.1.0, a published fork of v1.0.0 whose change clashes with v1.1.0,
// and the fork's checkout at checkout, with the real upstream URL routed
// to the local one.
func selfUpdateFixture(t *testing.T, checkout string) string {
	t.Helper()
	root := t.TempDir()
	write := func(dir, name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	upstream := filepath.Join(root, "upstream")
	require.NoError(t, os.Mkdir(upstream, 0o755))
	selfUpdateGit(t, upstream, "init", "-q", "-b", "main")
	write(upstream, "go.mod", "module github.com/charmbracelet/crush\n")
	write(upstream, "main.go", "package main\n\nfunc main() {}\n")
	write(upstream, "app.txt", "one\ntwo\nthree\n")
	selfUpdateGit(t, upstream, "add", ".")
	selfUpdateGit(t, upstream, "commit", "-qm", "v1.0.0")
	selfUpdateGit(t, upstream, "tag", "v1.0.0")

	fork := filepath.Join(root, "fork.git")
	selfUpdateGit(t, root, "clone", "-q", "--bare", upstream, fork)
	require.NoError(t, os.MkdirAll(filepath.Dir(checkout), 0o755))
	selfUpdateGit(t, root, "clone", "-q", fork, checkout)
	selfUpdateGit(t, checkout, "config", "url."+upstream+".insteadOf", "https://github.com/charmbracelet/crush.git")
	write(checkout, "app.txt", "one\nfork two\nthree\n")
	selfUpdateGit(t, checkout, "commit", "-qam", "fork change")
	selfUpdateGit(t, checkout, "push", "-q", "origin", "main")

	write(upstream, "app.txt", "one\nupstream two\nthree\n")
	selfUpdateGit(t, upstream, "commit", "-qam", "v1.1.0")
	selfUpdateGit(t, upstream, "tag", "v1.1.0")
	return checkout
}

func selfUpdateGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
