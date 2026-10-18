package backend

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/version"
)

// Errors of the engine lifecycle (engine-design.md 5.1 c and 5.2).
var (
	// ErrProtocolMismatch: the app speaks another protocol than this
	// engine. It answers 426 protocol_mismatch.
	ErrProtocolMismatch = errors.New("protocol mismatch")
	// ErrWrongProject: the engine serves another project. It answers 409
	// wrong_project.
	ErrWrongProject = errors.New("wrong project")
)

// EngineIdleShutdownDelay is how long an engine stays alive after its last
// workspace goes. CRUSH_ENGINE_IDLE_TIMEOUT overrides it (tests): a Go
// duration such as "2s", or a whole number of seconds.
const EngineIdleShutdownDelay = 10 * time.Second

// engineIdleShutdownDelayFromEnv returns the engine's idle delay, honoring
// CRUSH_ENGINE_IDLE_TIMEOUT.
func engineIdleShutdownDelayFromEnv() time.Duration {
	v := os.Getenv("CRUSH_ENGINE_IDLE_TIMEOUT")
	if v == "" {
		return EngineIdleShutdownDelay
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return EngineIdleShutdownDelay
}

// SetEngine makes b the engine of one project, whose canonical data folder
// is canonicalDataDir: every app of the project joins one workspace, keyed
// by that folder, creates for other projects are refused, and the server
// exits [EngineIdleShutdownDelay] after the last workspace goes. The legacy
// server never calls it.
func (b *Backend) SetEngine(canonicalDataDir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.engineDataDir = canonicalDataDir
	b.lingerDelay = engineIdleShutdownDelayFromEnv()
}

// EngineDataDir is the engine's canonical data folder, or "" on the legacy
// server.
func (b *Backend) EngineDataDir() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.engineDataDir
}

// Closing reports whether the server decided to exit. Once it has, creates
// are refused with [ErrServerShuttingDown].
func (b *Backend) Closing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

// EngineInfo describes this engine for GET /v1/engine; address is where
// the server listens. The workspace fields are filled when exactly one
// workspace is live, which is always the case for an engine that hosts
// one; Apps counts the registered apps of every workspace.
func (b *Backend) EngineInfo(address string) proto.EngineInfo {
	b.mu.Lock()
	info := proto.EngineInfo{
		Protocol:  proto.ProtocolVersion,
		Version:   version.Version,
		PID:       os.Getpid(),
		DataDir:   b.engineDataDir,
		Address:   address,
		StartedAt: b.startedAt,
		Closing:   b.closing,
	}
	b.mu.Unlock()
	live := 0
	for _, ws := range b.workspaces.Seq2() {
		live++
		info.WorkspaceID, info.WorkspacePath = ws.ID, ws.Path
		ws.clientsMu.Lock()
		info.Apps += len(ws.clients)
		ws.clientsMu.Unlock()
	}
	if live != 1 {
		info.WorkspaceID, info.WorkspacePath = "", ""
	}
	return info
}

// checkProtocol refuses an app that speaks another protocol, on the engine
// and the legacy server alike.
func checkProtocol(args proto.Workspace) error {
	if args.Protocol != proto.ProtocolVersion {
		return fmt.Errorf("%w: this Crush engine speaks protocol %d (Crush %s) and the app speaks protocol %d. Update the app, or close every Crush window on this project so the engine restarts",
			ErrProtocolMismatch, proto.ProtocolVersion, version.Version, args.Protocol)
	}
	return nil
}

// workspaceKey returns the key a create is matched by. The legacy server
// keys workspaces by folder. An engine keys its one workspace by its data
// folder, so apps in different folders of the project join it, and it
// refuses a create whose folder resolves to another project's data folder.
func (b *Backend) workspaceKey(args proto.Workspace) (string, error) {
	engineDir := b.EngineDataDir()
	if engineDir == "" {
		key, err := resolveWorkspaceKey(args.Path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve workspace path: %w", err)
		}
		return key, nil
	}
	dataDir, err := config.ResolveDataDir(args.Path, args.DataDir)
	if err != nil {
		return "", err
	}
	canonical, err := config.CanonicalDataDir(dataDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the data folder %q: %w", dataDir, err)
	}
	if canonical != engineDir {
		return "", fmt.Errorf("%w: this Crush engine serves the project whose data folder is %s, not %s", ErrWrongProject, engineDir, dataDir)
	}
	return engineDir, nil
}
