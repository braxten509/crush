// Package app wires together services, coordinates agents, and manages
// application lifecycle.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/clipboard"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/herdr"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/charmbracelet/crush/internal/lockedchats"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/openchats"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pinentry"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/prompthistory"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/sshaskpass"
	"github.com/charmbracelet/crush/internal/update"
	"github.com/charmbracelet/crush/internal/version"
)

// UpdateAvailableMsg is sent when a new version is available.
type UpdateAvailableMsg struct {
	CurrentVersion string
	LatestVersion  string
	IsDevelopment  bool
}

type App struct {
	Sessions    session.Service
	Messages    message.Service
	History     history.Service
	Permissions permission.Service
	Questions   question.Service
	FileTracker filetracker.Service
	Pinentry    *pinentry.Service

	AgentCoordinator agent.Coordinator

	// LockedChats locks chats into the Plasma Vault and back (owner,
	// 2026-11-03).
	LockedChats *lockedchats.Service

	LSPManager *lsp.Manager

	Skills *skills.Manager

	config *config.ConfigStore

	serviceEventsWG *sync.WaitGroup
	eventsCtx       context.Context
	events          *pubsub.Broker[tea.Msg]
	tuiWG           *sync.WaitGroup

	// global context and cleanup functions
	globalCtx          context.Context
	cleanupFuncs       []func(context.Context) error
	agentNotifications *pubsub.Broker[notify.Notification]
	// runCompletions is the authoritative per-run completion signal,
	// emitted once per top-level agent turn after all message
	// updates have been flushed. Bridged into app.events so SSE
	// subscribers (notably `crush run` in client/server mode) can
	// drive their exit on a deterministic, payload-bearing event
	// instead of guessing from message finish parts.
	runCompletions *pubsub.Broker[notify.RunComplete]
	tasks          *pubsub.Broker[agent.Task]

	// herdrClient reports agent state to herdr when running inside
	// a herdr-managed pane. Nil when not in a herdr environment.
	herdrClient *herdr.Client

	// hookRunner runs user hooks for every event; it reads the config
	// store on each event so reloads need no extra wiring.
	hookRunner *hooks.Runner

	// currentSessionID is the session the user is working in, reported
	// by the TUI and the non-interactive runner, for SessionEnd.
	currentSessionID atomic.Value
}

