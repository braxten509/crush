package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	fang "charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/exitbanner"
	ui "github.com/charmbracelet/crush/internal/ui/model"
	"github.com/charmbracelet/crush/internal/ui/sessionhost"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var clientHost string

func init() {
	rootCmd.PersistentFlags().StringP("cwd", "c", "", "Current working directory")
	rootCmd.PersistentFlags().StringP("data-dir", "D", "", "Custom crush data directory")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "Debug")
	rootCmd.PersistentFlags().StringVarP(&clientHost, "host", "H", server.DefaultHost(), "Connect to a specific crush server host (for advanced users)")
	rootCmd.Flags().BoolP("help", "h", false, "Help")
	rootCmd.Flags().BoolP("yolo", "y", true, "Automatically accept all permissions (dangerous mode; default on, --yolo=false to disable)")
	rootCmd.PersistentFlags().StringSlice("channels", nil, "MCP servers to enable as channels (repeatable), e.g. --channels server:webhook")
	_ = rootCmd.PersistentFlags().MarkHidden("channels")
	rootCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	rootCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	rootCmd.MarkFlagsMutuallyExclusive("session", "continue")
	rootCmd.Flags().Bool("single", false, "Show one session in this terminal, without the session list")

	rootCmd.AddCommand(
		runCmd,
		dirsCmd,
		projectsCmd,
		saveProjectCmd,
		openProjectCmd,
		updateProvidersCmd,
		logsCmd,
		logoutCmd,
		schemaCmd,
		loginCmd,
		statsCmd,
		sessionCmd,
	)
}

