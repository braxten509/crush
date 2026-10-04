package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/spf13/cobra"
)

var spawnCmd = &cobra.Command{
	Use:   "spawn [prompt]",
	Short: "Start a background sub-agent from inside a Crush session",
	Long: `Start a background sub-agent from inside a Crush session. Only works from an agent CLI that Crush is running.
The prompt comes from the argument or stdin. Crush reports the result back to the session when the sub-agent finishes.`,
	Example: `crush spawn --cli codex --name "Review auth" <<'EOF'
Review internal/auth for bugs and report what you find.
EOF

crush spawn --cli claude --model opus --effort max --fast --name "Compose a tune" <<'EOF'
Write the song.
EOF

crush spawn --continue t2 <<'EOF'
Also check the error paths you skipped.
EOF

crush spawn --stop t2`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, session := os.Getenv(agent.TasksDirEnv), os.Getenv(agent.TasksSessionEnv)
		if dir == "" || session == "" {
			return errors.New("not running inside a Crush session that can start sub-agents")
		}
		req := agent.TaskRequest{Session: session}
		req.Stop, _ = cmd.Flags().GetString("stop")
		req.Continue, _ = cmd.Flags().GetString("continue")
		if req.Stop == "" {
			req.CLI, _ = cmd.Flags().GetString("cli")
			req.Model, _ = cmd.Flags().GetString("model")
			req.Effort, _ = cmd.Flags().GetString("effort")
			req.Fast, _ = cmd.Flags().GetBool("fast")
			req.Name, _ = cmd.Flags().GetString("name")
			if len(args) > 0 {
				req.Prompt = args[0]
			} else {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				req.Prompt = string(data)
			}
			if req.CLI == "" && req.Continue == "" {
				return errors.New("--cli is required")
			}
			if strings.TrimSpace(req.Prompt) == "" {
				return errors.New("the prompt is empty: pass it as an argument or on stdin")
			}
		}

		reply, err := sendTaskRequest(dir, req)
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		t := reply.Task
		if req.Stop != "" {
			cmd.Printf("Stopped task %s (%s).\n", t.ID, t.Name)
			return nil
		}
		verb := "Started"
		if req.Continue != "" {
			verb = "Continued"
		}
		tuning := ""
		if t.Effort != "" {
			tuning += ", effort " + t.Effort
		}
		if t.Fast {
			tuning += ", fast"
		}
		cmd.Printf("%s task %s (%s) on %s/%s%s. It runs in the background; its result will arrive as a <%s> message when it finishes. Don't wait for it.\n",
			verb, t.ID, t.Name, t.CLI, t.Model, tuning, agent.TaskNotificationTag)
		return nil
	},
}

func init() {
	spawnCmd.Flags().String("cli", "", "Agent CLI to run the sub-agent on (claude, codex, grok, opencode, agy)")
	spawnCmd.Flags().String("model", "", "Model to use (defaults to the CLI's first model)")
	spawnCmd.Flags().String("effort", "", "Reasoning effort, one of the model's levels (like low, medium, high, xhigh, max)")
	spawnCmd.Flags().Bool("fast", false, "Run in fast mode (claude and codex only)")
	spawnCmd.Flags().String("name", "", "Short title shown in Crush")
	spawnCmd.Flags().String("stop", "", "Stop the task with this ID instead")
	spawnCmd.Flags().String("continue", "", "Send the prompt as a follow-up to this finished task; it keeps its conversation")
	rootCmd.AddCommand(spawnCmd)
}

// sendTaskRequest drops a request into Crush's task directory and waits for
// its reply.
func sendTaskRequest(dir string, req agent.TaskRequest) (agent.TaskReply, error) {
	return sendTaskRequestTimeout(dir, req, 30*time.Second)
}

func sendTaskRequestTimeout(dir string, req agent.TaskRequest, timeout time.Duration) (agent.TaskReply, error) {
	var reply agent.TaskReply
	data, err := json.Marshal(req)
	if err != nil {
		return reply, err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	tmp := filepath.Join(dir, id+".new")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return reply, fmt.Errorf("can't reach Crush: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, id+".req")); err != nil {
		return reply, fmt.Errorf("can't reach Crush: %w", err)
	}
	ack := filepath.Join(dir, id+".ack")
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		data, err := os.ReadFile(ack)
		if err != nil {
			continue
		}
		_ = os.Remove(ack)
		return reply, json.Unmarshal(data, &reply)
	}
	_ = os.Remove(filepath.Join(dir, id+".req"))
	return reply, errors.New("Crush did not answer; is it still running?")
}
