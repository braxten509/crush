package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var openProjectCmd = &cobra.Command{
	Use:   "open-project",
	Short: "Open and start a session in a saved project",
	Long:  "List your saved projects and start a Crush session in one of them",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectList, err := projects.List()
		if err != nil {
			return err
		}

		if len(projectList) == 0 {
			cmd.Println("No projects tracked yet. Use 'crush save-project' to save one.")
			return nil
		}

		if term.IsTerminal(os.Stdout.Fd()) {
			// Interactive mode: show table and prompt for selection
			t := table.New().
				Border(lipgloss.RoundedBorder()).
				StyleFunc(func(row, col int) lipgloss.Style {
					return lipgloss.NewStyle().Padding(0, 2)
				}).
				Headers("#", "Path", "Last Accessed")

			for i, p := range projectList {
				t.Row(
					fmt.Sprintf("%d", i+1),
					p.Path,
					p.LastAccessed.Local().Format("2006-01-02 15:04"),
				)
			}
			lipgloss.Println(t)

			cmd.Print("\nSelect a project (number): ")

			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = input[:len(input)-1] // Remove newline

			selected, err := strconv.Atoi(input)
			if err != nil || selected < 1 || selected > len(projectList) {
				cmd.Println("Invalid selection")
				return nil
			}

			selectedProject := projectList[selected-1]
			cmd.Printf("Opening project: %s\n", selectedProject.Path)

			// Register the access time
			if err := projects.Register(selectedProject.Path, selectedProject.DataDir); err != nil {
				return err
			}

			// Output cd command that can be sourced
			cmd.Printf("cd '%s'\n", selectedProject.Path)
			return nil
		}

		// Non-interactive mode: output simple list
		for i, p := range projectList {
			cmd.Printf("%d\t%s\t%s\n", i+1, p.Path, p.LastAccessed.Format("2006-01-02T15:04:05Z07:00"))
		}
		return nil
	},
}