// New initializes a new application instance. skillsMgr carries the
// per-workspace skill discovery results computed by the caller; the
// caller is responsible for constructing it (typically via
// skills.NewManager + skills.DiscoverFromConfig).
func New(ctx context.Context, conn *sql.DB, store *config.ConfigStore, skillsMgr *skills.Manager) (*App, error) {
	q := db.New(conn)
	versions := filehistory.New(conn, store.Config().Options.DataDirectory, store.WorkingDir())
	versions.ScheduleMaintenance(ctx)
	go maintainFileHistory(ctx, versions)
	sessions := session.NewService(q, conn, versions.Maintain)
	messages := message.NewService(q, message.WithFileHistory(versions))
	files := history.NewService(q, conn)
	cfg := store.Config()
	var allowedTools []string
	if cfg.Permissions != nil && cfg.Permissions.AllowedTools != nil {
		allowedTools = cfg.Permissions.AllowedTools
	}

	hookRunner := hooks.NewRunner(func() map[string][]config.HookConfig {
		return store.Config().Hooks
	}, store.WorkingDir(), store.WorkingDir())

	permSvc := permission.NewPermissionService(store.WorkingDir(), allowedTools, permissionHooks{hookRunner})
	if store.Overrides().PermissionMode != permission.PermissionModeNormal {
		permSvc.SetPermissionMode(store.Overrides().PermissionMode)
	}

	app := &App{
		Sessions:    sessions,
		Messages:    messages,
		History:     files,
		Permissions: permSvc,
		Questions:   question.NewService(),
		FileTracker: filetracker.NewService(q),
		Pinentry:    pinentry.DefaultService(),
		LSPManager:  lsp.NewManager(store),
		Skills:      skillsMgr,

		globalCtx:  ctx,
		hookRunner: hookRunner,

		config: store,

		events:             pubsub.NewCoalescingBroker(eventCoalesceKey),
		serviceEventsWG:    &sync.WaitGroup{},
		tuiWG:              &sync.WaitGroup{},
		agentNotifications: pubsub.NewBroker[notify.Notification](),
		runCompletions:     pubsub.NewBroker[notify.RunComplete](),
		tasks:              pubsub.NewBroker[agent.Task](),
	}

	app.setupEvents()
	app.wireChatModels(ctx)
	app.LockedChats = app.newLockedChats(conn, store)
	if err := app.LockedChats.Start(ctx); err != nil {
		return nil, fmt.Errorf("cannot secure unlocked chats before startup: %w", err)
	}

	// Start the pinentry watcher so terminal-based GPG passphrase prompts
	// (pinentry-curses/pinentry-tty) get exclusive terminal access while a
	// shell command runs. It only polls while commands are executing, and
	// stops with the events pipeline.
	app.Pinentry.Start(app.eventsCtx)

	// Enable the integrated pinentry: GPG passphrase/PIN prompts are
	// rendered as native dialogs (loopback pinentry) for Crush-spawned
	// processes without touching GPG or git configuration. The
	// terminal-handover watcher above remains the fallback.
	if cfg.Options.GetPinentryIntegrated() {
		if executable, err := os.Executable(); err != nil {
			slog.Warn("Integrated pinentry unavailable", "error", err)
		} else if cleanup, err := pinentry.StartIntegration(ctx, executable, cfg.Options.GetPinentryCacheTimeout()); err != nil {
			slog.Warn("Integrated pinentry unavailable", "error", err)
		} else {
			app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
				cleanup()
				return nil
			})
		}
	}

	// Enable the integrated ssh askpass: OpenSSH credential and
	// confirmation prompts (passwords, key passphrases, security key
	// touches) for Crush-spawned commands are rendered as native
	// dialogs. The redirect is environment-scoped; no ssh configuration
	// or files are modified.
	if executable, err := os.Executable(); err != nil {
		slog.Warn("Integrated ssh prompts unavailable", "error", err)
	} else if cleanup, err := sshaskpass.StartIntegration(ctx, executable); err != nil {
		slog.Warn("Integrated ssh prompts unavailable", "error", err)
	} else {
		app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
			cleanup()
			return nil
		})
	}

	// Initialize clipboard support. This is best-effort; if it fails
	// (e.g., headless environment), clipboard operations will return nil.
	if err := clipboard.Init(); err != nil {
		slog.Warn("Clipboard initialization failed", "error", err)
	}

	// Check for updates in the background.
	go app.checkForUpdates(ctx)

	app.startMCP(ctx, store)

	// Start herdr integration when running inside a herdr pane.
	app.herdrClient = herdr.Init()
	herdr.BridgeLocal(ctx, app.herdrClient, herdr.BridgeSources{
		PermRequests:      app.Permissions,
		PermNotifications: app.Permissions,
		RunCompletions:    app.runCompletions,
		Messages:          app.Messages,
	})

	// Release the shared database connection on shutdown. The pool
	// closes the underlying *sql.DB when the last reference is released.
	dataDir := cfg.Options.DataDirectory
	app.cleanupFuncs = append(
		app.cleanupFuncs,
		func(context.Context) error { return db.Release(dataDir) },
		func(ctx context.Context) error { return mcp.Close(ctx) },
	)

	// TODO: remove the concept of agent config, most likely.
	if !cfg.IsConfigured() {
		slog.Warn("No agent configuration found")
		return app, nil
	}
	if err := app.InitCoderAgent(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize coder agent: %w", err)
	}
	go app.refreshDiscoveredModels(ctx, app.AgentCoordinator)

	app.watchLSP(ctx)

	return app, nil
}

// maintainFileHistory reschedules file-history maintenance every minute
// until ctx is done.
func maintainFileHistory(ctx context.Context, versions *filehistory.Store) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			versions.ScheduleMaintenance(ctx)
		}
	}
}

// startMCP starts MCP initialization in the background, honoring the
// repository's Toggle MCPs overrides.
func (app *App) startMCP(ctx context.Context, store *config.ConfigStore) {
	// Arm initialization synchronously before launching it so WaitForInit
	// blocks for the in-flight init instead of racing the goroutine and
	// returning before any MCP tools register.
	mcp.ArmInit()
	// Config-disabled servers the user enabled via Toggle MCPs have a
	// repository-scoped enabled override; force-start them so the toggle
	// survives restarts. A read failure only means the override is skipped.
	var forceStart []string
	if enabled, err := app.Sessions.MCPServersEnabled(ctx); err != nil {
		slog.Warn("Failed to list enabled MCP overrides; config-disabled servers stay disabled", "error", err)
	} else {
		for _, name := range enabled {
			if m, ok := store.Config().MCP[name]; ok && m.Disabled {
				forceStart = append(forceStart, name)
			}
		}
	}
	// Servers disabled for this repository via Toggle MCPs are not started.
	localDisabled, err := app.Sessions.MCPDisabledServers(ctx)
	if err != nil {
		slog.Warn("Failed to list disabled MCP overrides; starting them anyway", "error", err)
	}
	go mcp.Initialize(ctx, app.Permissions, store, forceStart, localDisabled)
}

