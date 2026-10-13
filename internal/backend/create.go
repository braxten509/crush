package backend

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/cpuaffinity"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/google/uuid"
)

// CreateWorkspace initializes a new workspace from the given
// parameters, or returns an existing workspace if one already exists at
// the same resolved path (first-wins semantics).
//
// args.ClientID must be a valid UUID identifying the calling client;
// the resulting workspace registers a creation hold on behalf of that
// client which is released either by the first SSE attach (which
// converts it into a stream claim) or by the grace window expiring.
func (b *Backend) CreateWorkspace(args proto.Workspace) (*Workspace, proto.Workspace, error) {
	if args.Path == "" {
		return nil, proto.Workspace{}, ErrPathRequired
	}
	clientID, err := validateClientID(args.ClientID)
	if err != nil {
		return nil, proto.Workspace{}, err
	}

	key, err := resolveWorkspaceKey(args.Path)
	if err != nil {
		return nil, proto.Workspace{}, fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	b.mu.Lock()
	if err := b.admitLocked(clientID); err != nil {
		b.mu.Unlock()
		return nil, proto.Workspace{}, err
	}
	// A client is arriving: cancel any pending idle shutdown so we never
	// hand back a workspace on a server that is about to tear itself down.
	b.cancelShutdownLocked()
	if existingID, ok := b.pathIndex[key]; ok {
		if ws, found := b.workspaces.Get(existingID); found {
			// Hold b.mu while registering: teardown also
			// acquires b.mu before tearing the workspace
			// down, so this guarantees the workspace we
			// return cannot be torn out from under us
			// between lookup and registerClient. Lock order
			// here is b.mu -> ws.clientsMu.
			if !stringSlicesEqual(ws.Cfg.Overrides().EnabledChannels, args.Channels) {
				b.mu.Unlock()
				return nil, proto.Workspace{}, ErrChannelOptInMismatch
			}
			logFirstWinsMismatch(ws, args)
			b.registerClient(ws, clientID)
			b.mu.Unlock()
			return ws, workspaceToProto(ws), nil
		}
		// pathIndex referenced a workspace that has since been
		// removed; clean the stale entry and fall through.
		delete(b.pathIndex, key)
	}
	// Commit to the slow creation path. Mark this create as pending
	// while mu is still held so a teardown that runs during the
	// unlocked init below cannot observe an empty backend and shut the
	// server down. The deferred decrement runs after the workspace has
	// been registered (or the create has failed), keeping the invariant
	// that pending only drops once the workspace is visible in the map.
	b.pending++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.pending--
		// If this create ended up registering nothing (it failed, or
		// deduped onto an existing workspace that has since gone) and
		// it was holding the last teardown back, the server may now be
		// idle with no pending work. Arm the idle-shutdown timer here so
		// a failed create racing the last teardown does not leak an
		// empty server that a plain teardown already declined to reap.
		shutdownNow := b.scheduleShutdownIfIdleLocked()
		b.mu.Unlock()
		if shutdownNow {
			slog.Info("No workspaces remain after create settled, shutting down server...")
			b.shutdownFn()
		}
	}()

	id := uuid.New().String()
	cfg, err := config.Init(args.Path, args.DataDir, args.Debug)
	if err != nil {
		return nil, proto.Workspace{}, fmt.Errorf("failed to initialize config: %w", err)
	}

	cpuaffinity.ApplyOptions(cfg.Config().Options)
	cfg.Overrides().SkipPermissionRequests = args.YOLO
	cfg.Overrides().EnabledChannels = args.Channels

	if err := createDotCrushDir(cfg.Config().Options.DataDirectory); err != nil {
		return nil, proto.Workspace{}, fmt.Errorf("failed to create data directory: %w", err)
	}

	if !testing.Testing() {
		if err := projects.Register(args.Path, cfg.Config().Options.DataDirectory); err != nil {
			slog.Warn("Failed to register project", "error", err)
		}
	}
	conn, err := db.Connect(b.ctx, cfg.Config().Options.DataDirectory, db.WithDataDirLock(true))
	if err != nil {
		return nil, proto.Workspace{}, fmt.Errorf("failed to connect to database: %w", err)
	}
	db.PruneIdleReviewsInBackground(b.ctx, conn)

	// Discover skills once per workspace, before app.New. The backend
	// hosts multiple workspaces concurrently, so the manager is
	// constructed WITHOUT WithGlobalMirror to prevent last-writer-wins
	// cross-talk between workspaces.
	discoveryCfg := skillsDiscoveryConfig(cfg)
	allSkills, activeSkills, skillStates := skills.DiscoverFromConfig(discoveryCfg)
	skillsMgr := skills.NewManager(
		allSkills, activeSkills, skillStates,
		skills.WithResolvedPaths(discoveryCfg.ResolvePaths()),
		skills.WithWorkingDir(discoveryCfg.WorkingDir),
	)

	appWorkspace, err := app.New(b.ctx, conn, cfg, skillsMgr)
	if err != nil {
		return nil, proto.Workspace{}, fmt.Errorf("failed to create app workspace: %w", err)
	}

	wsCtx, wsCancel := context.WithCancel(b.ctx)
	ws := &Workspace{
		App:          appWorkspace,
		ID:           id,
		Path:         args.Path,
		Cfg:          cfg,
		Env:          args.Env,
		Skills:       skillsMgr,
		resolvedPath: key,
		ctx:          wsCtx,
		cancel:       wsCancel,
		clients:      make(map[string]*clientState),
	}

	b.mu.Lock()
	// Re-check admission: the client may have retired while the slow
	// init above ran with b.mu released, and registering a claim for a
	// client that has already announced its exit would strand the
	// workspace. (b.closing cannot have flipped: every shutdown decision
	// requires pending == 0, and this create has held pending since
	// before it released b.mu.)
	if err := b.admitLocked(clientID); err != nil {
		b.mu.Unlock()
		ws.invokeShutdown()
		return nil, proto.Workspace{}, err
	}
	// Re-check the index under the lock: a concurrent caller may have
	// won the race between the initial unlock and here.
	if existingID, ok := b.pathIndex[key]; ok {
		if existing, found := b.workspaces.Get(existingID); found {
			// Register under b.mu so teardown cannot run
			// between lookup and registerClient. Lock order
			// is b.mu -> ws.clientsMu.
			if !stringSlicesEqual(existing.Cfg.Overrides().EnabledChannels, args.Channels) {
				b.mu.Unlock()
				ws.invokeShutdown()
				return nil, proto.Workspace{}, ErrChannelOptInMismatch
			}
			logFirstWinsMismatch(existing, args)
			b.registerClient(existing, clientID)
			b.mu.Unlock()
			ws.invokeShutdown()
			return existing, workspaceToProto(existing), nil
		}
		delete(b.pathIndex, key)
	}
	b.workspaces.Set(id, ws)
	b.pathIndex[key] = id
	// Register the originating client's hold while still holding
	// b.mu so the workspace is observable with its claim from the
	// moment it appears in the index.
	b.registerClient(ws, clientID)
	b.mu.Unlock()

	// Only logged: the workspace has no event subscribers yet (its
	// clients attach after this returns), and the SSE stream carries no
	// UI notices, so an event here would never reach a client.
	if args.Version != "" && args.Version != version.Version {
		slog.Warn(
			"Client/server version mismatch",
			"client", args.Version,
			"server", version.Version,
		)
	}

	return ws, workspaceToProto(ws), nil
}

