package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/spf13/cobra"
)

var selfUpdateCmd = &cobra.Command{
	Use:    "self-update [check|merge|build|install]",
	Short:  "Update this fork of Crush to upstream's latest stable release",
	Hidden: true,
	Long: `Merges upstream's latest stable release into the fork's checkout, builds it,
runs every test and installs it. The TUI offers this at startup; these steps
let an agent or a person run it by hand:

  check    show whether a newer stable release exists
  merge    merge it into the checkout (lists clashing files)
  build    build the merged checkout and run every test
  install  install the tested build and upload the merge to GitHub`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"check", "merge", "build", "install"},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		dir := selfupdate.Dir()
		if dir == "" {
			return errors.New("this build has no fork checkout to update")
		}
		if args[0] == "install" {
			installed, pushErr, err := selfupdate.Install(ctx, dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed Crush %s. Restart Crush to use it.\n", installed)
			return pushErr
		}
		release, err := selfupdate.Check(ctx, dir)
		if err != nil {
			return err
		}
		if release == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Crush is on the latest stable release.")
			return nil
		}
		switch args[0] {
		case "check":
			fmt.Fprintf(cmd.OutOrStdout(), "Crush %s is out (this build is based on %s).\n", release.Tag, release.Current)
		case "merge":
			clashes, err := selfupdate.Merge(ctx, dir, release.Tag)
			if err != nil {
				return err
			}
			if len(clashes) > 0 {
				return fmt.Errorf("merging %s clashes in: %v", release.Tag, clashes)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Merged %s.\n", release.Tag)
		case "build":
			if err := selfupdate.Build(ctx, dir, release.Tag, os.Stderr); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Built %s and every test passed. It is ready to install.\n", release.Tag)
		default:
			return fmt.Errorf("unknown step %q", args[0])
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
}
