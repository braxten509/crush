package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/spf13/cobra"
)

var selfUpdateCmd = &cobra.Command{
	Use:    "self-update [check|merge|build|install|run]",
	Short:  "Update this fork of Crush to upstream's latest stable release",
	Hidden: true,
	Long: `Merges upstream's latest stable release into the fork's checkout, builds it,
runs every test and installs it. The TUI offers this at startup; these steps
let an agent or a person run it by hand:

  check    show whether a newer stable release exists
  merge    commit local changes, then merge the update (lists clashing files)
  build    build the merged checkout and run every test
  install  install the tested build and upload the merge to GitHub
  run      the whole background update the TUI runs for --tag: commit, merge,
           build and test, with an AI agent fixing clashes and failures

With --json, run and install print one JSON object:
{"ok", "firstLine", "logPath", "version", "fixed"}.`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"check", "merge", "build", "install", "run"},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		asJSON, _ := cmd.Flags().GetBool("json")
		dir := selfupdate.Dir()
		if dir == "" {
			if asJSON && (args[0] == "run" || args[0] == "install") {
				return printSelfUpdateJSON(cmd, "", nil, errors.New("this build has no fork checkout to update"))
			}
			return errors.New("this build has no fork checkout to update")
		}
		if args[0] == "run" {
			tag, _ := cmd.Flags().GetString("tag")
			if tag == "" {
				return errors.New("run needs --tag")
			}
			fixed, version, err := selfupdate.Run(ctx, dir, tag)
			if asJSON {
				return printSelfUpdateJSON(cmd, version, fixed, err)
			}
			if err != nil {
				return fmt.Errorf("Crush %s update stopped: %w. Details: %s", tag, err, selfupdate.LogPath())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Built %s and every test passed. It is ready to install.\n", version)
			return nil
		}
		if args[0] == "install" {
			installed, pushErr, err := selfupdate.Install(ctx, dir)
			if asJSON {
				if err == nil {
					err = pushErr
				}
				return printSelfUpdateJSONInstalled(cmd, installed, err)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed Crush %s. Restart Crush to use it.\n", installed)
			return pushErr
		}
		if args[0] == "build" {
			tag, err := selfupdate.MergedTag(ctx, dir)
			if err != nil {
				return err
			}
			if err := selfupdate.Build(ctx, dir, tag, os.Stderr); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Built %s and every test passed. It is ready to install.\n", tag)
			return nil
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
		default:
			return fmt.Errorf("unknown step %q", args[0])
		}
		return nil
	},
}

// selfUpdateResult is the --json output of run and install (G-19): ok, the
// first line of what went wrong (for install, of the failed upload when ok),
// the log's path, the version built or installed, and for run the files an
// agent fixed clashes in.
type selfUpdateResult struct {
	OK        bool     `json:"ok"`
	FirstLine string   `json:"firstLine"`
	LogPath   string   `json:"logPath"`
	Version   string   `json:"version,omitempty"`
	Fixed     []string `json:"fixed,omitempty"`
}

// printSelfUpdateJSON prints run's result. The outcome is in the JSON, so
// the command itself succeeds.
func printSelfUpdateJSON(cmd *cobra.Command, version string, fixed []string, err error) error {
	result := selfUpdateResult{OK: err == nil, LogPath: selfupdate.LogPath(), Fixed: fixed}
	if err != nil {
		result.FirstLine = selfUpdateFirstLine(err)
	} else {
		result.Version = version
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}

// printSelfUpdateJSONInstalled prints install's result: ok once installed,
// with a failed upload's first line.
func printSelfUpdateJSONInstalled(cmd *cobra.Command, installed string, err error) error {
	result := selfUpdateResult{OK: installed != "", Version: installed, LogPath: selfupdate.LogPath()}
	if err != nil {
		result.FirstLine = selfUpdateFirstLine(err)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}

func selfUpdateFirstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func init() {
	selfUpdateCmd.Flags().String("tag", "", "Release tag to update to (run)")
	selfUpdateCmd.Flags().Bool("json", false, "Print run's or install's result as JSON")
	rootCmd.AddCommand(selfUpdateCmd)
}
