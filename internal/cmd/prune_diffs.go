package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/spf13/cobra"
)

var pruneDiffsCmd = &cobra.Command{
	Use:    "prune-diffs",
	Short:  "Delete saved diffs of chats unused for three days",
	Hidden: true,
	Long: `Deletes the saved file diffs of every chat, in every known project, that
has not been used for three days. Chats, their messages and their short change
summaries stay. A daily systemd user timer runs it; Crush also prunes the
open project when it starts.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		list, err := projects.Load()
		if err != nil {
			return fmt.Errorf("failed to load projects: %w", err)
		}
		cutoff := time.Now().AddDate(0, 0, -db.ReviewIdleDays)
		seen := map[string]bool{}
		for _, project := range list.Projects {
			path := filepath.Join(project.DataDir, "crush.db")
			if seen[path] {
				continue
			}
			seen[path] = true
			removed, err := db.PruneIdleReviewsAt(cmd.Context(), path, cutoff)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %s: %v\n", path, err)
				continue
			}
			if removed > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: removed %d saved diffs\n", project.Path, removed)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pruneDiffsCmd)
}
