package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/spf13/cobra"
)

// joinMemoryBox joins the memory box shared by every Crush process. A var
// so tests can see that the server and the engine call it.
var joinMemoryBox = memlimit.Join

// Exit codes of `crush engine ensure` and `crush engine serve`
// (engine-design.md 5.2).
const (
	engineExitOther            = 1
	engineExitProtocolMismatch = 3
	engineExitProjectLocked    = 4
	engineExitStartFailed      = 5
	engineExitUnsafeSocketDir  = 6
)

const (
	// engineLockWait is how long a new engine waits for an exiting engine
	// to let go of the project (5.10).
	engineLockWait = 5 * time.Second
	// engineListProbeTimeout bounds the question to each engine in
	// `crush engine list`.
	engineListProbeTimeout = time.Second
)

func init() {
	engineEnsureCmd.Flags().Duration("timeout", 15*time.Second, "How long to wait for the engine")
	engineCmd.AddCommand(engineVersionCmd, engineEnsureCmd, engineListCmd, engineServeCmd)
	rootCmd.AddCommand(engineCmd)
}

var engineCmd = &cobra.Command{
	Use:   "engine",
	Short: "Find, start or list the Crush engine of a project",
	Long: "The Crush engine runs a project's chats for the desktop app and for shared terminal windows. " +
		"Each command prints one JSON value on stdout.",
}

var engineVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print this crush's version and protocol as JSON",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return writeEngineJSON(cmd.OutOrStdout(), struct {
			Version  string `json:"version"`
			Protocol int    `json:"protocol"`
		}{version.Version, proto.ProtocolVersion})
	},
}

var engineEnsureCmd = &cobra.Command{
	Use:   "ensure",
	Short: "Find the project's engine, or start it, and print how to reach it as JSON",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		opts, err := engineOptions(cmd)
		var h serverctl.EngineHandle
		if err == nil {
			opts.Timeout, _ = cmd.Flags().GetDuration("timeout")
			h, err = serverctl.EnsureEngine(cmd.Context(), opts)
		}
		out, code := engineEnsureOutput(h, err)
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "crush engine ensure:", err)
		}
		_ = writeEngineJSON(cmd.OutOrStdout(), out)
		if code != 0 {
			os.Exit(code)
		}
	},
}

var engineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List this user's live engines as JSON",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		engines, err := liveEngines(cmd.Context())
		if err != nil {
			// As for ensure: no safe socket folder is unsafe_socket_dir,
			// exit 6. [] only ever means that no engine runs.
			fmt.Fprintln(cmd.ErrOrStderr(), "crush engine list:", err)
			out, code := engineFailureOutput(err)
			_ = writeEngineJSON(cmd.OutOrStdout(), out)
			os.Exit(code)
		}
		_ = writeEngineJSON(cmd.OutOrStdout(), engines)
	},
}

var engineServeCmd = &cobra.Command{
	Use:    "serve",
	Short:  "Run the engine of the project whose data folder --data-dir names",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runEngineServe,
}

// engineEnsureSuccess is the success output of `crush engine ensure`.
type engineEnsureSuccess struct {
	Address  string `json:"address"`
	Socket   string `json:"socket"`
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	PID      int    `json:"pid"`
	DataDir  string `json:"data_dir"`
	Started  bool   `json:"started"`
}

// engineEnsureFailure is the error output of `crush engine ensure` and
// `crush engine list`.
type engineEnsureFailure struct {
	Error engineError `json:"error"`
}

type engineError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// engineEnsureOutput is what `crush engine ensure` prints for an ensure's
// result, and its exit code.
func engineEnsureOutput(h serverctl.EngineHandle, err error) (any, int) {
	if err == nil {
		return engineEnsureSuccess{
			Address: h.Address, Socket: h.Socket, Protocol: h.Protocol,
			Version: h.Version, PID: h.PID, DataDir: h.DataDir, Started: h.Started,
		}, 0
	}
	return engineFailureOutput(err)
}

// engineFailureOutput is what an engine command prints for err, and its
// exit code.
func engineFailureOutput(err error) (engineEnsureFailure, int) {
	code, exit := "internal", engineExitOther
	switch {
	case errors.Is(err, backend.ErrProtocolMismatch):
		code, exit = "protocol_mismatch", engineExitProtocolMismatch
	case errors.Is(err, serverctl.ErrProjectLocked):
		code, exit = "project_locked", engineExitProjectLocked
	case errors.Is(err, serverctl.ErrEngineStart):
		code, exit = "start_failed", engineExitStartFailed
	case errors.Is(err, server.ErrNoPrivateSocketDir):
		code, exit = "unsafe_socket_dir", engineExitUnsafeSocketDir
	case errors.Is(err, backend.ErrWrongProject):
		code = "wrong_project"
	}
	return engineEnsureFailure{Error: engineError{Code: code, Message: err.Error()}}, exit
}

