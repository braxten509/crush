package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/spf13/cobra"
)

var secureEntryCmd = &cobra.Command{
	Use:   "secure-entry --file PATH",
	Short: "Open a local masked dialog that writes a secret directly into a file",
	Long: `Open a local masked dialog. The file must already contain a literal placeholder (default %s).
One occurrence is replaced per dialog. No entered value is returned to the agent,
written to request files, or sent through question, chat, or remote APIs.
Replacement is literal; prepare the surrounding file format beforehand.
The destination is saved with owner-only permissions (0600).
Never pass secrets in arguments, stdin, or chat. Do not read the populated file
through agent tools afterward. This is an entry channel, not a sandbox preventing
programs running as your user from later reading the saved file.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir, session := os.Getenv(agent.TasksDirEnv), os.Getenv(agent.TasksSessionEnv)
		if dir == "" || session == "" {
			return errors.New("secure entry requires a main agent running inside Crush")
		}
		path, _ := cmd.Flags().GetString("file")
		if path == "" {
			return errors.New("--file is required")
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return errors.New("cannot resolve destination path")
		}
		label, _ := cmd.Flags().GetString("label")
		placeholder, _ := cmd.Flags().GetString("placeholder")
		occurrence, _ := cmd.Flags().GetInt("occurrence")
		reply, err := sendTaskRequest(dir, agent.TaskRequest{Session: session, SecureEntry: &secureentry.Spec{File: path, Label: label, Placeholder: placeholder, Occurrence: occurrence}})
		if err != nil {
			return err
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		cmd.Println("Secure entry is open in the local Crush terminal. End your turn; only saved/cancelled status will return as a Secure entry task result. Never read or print the destination file after entry.")
		return nil
	},
}

func init() {
	secureEntryCmd.Flags().String("file", "", "Existing template file to edit")
	secureEntryCmd.Flags().String("label", "API key", "Name of the key requested")
	secureEntryCmd.Flags().String("placeholder", "%s", "Literal placeholder to replace")
	secureEntryCmd.Flags().Int("occurrence", 1, "One-based occurrence of the remaining placeholder")
	rootCmd.AddCommand(secureEntryCmd)
}
