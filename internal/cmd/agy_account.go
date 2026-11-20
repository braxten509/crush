// Runs AGY under the same account profile for sign-in, turns and usage.
package cmd

import (
	"errors"

	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:    "__agy-account",
		Hidden: true,
		Args:   cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != "login" && args[1] != "run" {
				return errors.New("invalid AGY account command")
			}
			return accounts.RunAGYProfile(cmd.Context(), args[0], args[2], args[3:], args[1] == "login")
		},
	})
}