// skillsDiscoveryConfig adapts a *config.ConfigStore to the
// skills.DiscoveryConfig that DiscoverFromConfig consumes.
func skillsDiscoveryConfig(cfg *config.ConfigStore) skills.DiscoveryConfig {
	opts := cfg.Config().Options
	var paths, disabled []string
	if opts != nil {
		paths = opts.SkillsPaths
		disabled = opts.DisabledSkills
	}
	var resolver func(string) (string, error)
	if r := cfg.Resolver(); r != nil {
		resolver = r.ResolveValue
	}
	return skills.DiscoveryConfig{
		SkillsPaths:    paths,
		DisabledSkills: disabled,
		WorkingDir:     cfg.WorkingDir(),
		Resolver:       resolver,
	}
}

// resolveWorkspaceKey returns a stable canonical form of path suitable
// for use as a dedup key. It applies filepath.Abs, then attempts
// filepath.EvalSymlinks; because EvalSymlinks errors on non-existent
// paths, it falls back to the cleaned absolute path in that case.
func resolveWorkspaceKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// logFirstWinsMismatch emits a debug line whenever a second
// CreateWorkspace at the same resolved path arrives with flags that
// differ from the originating workspace. The existing workspace wins;
// the incoming flags are silently ignored.
//
// The comparison is done against the incoming args as the caller sent
// them — including empty/zero values — rather than after defaulting.
// This means that, for example, a second caller who omits DataDir
// while the first set one will still log the mismatch.
func logFirstWinsMismatch(existing *Workspace, args proto.Workspace) {
	existingCfg := existing.Cfg.Config()
	existingYOLO := existing.Cfg.Overrides().SkipPermissionRequests
	existingChannels := existing.Cfg.Overrides().EnabledChannels
	if existingYOLO == args.YOLO &&
		existingCfg.Options.Debug == args.Debug &&
		existingCfg.Options.DataDirectory == args.DataDir &&
		stringSlicesEqual(existing.Env, args.Env) &&
		stringSlicesEqual(existingChannels, args.Channels) {
		return
	}
	slog.Debug(
		"Workspace flag mismatch on duplicate create; first wins",
		"workspace_id", existing.ID,
		"path", existing.Path,
		"existing_yolo", existingYOLO,
		"requested_yolo", args.YOLO,
		"existing_debug", existingCfg.Options.Debug,
		"requested_debug", args.Debug,
		"existing_data_dir", existingCfg.Options.DataDirectory,
		"requested_data_dir", args.DataDir,
		"existing_env", existing.Env,
		"requested_env", args.Env,
		"existing_channels", existingChannels,
		"requested_channels", args.Channels,
	)
}

// stringSlicesEqual reports whether a and b contain the same strings
// in the same order. nil and empty are treated as equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
