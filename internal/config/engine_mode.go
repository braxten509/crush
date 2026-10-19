package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EngineMode is where a terminal window in workingDir runs its chats
// (engine-design.md 5.13): CRUSH_ENGINE when it names a mode, else
// options.engine from the config files and the workspace overlay, else
// defaultEngineMode. It reads only the config files, without catalogs or
// providers, so the shared path can decide before it joins the engine.
func EngineMode(workingDir, dataDir string) (string, error) {
	if value := os.Getenv("CRUSH_ENGINE"); value != "" {
		if mode, ok := engineModeOf(value); ok {
			return mode, nil
		}
		slog.Warn("CRUSH_ENGINE is not shared or alone; ignoring it", "value", value)
	}
	cfg, _, err := loadFromConfigPaths(context.Background(), lookupConfigs(workingDir))
	if err != nil {
		return defaultEngineMode(), fmt.Errorf("failed to read the engine setting: %w", err)
	}
	cfg.setDefaults(workingDir, dataDir)
	_, _ = cfg.applyWorkspaceOverlay(workingDir, filepath.Join(cfg.Options.DataDirectory, appName+".json"))
	if cfg.Options != nil {
		if mode, ok := engineModeOf(cfg.Options.Engine); ok {
			return mode, nil
		}
	}
	return defaultEngineMode(), nil
}

// defaultEngineMode is EngineShared, except on Windows, where the engine
// does not run yet (engine-design.md, out of scope): windows there work
// alone unless a setting says otherwise.
func defaultEngineMode() string {
	if runtime.GOOS == "windows" {
		return EngineAlone
	}
	return EngineShared
}

func engineModeOf(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EngineShared:
		return EngineShared, true
	case EngineAlone:
		return EngineAlone, true
	}
	return "", false
}
