package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/event"
	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
	xstrings "github.com/charmbracelet/x/exp/strings"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// supportsProgressBar tries to determine whether the current terminal supports
// progress bars by looking into environment variables.
func supportsProgressBar() bool {
	if !term.IsTerminal(os.Stderr.Fd()) {
		return false
	}
	termProg := os.Getenv("TERM_PROGRAM")
	_, isWindowsTerminal := os.LookupEnv("WT_SESSION")

	return isWindowsTerminal || xstrings.ContainsAnyOf(strings.ToLower(termProg), "ghostty", "iterm2", "rio")
}

// useClientServer returns true when the client/server architecture is
// enabled via the CRUSH_CLIENT_SERVER environment variable.
func useClientServer() bool {
	v, _ := strconv.ParseBool(os.Getenv("CRUSH_CLIENT_SERVER"))
	return v
}

// setupWorkspaceWithProgressBar wraps setupWorkspace with an optional
// terminal progress bar shown during initialization.
func setupWorkspaceWithProgressBar(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	showProgress := supportsProgressBar()
	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
	}

	ws, cleanup, err := setupWorkspace(cmd)

	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar)
	}

	return ws, cleanup, err
}

// setupWorkspace returns a Workspace and cleanup function. When
// CRUSH_CLIENT_SERVER=1, it connects to a server process and returns a
// ClientWorkspace. Otherwise it creates an in-process app.App and
// returns an AppWorkspace.
func setupWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	if useClientServer() {
		return setupClientServerWorkspace(cmd)
	}
	return setupLocalWorkspace(cmd)
}

// setupLocalWorkspace creates an in-process app.App and wraps it in an
// AppWorkspace.
func setupLocalWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	appInstance, cleanup, err := setupLocalApp(cmd)
	if err != nil {
		return nil, nil, err
	}
	return workspace.NewAppWorkspace(appInstance, appInstance.Store()), cleanup, nil
}

// setupLocalApp loads the config for the working directory and creates an
// in-process app.App. The cleanup function shuts the app down.
func setupLocalApp(cmd *cobra.Command) (*app.App, func(), error) {
	debug, _ := cmd.Flags().GetBool("debug")
	yolo, _ := cmd.Flags().GetBool("yolo")
	channels, _ := cmd.Flags().GetStringSlice("channels")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, err
	}

	store, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, nil, err
	}

	cfg := store.Config()
	cpuaffinity.ApplyOptions(cfg.Options)
	store.Overrides().SkipPermissionRequests = yolo
	store.Overrides().EnabledChannels = channels

	if err := os.MkdirAll(cfg.Options.DataDirectory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create data directory: %q %w", cfg.Options.DataDirectory, err)
	}

	gitIgnorePath := filepath.Join(cfg.Options.DataDirectory, ".gitignore")
	if _, err := os.Stat(gitIgnorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitIgnorePath, []byte("*\n"), 0o644); err != nil {
			return nil, nil, fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}

	if err := projects.Register(cwd, cfg.Options.DataDirectory); err != nil {
		slog.Warn("Failed to register project", "error", err)
	}

	conn, err := db.Connect(ctx, cfg.Options.DataDirectory)
	if err != nil {
		return nil, nil, err
	}
	db.PruneIdleReviewsInBackground(ctx, conn)

	logFile := filepath.Join(cfg.Options.DataDirectory, "logs", "crush.log")
	crushlog.Setup(logFile, debug)

	// Discover skills once before app.New. Local mode hosts a single
	// workspace per process, so WithGlobalMirror keeps the package
	// globals (which the TUI reads via skills.GetLatestStates) in sync
	// with the manager.
	discoveryCfg := localSkillsDiscoveryConfig(store)
	allSkills, activeSkills, skillStates := skills.DiscoverFromConfig(discoveryCfg)
	skillsMgr := skills.NewManager(
		allSkills, activeSkills, skillStates,
		skills.WithGlobalMirror(),
		skills.WithResolvedPaths(discoveryCfg.ResolvePaths()),
		skills.WithWorkingDir(discoveryCfg.WorkingDir),
	)

	appInstance, err := app.New(ctx, conn, store, skillsMgr)
	if err != nil {
		_ = conn.Close()
		slog.Error("Failed to create app instance", "error", err)
		return nil, nil, err
	}

	if shouldEnableMetrics(cfg) {
		event.Init()
	}

	cleanup := func() { appInstance.Shutdown() }
	return appInstance, cleanup, nil
}

