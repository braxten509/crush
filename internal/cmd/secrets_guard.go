package cmd

import (
	"os"

	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/spf13/cobra"
)

var secretsGuardCmd = &cobra.Command{
	Use:    "secrets-guard",
	Short:  "PreToolUse hook that blocks agent tool calls reading secrets folders",
	Hidden: true,
	Long: `Reads a PreToolUse hook event on stdin, as Crush, Claude Code and Codex send it.
Exits 2 with a reason on stderr when the tool call would read a secrets folder
(like ~/.config/secrets or ~/.config/crush/secrets), and 0 otherwise. Crush adds
it to the CLI agents it starts; it can also be listed as a hook by hand.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		os.Exit(secretguard.RunHook(cmd.InOrStdin(), cmd.ErrOrStderr()))
	},
}

func init() {
	rootCmd.AddCommand(secretsGuardCmd)
}
