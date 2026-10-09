package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

func newBackgroundCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bg [flags] -- command [arguments...]",
		Short:   "Run a tracked background command inside a Crush session",
		Example: "crush bg -- 'timeout 600 go test ./...'\ncrush bg --service -- 'npm run dev'\ncrush bg --output 001\ncrush bg --stop 001",
		RunE: func(cmd *cobra.Command, args []string) error {
			directory, session := os.Getenv(agent.TasksDirEnv), os.Getenv(agent.TasksSessionEnv)
			if directory == "" || session == "" {
				return errors.New("not running inside a Crush session")
			}
			request := agent.BackgroundRequest{}
			request.Name, _ = cmd.Flags().GetString("name")
			request.Service, _ = cmd.Flags().GetBool("service")
			request.OutputID, _ = cmd.Flags().GetString("output")
			request.StopID, _ = cmd.Flags().GetString("stop")
			if len(args) == 1 {
				request.Command = args[0]
			} else if len(args) > 1 {
				quoted := make([]string, len(args))
				for i, arg := range args {
					var err error
					quoted[i], err = syntax.Quote(arg, syntax.LangBash)
					if err != nil {
						return err
					}
				}
				request.Command = strings.Join(quoted, " ")
			}
			operations := 0
			for _, value := range []string{request.Command, request.OutputID, request.StopID} {
				if strings.TrimSpace(value) != "" {
					operations++
				}
			}
			if operations != 1 {
				return errors.New("provide exactly one command, --output ID, or --stop ID")
			}
			if request.Service && strings.TrimSpace(request.Command) == "" {
				return errors.New("--service requires a command to start")
			}
			var err error
			request.WorkingDir, err = os.Getwd()
			if err != nil {
				return err
			}
			// Leave time for the normal permission dialog, without stalling the hub.
			reply, err := sendTaskRequestTimeout(directory, agent.TaskRequest{Session: session, Background: &request}, 10*time.Minute)
			if err != nil {
				return err
			}
			if reply.Error != "" {
				return errors.New(reply.Error)
			}
			if reply.Background == nil {
				return errors.New("Crush returned no background job result")
			}
			if reply.Background.IsError {
				return errors.New(reply.Background.Content)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), reply.Background.Content)
			return err
		},
	}
	cmd.Flags().Bool("service", false, "Keep this long-running service from delaying finish notifications")
	cmd.Flags().String("name", "", "Short description of the job")
	cmd.Flags().String("output", "", "Read output from a job")
	cmd.Flags().String("stop", "", "Stop a job")
	return cmd
}

func init() { rootCmd.AddCommand(newBackgroundCommand()) }