// localSkillsDiscoveryConfig adapts a *config.ConfigStore to the inputs
// skills.DiscoverFromConfig expects.
func localSkillsDiscoveryConfig(store *config.ConfigStore) skills.DiscoveryConfig {
	opts := store.Config().Options
	var paths, disabled []string
	if opts != nil {
		paths = opts.SkillsPaths
		disabled = opts.DisabledSkills
	}
	var resolver func(string) (string, error)
	if r := store.Resolver(); r != nil {
		resolver = r.ResolveValue
	}
	return skills.DiscoveryConfig{
		SkillsPaths:    paths,
		DisabledSkills: disabled,
		WorkingDir:     store.WorkingDir(),
		Resolver:       resolver,
	}
}

// setupClientServerWorkspace connects to a server process and wraps the
// result in a ClientWorkspace.
func setupClientServerWorkspace(cmd *cobra.Command) (workspace.Workspace, func(), error) {
	conn, err := connectToServerOrEngine(cmd)
	if err != nil {
		return nil, nil, err
	}
	protoWs := conn.ws

	clientWs := workspace.NewClientWorkspace(conn.client, *protoWs)
	if conn.engine != nil {
		// When the engine stops answering, find or start it again before
		// re-creating the workspace (engine-design.md 5.14).
		opts := *conn.engine
		clientWs.SetEnsureEngine(func(ctx context.Context) error {
			_, err := serverctl.EnsureEngine(ctx, opts)
			return err
		})
	}

	if protoWs.Config.IsConfigured() {
		if err := clientWs.InitCoderAgent(cmd.Context()); err != nil {
			slog.Error("Failed to initialize coder agent", "error", err)
		}
	}

	// Clean up via Shutdown rather than connectToServer's closure: it stops
	// the subscription's reconnect/recovery loop first, so our own exit
	// cannot be mistaken for a lost workspace and re-created mid-quit.
	return clientWs, clientWs.Shutdown, nil
}

// connectToServer ensures the server is running, creates a client and
// workspace, and returns a cleanup function that deletes the workspace.
func connectToServer(cmd *cobra.Command) (*client.Client, *proto.Workspace, func(), error) {
	conn, err := connectToServerOrEngine(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	return conn.client, conn.ws, conn.cleanup, nil
}

// serverConnection is a workspace connectToServerOrEngine created.
type serverConnection struct {
	client  *client.Client
	ws      *proto.Workspace
	cleanup func()
	// engine is set when the workspace is on the project's engine: the
	// options that find or start that engine again.
	engine *serverctl.EnsureOptions
}

// connectToServerOrEngine picks where the workspace lives
// (engine-design.md 5.2), creates it there, and returns a cleanup that
// leaves it:
//
//  1. With CRUSH_CLIENT_SERVER=1 and no --host: the project's engine,
//     started if needed.
//  2. With the switch off and no --host (only `crush logout` gets here):
//     the project's live engine, when FindEngine finds one. It never
//     starts one.
//  3. Otherwise the legacy server at --host, as before.
func connectToServerOrEngine(cmd *cobra.Command) (*serverConnection, error) {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, err
	}
	if cmd.Flags().Changed("host") {
		return connectToLegacyServer(cmd, cwd)
	}

	// ResolveCwd changed into the folder, so this is its absolute form:
	// the engine resolves the project from it.
	absCwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %v", err)
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	opts := serverctl.EnsureOptions{Cwd: absCwd, DataDir: dataDir, Debug: debug}

	if useClientServer() {
		h, err := serverctl.EnsureEngine(cmd.Context(), opts)
		if err != nil {
			return nil, err
		}
		return connectToEngine(cmd, opts, h)
	}
	h, ok, err := serverctl.FindEngine(cmd.Context(), opts)
	if err != nil {
		slog.Warn("Failed to look for the project's engine; using the server", "error", err)
	}
	if ok {
		return connectToEngine(cmd, opts, h)
	}
	return connectToLegacyServer(cmd, cwd)
}

