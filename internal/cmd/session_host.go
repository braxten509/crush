package cmd

import (
	"os"
	"runtime"

	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/sessionhost"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// useSessionHost reports whether this Crush shows the session list, with
// each session a Crush of its own. It doesn't when asked for one session,
// when it already is a session in the list, when the phone's launcher
// opened it (that window shares one chat), or off a terminal.
func useSessionHost(cmd *cobra.Command) bool {
	single, _ := cmd.Flags().GetBool("single")
	return !single &&
		runtime.GOOS != "windows" &&
		!sessionhost.InHost() &&
		os.Getenv(remote.LaunchEnv) == "" &&
		term.IsTerminal(os.Stdin.Fd()) &&
		term.IsTerminal(os.Stdout.Fd())
}

func runSessionHost(cmd *cobra.Command) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return err
	}
	return sessionhost.Run(cmd.Context(), sessionhost.Options{
		Exe:       exe,
		FirstArgs: os.Args[1:],
		NewArgs:   func(dir string) []string { return relaunchArgs(cmd, dir) },
		Dir:       cwd,
		// Each session gets its own crash guard and report.
		Env:      sessionhost.ChildEnviron(os.Environ(), crashReportEnv),
		Shell:    os.Getenv("SHELL"),
		ShellEnv: sessionhost.TerminalEnviron(os.Environ(), crashReportEnv),
		Recent:   recentFolders,
	})
}

// recentFolders lists the folders Crush was used in, the latest first.
func recentFolders() []string {
	list, err := projects.List()
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(list))
	for _, p := range list {
		dirs = append(dirs, p.Path)
	}
	return dirs
}
