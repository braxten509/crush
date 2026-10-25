package cliagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
)

// tryPrompt is the hidden request TryAccount sends.
const tryPrompt = "Reply with the word OK."

// tryTimeout bounds TryAccount; a variable so tests can shorten it.
var tryTimeout = 90 * time.Second

// TryAccount sends one tiny request to model on an account, outside every
// chat: no session is saved, and the tool runs in an empty folder without
// its tools, hooks, MCP servers or user config. The fallback order asks for
// it when an account's usage can't be read (owner, 2026-10-21: "try it
// silently not in the chat"). nil means the account answered; an error
// carries the tool's own words, so a usage limit reads as one. Tools other
// than Claude Code and Codex are not tried and report nil.
func TryAccount(ctx context.Context, kind catwalk.Type, account, model string) error {
	var name string
	var args []string
	switch kind {
	case config.TypeClaudeCode:
		name = "claude"
		args = []string{"-p", "--output-format", "json", "--no-session-persistence", "--setting-sources", "project",
			"--strict-mcp-config", "--disable-slash-commands", "--tools", ""}
	case config.TypeCodexCLI:
		name = "codex"
		args = []string{"exec", "--json", "--ephemeral", "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules",
			"--sandbox", "read-only", "-c", `model_reasoning_effort="low"`}
	default:
		return nil
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, tryPrompt)

	ctx, cancel := context.WithTimeout(ctx, tryTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "crush-try-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// Stdin stays empty (nil is the null device): both tools wait for piped
	// input otherwise.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), config.AccountEnv(kind, account)...)
	// Past the limit the tool's own children go too, and a child that
	// still holds the output can't keep the try waiting.
	ownGroup(cmd)
	cmd.Cancel = func() error {
		(&proc{cmd: cmd}).kill()
		return nil
	}
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if why := tryFailure(kind, out); why != "" {
		return fmt.Errorf("%s: %s", name, why)
	}
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s did not answer within %s", name, tryTimeout)
		}
		return fmt.Errorf("%s: %s", name, lastLines(strings.TrimSpace(string(out)), 5))
	}
	return nil
}

// tryFailure is the tool's own words for a request that failed, read from
// its JSON output: Claude's result with is_error, Codex's last error or
// turn.failed event. "" when it reports none.
func tryFailure(kind catwalk.Type, out []byte) string {
	why := ""
	for line := range bytes.Lines(out) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var ev struct {
			Type    string          `json:"type"`
			IsError bool            `json:"is_error"`
			Result  string          `json:"result"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"` // an object in Codex's turn.failed
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch {
		case kind == config.TypeClaudeCode && ev.Type == "result" && ev.IsError:
			why = ev.Result
		case kind == config.TypeCodexCLI && ev.Type == "error":
			why = ev.Message
		case kind == config.TypeCodexCLI && ev.Type == "turn.failed":
			var failed struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(ev.Error, &failed) == nil {
				why = failed.Message
			}
		}
	}
	return strings.TrimSpace(why)
}