// watchLSP reports LSP state changes as events and announces the
// configured servers.
func (app *App) watchLSP(ctx context.Context) {
	// Set up callback for LSP state updates.
	app.LSPManager.SetCallback(func(name string, client *lsp.Client) {
		if client == nil {
			updateLSPState(name, lsp.StateUnstarted, nil, nil, 0)
			return
		}
		client.SetDiagnosticsCallback(updateLSPDiagnostics)
		updateLSPState(name, client.GetServerState(), nil, client, 0)
	})

	// TrackConfigured must run after SetCallback so the callback is already
	// installed when configured-but-not-yet-started LSPs are announced.
	go app.LSPManager.TrackConfigured(ctx)
}

// refreshDiscoveredModels updates custom provider models that startup took
// from the discovery cache. The coordinator is passed in rather than read
// from app so this goroutine never races a later InitCoderAgent.
func (app *App) refreshDiscoveredModels(ctx context.Context, coordinator agent.Coordinator) {
	defer log.RecoverPanic("app.refreshDiscoveredModels", nil)
	reloaded, err := app.config.RefreshDiscoveredModels(ctx)
	if err != nil {
		slog.Warn("Failed to apply refreshed provider models", "error", err)
	}
	if !reloaded {
		return
	}
	if err := coordinator.UpdateModels(ctx); err != nil {
		slog.Warn("Failed to update agent models after discovery", "error", err)
	}
}

// Config returns the pure-data configuration.
func (app *App) Config() *config.Config {
	return app.config.Config()
}

// Store returns the config store.
func (app *App) Store() *config.ConfigStore {
	return app.config
}

// Events returns a per-caller subscription channel for application events.
// Each caller receives its own channel; all callers receive every event.
func (app *App) Events(ctx context.Context) <-chan pubsub.Event[tea.Msg] {
	return app.events.Subscribe(ctx)
}

// SendEvent publishes a message to all event subscribers.
func (app *App) SendEvent(msg tea.Msg) {
	app.events.Publish(pubsub.UpdatedEvent, msg)
}

// AgentNotifications returns the broker for agent notification events.
func (app *App) AgentNotifications() *pubsub.Broker[notify.Notification] {
	return app.agentNotifications
}

// RunCompletions returns the broker for the authoritative per-run
// terminal RunComplete events. The dispatcher (backend.runAgent) uses
// it to emit a reliable terminal event when a run fails before the
// coordinator could publish one of its own.
func (app *App) RunCompletions() *pubsub.Broker[notify.RunComplete] {
	return app.runCompletions
}

// ReportCurrentSession tells herdr which session the user is now
// viewing so it can persist a resumable reference for the pane. Safe
// to call when not running inside a herdr pane; the underlying client
// is nil-safe. Call this whenever the active session changes (load,
// new, or select).
func (app *App) ReportCurrentSession(sessionID string) {
	app.currentSessionID.Store(sessionID)
	app.herdrClient.SetSessionID(sessionID)
}

func (app *App) UpdateAgentModel(ctx context.Context) error {
	if app.AgentCoordinator == nil {
		return fmt.Errorf("agent configuration is missing")
	}
	return app.AgentCoordinator.UpdateModels(ctx)
}

func (app *App) InitCoderAgent(ctx context.Context) error {
	return app.initCoderAgent(ctx, true)
}

// InitCoderAgentNonInteractive initializes the coder agent without
// interactive-only tools (e.g. question).
func (app *App) InitCoderAgentNonInteractive(ctx context.Context) error {
	return app.initCoderAgent(ctx, false)
}

func (app *App) initCoderAgent(ctx context.Context, interactive bool) error {
	coderAgentCfg := app.config.Config().Agents[config.AgentCoder]
	if coderAgentCfg.ID == "" {
		return fmt.Errorf("coder agent configuration is missing")
	}
	var err error
	app.AgentCoordinator, err = agent.NewCoordinator(ctx, agent.CoordinatorOptions{
		Config:      app.config,
		Sessions:    app.Sessions,
		Messages:    app.Messages,
		Permissions: app.Permissions,
		Questions:   app.Questions,
		History:     app.History,
		FileTracker: app.FileTracker,
		LSPManager:  app.LSPManager,
		Notify:      app.agentNotifications,
		RunComplete: app.runCompletions,
		Tasks:       app.tasks,
		Skills:      app.Skills,
		HookRunner:  app.hookRunner,
		Interactive: interactive,
	})
	if err != nil {
		slog.Error("Failed to create coder agent", "err", err)
		return err
	}
	return nil
}

