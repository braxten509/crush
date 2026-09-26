package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/spf13/cobra"
)

var openProjectCmd = &cobra.Command{
	Use:   "open-project [number]",
	Short: "Start Crush in a saved project",
	Long:  "List your saved projects and start a Crush session in the one you pick",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		saved, err := projects.SavedList()
		if err != nil {
			return err
		}
		if len(saved) == 0 {
			cmd.Println("No saved projects yet. Use 'crush save-project' or Save Project inside Crush.")
			return nil
		}

		choice := ""
		if len(args) == 1 {
			choice = args[0]
		} else {
			for i, p := range saved {
				cmd.Printf("%2d  %s\n", i+1, home.Short(p.Path))
			}
			cmd.Print("\nSelect a project (number): ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			choice = strings.TrimSpace(line)
		}

		n, err := strconv.Atoi(choice)
		if err != nil || n < 1 || n > len(saved) {
			return fmt.Errorf("invalid selection %q", choice)
		}
		return relaunch(saved[n-1].Path, relaunchArgs(cmd, saved[n-1].Path))
	},
}
