package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// remotePasswordFile holds the launcher password's bcrypt hash.
func remotePasswordFile() string {
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), "remote-password")
}

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Let the Pocket Agents phone app open new Crush windows here",
}

var remotePasswordCmd = &cobra.Command{
	Use:   "password",
	Short: "Set the password the phone needs to open a new Crush window",
	Long: `Set the password the Pocket Agents app asks for before it opens a new Crush
window on this computer (in your home folder, shared with the phone). Only a
bcrypt hash is stored. Type it here; it is never passed as an argument.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return errors.New("run this in a terminal, so the password can be typed without showing")
		}
		read := func(prompt string) (string, error) {
			fmt.Fprint(cmd.OutOrStdout(), prompt)
			b, err := term.ReadPassword(fd)
			fmt.Fprintln(cmd.OutOrStdout())
			return string(b), err
		}
		first, err := read("New password: ")
		if err != nil {
			return err
		}
		second, err := read("Type it again: ")
		if err != nil {
			return err
		}
		if first != second {
			return errors.New("the two passwords don't match; nothing changed")
		}
		if err := remote.SetPassword(remotePasswordFile(), first); err != nil {
			return err
		}
		cmd.Println("Password set. The phone asks for it before it opens a new Crush window here.")
		return nil
	},
}

var remoteLauncherCmd = &cobra.Command{
	Use:   "launcher",
	Short: "Serve the phone's requests to open a new Crush window (run as a service)",
	Long: `Listen on this computer's Tailscale address for the Pocket Agents app. With the
password from "crush remote password", the phone opens a new Konsole window
running Crush in your home folder, shared with the phone. Only your own
Tailscale devices are answered. Set CRUSH_LAUNCH_COMMAND to replace the
Konsole command (a shell command line; $CRUSH_EXE is this Crush).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// The crush on PATH, so a self-update (which replaces that file) is
		// what the next window runs.
		exe, err := exec.LookPath("crush")
		if err != nil {
			if exe, err = os.Executable(); err != nil {
				return err
			}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return remote.RunLauncher(ctx, remotePasswordFile(), remote.DefaultLaunchCommand(exe, home))
	},
}

func init() {
	remoteCmd.AddCommand(remotePasswordCmd, remoteLauncherCmd)
	rootCmd.AddCommand(remoteCmd)
}