// connectToEngine creates the workspace on the project's engine h. A
// create that meets the engine's exit runs ensure again and retries.
func connectToEngine(cmd *cobra.Command, opts serverctl.EnsureOptions, h serverctl.EngineHandle) (*serverConnection, error) {
	c, err := client.NewClient(opts.Cwd, "unix", h.Socket)
	if err != nil {
		return nil, err
	}
	conn, err := createClientWorkspace(cmd, c, opts.Cwd, func() error {
		_, err := serverctl.EnsureEngine(cmd.Context(), opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	conn.engine = &opts
	return conn, nil
}

// connectToLegacyServer creates the workspace on the server at --host,
// starting or replacing that server as needed.
func connectToLegacyServer(cmd *cobra.Command, cwd string) (*serverConnection, error) {
	if clientHost == "" {
		return nil, errNoDefaultHost()
	}
	hostURL, err := server.ParseHostURL(clientHost)
	if err != nil {
		return nil, fmt.Errorf("invalid host URL: %v", err)
	}

	debug, _ := cmd.Flags().GetBool("debug")

	if err := serverctl.Ensure(cmd.Context(), hostURL, clientHost, debug); err != nil {
		return nil, err
	}

	c, err := client.NewClient(cwd, hostURL.Scheme, hostURL.Host)
	if err != nil {
		return nil, err
	}
	return createClientWorkspace(cmd, c, cwd, func() error {
		return serverctl.ReplaceExiting(cmd.Context(), hostURL, clientHost)
	})
}

// createClientWorkspace creates the workspace for cwd through c. replace
// brings up a new server when the one c reached is exiting.
func createClientWorkspace(cmd *cobra.Command, c *client.Client, cwd string, replace func() error) (*serverConnection, error) {
	debug, _ := cmd.Flags().GetBool("debug")
	yolo, _ := cmd.Flags().GetBool("yolo")
	channels, _ := cmd.Flags().GetStringSlice("channels")
	dataDir, _ := cmd.Flags().GetString("data-dir")

	wsReq := proto.Workspace{
		Path:     cwd,
		DataDir:  dataDir,
		Debug:    debug,
		YOLO:     yolo,
		Channels: channels,
		Version:  version.Version,
		Protocol: proto.ProtocolVersion,
		Env:      os.Environ(),
	}

	ws, err := serverctl.CreateWorkspace(cmd.Context(), c, wsReq, replace)
	if err != nil {
		return nil, err
	}

	if shouldEnableMetrics(ws.Config) {
		event.Init()
	}

	if ws.Config != nil {
		logFile := filepath.Join(ws.Config.Options.DataDirectory, "logs", "crush.log")
		crushlog.Setup(logFile, debug)
	}

	// Retiring the client releases every claim it holds, so it covers
	// workspaces this process created but never learned the ID of.
	cleanup := func() {
		if err := c.RetireClient(context.Background()); err != nil {
			_ = c.DeleteWorkspace(context.Background(), ws.ID)
		}
	}
	return &serverConnection{client: c, ws: ws, cleanup: cleanup}, nil
}

func shouldEnableMetrics(cfg *config.Config) bool {
	if v, _ := strconv.ParseBool(os.Getenv("CRUSH_DISABLE_METRICS")); v {
		return false
	}
	if v, _ := strconv.ParseBool(os.Getenv("DO_NOT_TRACK")); v {
		return false
	}
	if cfg.Options.DisableMetrics {
		return false
	}
	return true
}
