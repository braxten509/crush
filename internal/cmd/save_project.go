package cmd

import (
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/spf13/cobra"
)

var saveProjectCmd = &cobra.Command{
	Use:   "save-project",
	Short: "Save current directory as a project",
	Long:  "Save the current working directory so it shows up in Open Project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}
		if err := projects.MarkSaved(cwd); err != nil {
			return err
		}
		cmd.Printf("Saved project: %s\n", cwd)
		return nil
	},
}
