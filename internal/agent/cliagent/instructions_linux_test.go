//go:build linux

package cliagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestInstructionIsolationPreservesGitAndSharedFiles(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap is not installed")
	}
	if _, err := os.Stat("/dev/fuse"); err != nil {
		t.Skip("FUSE is not available")
	}
	cat, err := exec.LookPath("cat")
	require.NoError(t, err)
	cat, err = filepath.EvalSymlinks(cat)
	require.NoError(t, err)
	view, err := newInstructionView(func(ctx context.Context) bool {
		caller, ok := fuse.FromContext(ctx)
		require.True(t, ok)
		path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", caller.Pid))
		return err != nil || path == cat
	})
	require.NoError(t, err)
	sharedInstructionView.Lock()
	previous := sharedInstructionView.view
	sharedInstructionView.view = view
	sharedInstructionView.Unlock()
	t.Cleanup(func() {
		sharedInstructionView.Lock()
		sharedInstructionView.view = previous
		sharedInstructionView.Unlock()
		require.NoError(t, view.close())
	})
	home := t.TempDir()
	project := filepath.Join(home, "project")
	write := func(relative, data string) string {
		path := filepath.Join(home, relative)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
		return path
	}
	global := write(".codex/AGENTS.md", "personal guidance\n")
	projectDoc := write("project/AGENTS.md", "project guidance\n")
	nested := write("project/src/CLAUDE.md", "nested guidance\n")
	rule := write(".claude/rules/behavior.md", "personal rule\n")
	projectRule := write("project/.claude/rules/behavior.md", "project rule\n")
	commandRule := write(".codex/rules/default.rules", "command policy fixture\n")
	credential := write(".codex/auth.json", "authentication fixture\n")
	skill := write(".agents/skills/example/SKILL.md", "shared skill\n")
	memory := write(".local/share/agent-memory/MEMORY.md", "shared memory\n")
	config := write(".config/opencode/opencode.jsonc", "{\n// comment\n\"instructions\":[\"extra.md\"],\"permission\":{\"bash\":\"ask\"}}")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "GROK_HOME", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG"} {
		t.Setenv(key, "")
	}
	run := func(name string, args ...string) string {
		cmd, cleanup, err := instructionCommand(project, home, name, args...)
		require.NoError(t, err)
		defer cleanup()
		cmd.Dir = project
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", name, out)
		return string(out)
	}
	for _, path := range []string{global, projectDoc, nested, rule, projectRule} {
		original, err := os.ReadFile(path)
		require.NoError(t, err)
		// Alternate readers to exercise the kernel cache, not just one read.
		for range 2 {
			require.Empty(t, run("cat", path))
			require.Equal(t, string(original), run("head", "-c", "1000", path))
		}
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, original, after)
	}
	for _, path := range []string{commandRule, credential, skill, memory} {
		original, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, string(original), run("cat", path))
	}
	require.JSONEq(t, `{"instructions":[],"permission":{"bash":"ask"}}`, run("cat", config))
	configOriginal, err := os.ReadFile(config)
	require.NoError(t, err)
	require.Equal(t, string(configOriginal), run("head", "-c", "1000", config))

	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", project}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	git("init", "-q")
	git("add", "AGENTS.md", ".claude/rules/behavior.md")
	before := git("status", "--porcelain")
	require.Equal(t, before, run("git", "status", "--porcelain"))
	run("git", "add", "AGENTS.md", ".claude/rules/behavior.md")
	require.Equal(t, "project guidance\n", git("show", ":AGENTS.md"))
	require.Equal(t, "project rule\n", git("show", ":.claude/rules/behavior.md"))
	require.Equal(t, before, git("status", "--porcelain"))
}

func TestInstructionMasksKeepSharedTargets(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, ".agents", "skills", "example")
	require.NoError(t, os.MkdirAll(shared, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(shared, "AGENTS.md"), []byte("skill instructions"), 0o600))
	masks, err := instructionMasks(home, home)
	require.NoError(t, err)
	require.NotContains(t, masks, filepath.Join(shared, "AGENTS.md"))
}

func TestInstructionWrapperKeepsBackgroundCommands(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap is not installed")
	}
	// Exercise process ancestry with the real wrapper, without CLI models.
	cmd := exec.Command("bwrap", "--bind", "/", "/", "--dev-bind", "/dev", "/dev", "--", "sh", "-c", "setsid sleep 31 & setsid sleep 32 & echo ready; wait")
	p, err := startProcCommand(cmd, t.TempDir(), nil)
	require.NoError(t, err)
	defer p.finish()
	defer p.kill()
	at := time.Now()
	require.True(t, p.lines.Scan())
	var cli int
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(p.cmd.Process.Pid)).Output()
	require.NoError(t, err)
	cli, err = strconv.Atoi(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	child := func(command string) int {
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(cli), "-fx", command).Output()
		require.NoError(t, err)
		pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		require.NoError(t, err)
		return pid
	}
	background, foreground := child("sleep 31"), child("sleep 32")
	defer syscall.Kill(-background, syscall.SIGKILL)
	defer syscall.Kill(-foreground, syscall.SIGKILL)
	p.killCommands([]openCall{{command: "sleep 32", words: []string{"sleep", "32"}, at: at}})
	require.Eventually(t, func() bool { return syscall.Kill(foreground, 0) != nil }, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, syscall.Kill(background, 0))
}

func TestInstalledInstructionIsolation(t *testing.T) {
	if os.Getenv("CRUSH_TEST_INSTALLED_CLIS") != "1" {
		t.Skip("opt-in local smoke test; never run real CLIs in the normal suite")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	defer CloseInstructionViews()
	for _, name := range []string{"claude", "codex", "grok", "opencode", "agy"} {
		t.Run(name, func(t *testing.T) {
			args := []string{"--help"}
			if name == "codex" {
				args = []string{"app-server", "--disable", "memories", "-c", "project_doc_max_bytes=0", "--help"}
			}
			cmd, cleanup, err := instructionCommand(home, home, name, args...)
			require.NoError(t, err)
			defer cleanup()
			cmd.Dir = home
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.NotEmpty(t, out)
			if name == "opencode" {
				probe, release, err := instructionCommand(home, home, name, "debug", "config")
				require.NoError(t, err)
				defer release()
				probe.Dir = home
				resolved, err := probe.Output()
				require.NoError(t, err)
				// Never log resolved config: it can contain credentials.
				require.True(t, gjson.ValidBytes(resolved))
				require.Zero(t, len(gjson.GetBytes(resolved, "instructions").Array()))
			}
		})
	}
}
