package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask",
	Short: "Ask the user questions in Crush's question form from inside a Crush session",
	Long: `Ask the user questions in Crush's question form. Only works from an agent CLI that Crush is running.
The questions are JSON on stdin, in the format of Crush's question tool. The command returns right away; the answers reach the session as a message once the user submits.`,
	Example: `crush ask <<'EOF'
{"questions":[{"type":"single_choice","label":"Database","question":"Which database?","description":"Picks the storage layer.","choices":[{"id":"pg","label":"Postgres"},{"id":"sqlite","label":"SQLite"}]}]}
EOF`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir, session := os.Getenv(agent.TasksDirEnv), os.Getenv(agent.TasksSessionEnv)
		if dir != "" && session == "" {
			return errors.New("sub-agents can't ask the user questions; put your question in your final report so the agent that started you can answer it or ask the user")
		}
		if dir == "" || session == "" {
			return errors.New("not running inside a Crush session that can ask questions")
		}
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return errors.New("stdin must be the questions as JSON")
		}
		reply, err := sendTaskRequest(dir, agent.TaskRequest{Session: session, Ask: data})
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		cmd.Printf("The questions are open in Crush. The user's answers will arrive as a <%s> message named %q. End your turn now instead of waiting for them.\n",
			agent.TaskNotificationTag, agent.AskName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(askCmd)
}