// Shutdown performs a graceful shutdown of the application.
func (app *App) Shutdown() {
	start := time.Now()
	defer func() { slog.Debug("Shutdown took " + time.Since(start).String()) }()

	// First, cancel all agents and wait for them to finish. This must complete
	// before closing the DB so agents can finish writing their state.
	if app.AgentCoordinator != nil {
		app.AgentCoordinator.CancelAll()
	}
	app.fireSessionEnd()

	// Lock again every chat this Crush unlocked (owner, 2026-11-03:
	// quitting Crush relocks), before the database closes.
	if app.LockedChats != nil {
		// Stop background commands too: the idle gate refuses live jobs.
		shell.GetBackgroundShellManager().KillAll(context.Background())
		app.LockedChats.Stop(context.Background())
	}
	// Bound only the remaining cleanup, after turns and relocking finished.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Drain any debounced message updates before the DB-close cleanup
	// runs in the parallel block below. message.Service buffers
	// streaming deltas (see internal/message/message.go) and we must
	// land them while the connection is still open.
	if app.Messages != nil {
		if err := app.Messages.FlushAll(shutdownCtx); err != nil {
			slog.Error("Failed to flush pending message updates on shutdown", "error", err)
		}
	}

	// Now run remaining cleanup tasks in parallel.
	var wg sync.WaitGroup

	// Send exit event
	wg.Go(func() {
		event.AppExited()
	})

	// Kill all background shells.
	wg.Go(func() {
		shell.GetBackgroundShellManager().KillAll(shutdownCtx)
	})

	// Close herdr client to stop its background writer.
	app.herdrClient.Close()

	// Shutdown all LSP clients.
	wg.Go(func() {
		app.LSPManager.KillAll(shutdownCtx)
	})

	// Call all cleanup functions.
	for _, cleanup := range app.cleanupFuncs {
		if cleanup != nil {
			wg.Go(func() {
				if err := cleanup(shutdownCtx); err != nil {
					slog.Error("Failed to cleanup app properly on shutdown", "error", err)
				}
			})
		}
	}
	wg.Wait()
}

// checkForUpdates checks for available updates.
func (app *App) checkForUpdates(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	info, err := update.Check(checkCtx, version.Version, update.Default)
	if err != nil || !info.Available() {
		return
	}
	app.events.Publish(pubsub.UpdatedEvent, UpdateAvailableMsg{
		CurrentVersion: info.Current,
		LatestVersion:  info.Latest,
		IsDevelopment:  info.IsDevelopment(),
	})
}

// newLockedChats makes the folder's locked-chats service. Prompts and
// locking share the coordinator's run locks through its idle gate.
func (app *App) newLockedChats(conn *sql.DB, store *config.ConfigStore) *lockedchats.Service {
	cache, _ := os.UserCacheDir()
	if cache != "" {
		cache = filepath.Join(cache, "crush")
	}
	return &lockedchats.Service{
		DB:       conn,
		DataDir:  store.Config().Options.DataDirectory,
		Folder:   store.WorkingDir(),
		Sessions: app.Sessions,
		Vault:    lockedchats.NewVault(),
		OtherAttachmentStores: func() ([]string, error) {
			known, err := projects.List()
			if err != nil {
				return nil, err
			}
			var paths []string
			for _, project := range known {
				if project.DataDir != "" {
					paths = append(paths, filepath.Join(project.DataDir, "crush.db"))
				}
			}
			return paths, nil
		},
		StopWork: func(ctx context.Context, ids []string) error {
			if drain, ok := app.AgentCoordinator.(agent.LockShutdownGate); ok {
				if err := drain.StopForLock(ctx, ids); err != nil {
					return err
				}
			}
			return app.Messages.FlushAll(ctx)
		},
		Gate: func(ctx context.Context, ids []string, fn func() error) error {
			if gate, ok := app.AgentCoordinator.(agent.IdleChatGate); ok {
				return gate.WithIdleChat(ctx, ids, fn)
			}
			return fn()
		},
		Shown: lockedChatShown,
		Notify: func(sessionID, text string) {
			app.agentNotifications.Publish(pubsub.CreatedEvent, notify.Notification{
				SessionID: sessionID,
				Type:      notify.TypeAgentError,
				Message:   text,
			})
		},
		PromptHistory: prompthistory.Path(),
		CacheDir:      cache,
	}
}

// lockedChatShown reports whether a running app shows the chat: it is in
// the open-chats list the terminal and desktop share, and an app runs. An
// unlocked chat no app shows is locked again (closed with ×, Complete, or
// every app quit).
func lockedChatShown(id string) bool {
	list, err := openchats.Open()
	if err != nil {
		return true // can't tell; quitting relocks it anyway
	}
	if !list.AppOpen() {
		return false
	}
	return slices.ContainsFunc(list.Chats(), func(c openchats.Chat) bool { return c.ID == id })
}