// writeEngineJSON prints v as one line of JSON.
func writeEngineJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// engineOptions reads the project the engine commands are about from the
// root flags, without changing the process's folder.
func engineOptions(cmd *cobra.Command) (serverctl.EnsureOptions, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd == "" {
		var err error
		if cwd, err = os.Getwd(); err != nil {
			return serverctl.EnsureOptions{}, fmt.Errorf("failed to get the current folder: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return serverctl.EnsureOptions{}, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	return serverctl.EnsureOptions{Cwd: cwd, DataDir: dataDir, Debug: debug}, nil
}

// liveEngines asks every engine socket in the private socket folder what
// it is. Sockets nobody answers on are skipped and left alone.
func liveEngines(ctx context.Context) ([]proto.EngineInfo, error) {
	engines := []proto.EngineInfo{}
	dir, err := server.PrivateSocketDir()
	if err != nil {
		return engines, err
	}
	sockets, err := filepath.Glob(filepath.Join(dir, "engine-*.sock"))
	if err != nil {
		return engines, err
	}
	slices.Sort(sockets)
	for _, socket := range sockets {
		c, err := client.NewClient("", "unix", socket)
		if err != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, engineListProbeTimeout)
		info, err := c.EngineInfo(probeCtx)
		cancel()
		if err != nil {
			slog.Debug("Skipped an engine socket nobody answers on", "socket", socket, "error", err)
			continue
		}
		engines = append(engines, info)
	}
	return engines, nil
}

// runEngineServe is `crush engine serve`: it serves one project until the
// engine decides to exit or is stopped.
//
//  1. It joins the memory box.
//  2. It makes the data folder canonical and finds its socket.
//  3. It takes the project lock, waiting up to 5 s for an exiting engine.
//     When another engine or a lone window holds the project, it writes
//     start-error.json with code project_locked and exits 4.
//  4. It loads the config as `crush server` does.
//  5. It creates the server, makes its backend the project's engine, and
//     listens on the project's engine socket.
//  6. On a stop it shuts down within 5 s. The lock is held until the end.
//
// Every other failure exits 1; its message goes to stderr, which is the
// engine.log an ensure shows the end of.
func runEngineServe(cmd *cobra.Command, _ []string) error {
	_ = joinMemoryBox(memlimit.Slice)
	_, crashDone := recordCrashes()
	defer crashDone()

	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	if dataDir == "" {
		return errors.New("--data-dir is required")
	}
	canonical, err := config.CanonicalDataDir(dataDir)
	if err != nil {
		return fmt.Errorf("failed to resolve the data folder %q: %w", dataDir, err)
	}
	socket, err := server.EngineSocketPath(canonical)
	if err != nil {
		return err
	}
	crushlog.Setup(serverctl.EngineLogFile(socket), debug)

	if err := os.MkdirAll(canonical, 0o700); err != nil {
		return fmt.Errorf("failed to create the data folder: %w", err)
	}
	release, err := db.LockProject(cmd.Context(), canonical, db.ProjectLockEngine, engineLockWait)
	if errors.Is(err, db.ErrDataDirLocked) {
		if werr := serverctl.WriteStartError(socket, serverctl.StartError{Code: "project_locked", Message: err.Error()}); werr != nil {
			slog.Error("Failed to record why the engine did not start", "error", werr)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		os.Exit(engineExitProjectLocked)
	}
	if err != nil {
		return fmt.Errorf("failed to take the project lock: %w", err)
	}
	defer func() { _ = release() }()

	cfg, err := config.Load(config.GlobalWorkspaceDir(), canonical, debug)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	srv := server.NewServer(cfg, "unix", socket)
	srv.Backend().SetEngine(canonical)
	srv.SetLogger(slog.Default())
	slog.Info("Starting the Crush engine...", "socket", socket, "data_dir", canonical)

	errch := make(chan error, 1)
	sigch := make(chan os.Signal, 1)
	sigs := []os.Signal{os.Interrupt}
	sigs = append(sigs, addSignals(sigs)...)
	signal.Notify(sigch, sigs...)
	defer signal.Stop(sigch)

	go func() {
		errch <- srv.ListenAndServe()
	}()

	select {
	case <-sigch:
		slog.Info("Received interrupt signal...")
	case err = <-errch:
		if errors.Is(err, server.ErrServerClosed) {
			// The engine decided to exit: its last workspace went and the
			// idle delay passed, or an ensure replaced it.
			return nil
		}
		_ = srv.Close()
		slog.Error("Engine error", "error", err)
		return fmt.Errorf("engine error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	slog.Info("Shutting down...")
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Failed to shut down the engine", "error", err)
		return fmt.Errorf("failed to shut down the engine: %w", err)
	}
	return nil
}
