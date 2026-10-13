package cmd

import (
	"errors"
	"os"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill on|off <name>",
	Short: "Turn a skill on or off for the whole chat from inside a Crush session",
	Long: `Turn a skill on or off for the whole Crush chat. Only works from an agent that Crush is running.
A skill that is on is given to the agent every turn until it is turned off. The agent can turn off only skills it turned on itself.`,
	Example: `crush skill on teach
crush skill off teach`,
	Args:      cobra.ExactArgs(2),
	ValidArgs: []string{"on", "off"},
	RunE: func(cmd *cobra.Command, args []string) error {
		state := strings.ToLower(args[0])
		if state != "on" && state != "off" {
			return errors.New(`the first argument must be "on" or "off"`)
		}
		dir, session := os.Getenv(agent.TasksDirEnv), os.Getenv(agent.TasksSessionEnv)
		if dir != "" && session == "" {
			return errors.New("sub-agents can't change the chat's skills; mention the skill in your final report instead")
		}
		if dir == "" || session == "" {
			return errors.New("not running inside a Crush session")
		}
		reply, err := sendTaskRequest(dir, agent.TaskRequest{Session: session, Skill: &agent.SkillRequest{Name: args[1], On: state == "on"}})
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		cmd.Println(reply.Message)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(skillCmd)
}
