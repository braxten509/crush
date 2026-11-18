package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/agent/tools"
)

// agyBin finds the real agy binary; shells often wrap it in a function
// that adds flags we don't want.
func agyBin() string {
	if home, err := os.UserHomeDir(); err == nil {
		if bin := filepath.Join(home, ".local", "bin", "agy"); isExec(bin) {
			return bin
		}
	}
	return "agy"
}

func isExec(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// runAGY drives AGY through its ACP server ("agy acp"). Its print mode
// holds a message sent mid-turn until the whole turn ends, even with its
// "send immediately" setting; the ACP server folds it into the running
// turn at the model's next step (agy 1.5.11). Print-mode conversations
// open with session/resume, so older chats carry on.
func runAGY(ctx context.Context, m *Model, t Turn) error {
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

// agyBackgroundNote is the result shown for a command moved to the
// background with Ctrl+B or the user's background time. AGY itself moves a
// slow command to its own background and carries on; the call's real end
// is not shown again.
const agyBackgroundNote = "Still running. AGY moved this command to its own background and carries on when it finishes."

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
		content, _ := os.ReadFile(in.FilePath)
		return marshal(tools.ViewResponseMetadata{FilePath: in.FilePath, Content: clipBytes(content, 64<<10)})
	case tools.EditToolName, tools.WriteToolName:
		return ""
	}
	return resultMetadata(name, input, output)
}

func clipBytes(b []byte, max int) string {
	if len(b) > max {
		b = b[:max]
	}
	return string(b)
}