var rootCmd = &cobra.Command{
	Use:   "crush",
	Short: "A terminal-first AI assistant for software development",
	Long:  "A glamorous, terminal-first AI assistant for software development and adjacent tasks",
	Example: `
# Run in interactive mode
crush

# Run non-interactively
crush run "Guess my 5 favorite Pokémon"

# Run a non-interactively with pipes and redirection
cat README.md | crush run "make this more glamorous" > GLAMOROUS_README.md

# Run with debug logging in a specific directory
crush --debug --cwd /path/to/project

# Run in yolo mode (auto-accept all permissions; use with care)
crush --yolo

# Run with custom data directory
crush --data-dir /path/to/custom/.crush

# Continue a previous session
crush --session {session-id}

# Continue the most recent session
crush --continue

# Show one session, without the session list
crush --single
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		if code, guarded := runCrashGuarded(); guarded {
			os.Exit(code)
		}
		crashFile, crashDone := recordCrashes()
		defer crashDone()
		if useSessionHost(cmd) {
			return runSessionHost(cmd)
		}
		sessionID, _ := cmd.Flags().GetString("session")
		continueLast, _ := cmd.Flags().GetBool("continue")

		ws, shutdown, err := setupWorkspaceWithProgressBar(cmd)
		if err != nil {
			return err
		}
		cleanup := sync.OnceFunc(shutdown)
		defer cleanup()

		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		}

		event.AppInitialized()

		com := common.DefaultCommon(ws)
		model := ui.New(com, sessionID, continueLast)

		inputFilter := ui.NewFilter()
		var env uv.Environ = os.Environ()
		program := tea.NewProgram(
			model,
			tea.WithEnvironment(env),
			tea.WithContext(cmd.Context()),
			tea.WithFilter(inputFilter.Filter),
		)
		go ws.Subscribe(program)
		sessionhost.QuitOnHangup(program.Quit)

		showScrollMarker := sync.OnceFunc(hideKonsoleScrollMarker(env, os.Stdout, term.IsTerminal(os.Stdout.Fd())))
		defer showScrollMarker()
		watchCtx, stopWatching := context.WithCancel(cmd.Context())
		go ui.WatchTerminalBackground(watchCtx, model, program.Send)
		restoreStderr := quietStderr(crashFile)
		_, err = program.Run()
		restoreStderr()
		stopWatching()
		showScrollMarker()
		if err != nil {
			event.Error(err)
			slog.Error("TUI run error", "error", err)
			return errors.New("Crush crashed. If metrics are enabled, we were notified about it. If you'd like to report it, please copy the stacktrace above and open an issue at https://github.com/charmbracelet/crush/issues/new?template=bug.yml") //nolint:staticcheck
		}
		if draft := model.CurrentSession(); draft != nil && !ws.AgentIsSessionBusy(draft.ID) {
			if cleaner, ok := ws.(session.EmptySessionWorkspace); ok {
				if err := cleaner.DiscardEmptySession(context.Background(), draft.ID); err != nil {
					slog.Warn("Could not discard empty chat", "error", err)
				}
			}
		}
		if dir := model.RelaunchDir(); dir != "" {
			cleanup()
			args := relaunchArgs(cmd, dir)
			if id, data := model.RelaunchSession(); id != "" {
				args = append(args, "--session", id, "--data-dir", data)
			}
			launchDir := dir
			if _, remote := ws.(*workspace.ClientWorkspace); remote {
				launchDir, _ = os.Getwd()
			}
			return relaunch(launchDir, args)
		}
		var banner config.ExitBanner
		if cfg := com.Config(); cfg != nil {
			banner = cfg.Options.TUI.ExitBanner
		}
		printSessionResume(model, banner, ws)
		return nil
	},
}

var heartbit = lipgloss.NewStyle().Foreground(charmtone.Dolly).SetString(`
    ▄▄▄▄▄▄▄▄    ▄▄▄▄▄▄▄▄
  ███████████  ███████████
████████████████████████████
████████████████████████████
██████████▀██████▀██████████
██████████ ██████ ██████████
▀▀██████▄████▄▄████▄██████▀▀
  ████████████████████████
    ████████████████████
       ▀▀██████████▀▀
           ▀▀▀▀▀▀
`)

// printSessionResume prints the exit banner after the TUI exits, including the
// session title and the hint to resume it with `crush -s <id>`. The banner
// style decides how much of that is shown; see config.ExitBanner.
func printSessionResume(model *ui.UI, banner config.ExitBanner, ws workspace.Workspace) {
	tw, _, _ := term.GetSize(os.Stdout.Fd())
	saved := model.CurrentSession()
	if saved != nil {
		if fresh, err := ws.GetSession(context.Background(), saved.ID); err == nil {
			saved = &fresh
		} else {
			saved = nil
		}
	}
	body := exitbanner.Render(banner, saved, tw)
	if body == "" {
		return
	}
	fmt.Fprintln(colorprofile.NewWriter(os.Stderr, os.Environ()), body)
}

// relaunchArgs keeps session-wide flags but drops project-specific ones
// (session, continue, data dir) when restarting Crush in another project.
func relaunchArgs(cmd *cobra.Command, dir string) []string {
	args := []string{"--cwd", dir}
	for _, name := range []string{"yolo", "debug"} {
		on, _ := cmd.Flags().GetBool(name)
		args = append(args, fmt.Sprintf("--%s=%t", name, on))
	}
	if cmd.Flags().Changed("host") {
		args = append(args, "--host", clientHost)
	}
	return args
}

// copied from cobra:
const defaultVersionTemplate = `{{with .DisplayName}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
`

func Execute() {
	// FIXME: config.Load uses slog internally during provider resolution,
	// but the file-based logger isn't set up until after config is loaded
	// (because the log path depends on the data directory from config).
	// This creates a window where slog calls in config.Load leak to
	// stderr. We discard early logs here as a workaround. The proper
	// fix is to remove slog calls from config.Load and have it return
	// warnings/diagnostics instead of logging them as a side effect.
	slog.SetDefault(slog.New(slog.DiscardHandler))

	// NOTE: very hacky: we create a colorprofile writer with STDOUT, then make
	// it forward to a bytes.Buffer, write the colored heartbit to it, and then
	// finally prepend it in the version template.
	// Unfortunately cobra doesn't give us a way to set a function to handle
	// printing the version, and PreRunE runs after the version is already
	// handled, so that doesn't work either.
	// This is the only way I could find that works relatively well.
	if term.IsTerminal(os.Stdout.Fd()) {
		var b bytes.Buffer
		w := colorprofile.NewWriter(os.Stdout, os.Environ())
		w.Forward = &b
		_, _ = w.WriteString(heartbit.String())
		rootCmd.SetVersionTemplate(b.String() + "\n" + defaultVersionTemplate)
	}
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}

func MaybePrependStdin(prompt string) (string, error) {
	if term.IsTerminal(os.Stdin.Fd()) {
		return prompt, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return prompt, err
	}
	// Check if stdin is a named pipe ( | ) or regular file ( < ).
	if fi.Mode()&os.ModeNamedPipe == 0 && !fi.Mode().IsRegular() {
		return prompt, nil
	}
	bts, err := io.ReadAll(os.Stdin)
	if err != nil {
		return prompt, err
	}
	return string(bts) + "\n\n" + prompt, nil
}

// resolveWorkspaceSessionID resolves a session ID that may be a full
// UUID, full hash, or hash prefix. Works against the Workspace
// interface so both local and client/server paths get hash prefix
// support.
func resolveWorkspaceSessionID(ctx context.Context, ws workspace.Workspace, id string) (session.Session, error) {
	if sess, err := ws.GetSession(ctx, id); err == nil {
		return sess, nil
	}

	sessions, err := ws.ListSessions(ctx)
	if err != nil {
		return session.Session{}, err
	}

	matches := sessionsMatchingHash(sessions, id, func(s session.Session) string { return s.ID })

	switch len(matches) {
	case 0:
		return session.Session{}, fmt.Errorf("session not found: %s", id)
	case 1:
		return matches[0], nil
	default:
		return session.Session{}, fmt.Errorf("session ID %q is ambiguous (%d matches)", id, len(matches))
	}
}

func ResolveCwd(cmd *cobra.Command) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		err := os.Chdir(cwd)
		if err != nil {
			return "", fmt.Errorf("failed to change directory: %v", err)
		}
		return cwd, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %v", err)
	}
	return cwd, nil
}
