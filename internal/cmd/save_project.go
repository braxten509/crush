package cmd

import (
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/spf13/cobra"
)

var saveProjectCmd = &cobra.Command{
	Use:   "save-project",
	Short: "Save current directory as a project",
	Long:  "Register the current working directory as a Crush project for future access",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")

		store, err := config.Init(cwd, dataDir, false)
		if err != nil {
			return err
		}

		cfg := store.Config()

		if err := projects.Register(cwd, cfg.Options.DataDirectory); err != nil {
			return err
		}

		cmd.Printf("Saved project: %s\n", cwd)
		return nil
	},
}
