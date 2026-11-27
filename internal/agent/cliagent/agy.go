package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/secretguard"
)

// agyBin finds the real agy binary; shells often wrap it in a function
// that adds flags we don't want.
func agyBin() string {
	return accounts.AGYBin()
}

// runAGY drives AGY through its ACP server ("agy acp"). Its print mode
// holds a message sent mid-turn until the whole turn ends, even with its
// "send immediately" setting; the ACP server folds it into the running
// turn at the model's next step (agy 1.5.11). Print-mode conversations
// open with session/resume, so older chats carry on.
func runAGY(ctx context.Context, m *Model, t Turn) error {
	if !t.NoTools {
		if err := ensureAGYGuardHook(); err != nil {
			return fmt.Errorf("installing AGY guard hook: %w", err)
		}
	}
	args := []string{"acp"}
	name := agyBin()
	if m.Guarded && !m.ReadOnly && !t.NoTools {
		// AGY runs every command without asking and can't hand them to
		// Crush, and its own sandbox falls back to running on the host.
		// Codex's sandbox keeps it to the project (plus its own state and
		// the network), which also rules out sudo.
		if _, err := exec.LookPath("codex"); err != nil {
			return errors.New("AGY sub-agents run inside Codex's sandbox, and codex isn't installed")
		}
		state := filepath.Join(os.Getenv("HOME"), ".gemini")
		args = append([]string{
			"sandbox",
			"-c", `sandbox_mode="workspace-write"`,
			"-c", "sandbox_workspace_write.network_access=true",
			"-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q]", state),
			"--", name,
		}, args...)
		name = "codex"
	}
	return runACPWith(ctx, m, t, "agy", name, args, true)
}

// diskMetadata is resultMetadata for CLIs that don't return file contents:
// views read the file itself. Edit diffs are filled in by the caller from
// before/after snapshots.
func diskMetadata(name, input, output string) string {
	switch name {
	case tools.ViewToolName:
		var in struct {
			FilePath string `json:"file_path"`
		}
		_ = json.Unmarshal([]byte(input), &in)
		if imageFilename(in.FilePath) {
			return marshal(tools.ViewResponseMetadata{FilePath: in.FilePath})
		}
		content, _ := os.ReadFile(in.FilePath)
		return marshal(tools.ViewResponseMetadata{FilePath: in.FilePath, Content: clipBytes(content, 64<<10)})
	case tools.EditToolName, tools.WriteToolName:
		return ""
	}
	return resultMetadata(name, input, output)
}

var agyHooksMu sync.Mutex

// ensureAGYGuardHook keeps Crush's guard hook registered in Antigravity's
// global hooks.json without removing any other hooks the user configured.
func ensureAGYGuardHook() error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	return ensureAGYGuardHookAt(filepath.Join(home, ".gemini", "config", "hooks.json"), secretguard.HookCommand())
}

func ensureAGYGuardHookAt(path, command string) error {
	agyHooksMu.Lock()
	defer agyHooksMu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lockFile, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	hooks := map[string]any{}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &hooks); err != nil {
			return fmt.Errorf("parsing existing AGY hooks.json at %s: %w", path, err)
		}
		if hooks == nil {
			hooks = map[string]any{}
		}
	}
	hooks["crush-secrets-guard"] = map[string]any{
		"PreToolUse": []any{
			map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": command,
						"timeout": 10,
					},
				},
			},
		},
	}
	// The shell hook only rewrites commands for an active Crush chat. It
	// leaves Agy's permission decision and the separate guard in place.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	hooks["crush-agy-shell"] = map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": "^run_command$",
			"hooks": []any{map[string]any{
				"type": "command", "command": "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' __agy-shell-hook", "timeout": 10,
			}},
		}},
	}
	data, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if bytes.Equal(old, data) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, "hooks-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// usageLimitPattern recognizes a usage-limit error that names no reset time.
// An account its provider refuses outright (Claude Code: "can't use your
// Claude subscription ... payment is past due") counts too, so the order moves
// on to the next account instead of failing the turn.
var usageLimitPattern = regexp.MustCompile(`(?i)usage limit|limit reached|hit your limit|out of (extra )?(?:usage|quota|credits)|rate.?limit|quota (?:exceeded|reached|exhausted)|exhausted your quota|credits balance is too low|prepaid balance is insufficient|can.t use your \w+ subscription|past due`)

// agyResetWarningPattern recognizes a reset time inside an AGY warning chunk
// ("Resets in 112h43m38s", "try again at 5:20 PM").
var agyResetWarningPattern = regexp.MustCompile(`(?i)\b(?:resets?|try again|available again|retry)\s+(?:(?:in|at|on)\s+)?(?:\d|(?:mon|tue|wed|thu|fri|sat|sun|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?,?\s)`)

// MatchesUsageLimitText reports whether text names a usage, quota, credits,
// or billing limit without relying on a reset time.
func MatchesUsageLimitText(text string) bool {
	return usageLimitPattern.MatchString(text)
}

// agyUsageWarning extracts the reason from an AGY ACP warning chunk
// ("\n⚠ Individual quota reached... Resets in 1h20m26s.\n") when it reports
// running out of usage or quota. Other warnings stay normal chat text.
func agyUsageWarning(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	rest, ok := strings.CutPrefix(trimmed, "⚠")
	if !ok {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "\ufe0f")
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	body := strings.TrimSpace(rest)
	if body == "" || strings.Count(body, "\n") > 2 {
		return "", false
	}
	if !MatchesUsageLimitText(body) && !agyResetWarningPattern.MatchString(body) {
		return "", false
	}
	return body, true
}

func clipBytes(b []byte, max int) string {
	if len(b) > max {
		b = b[:max]
	}
	return string(b)
}
