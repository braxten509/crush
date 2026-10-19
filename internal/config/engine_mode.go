package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EngineMode is where a terminal window in workingDir runs its chats
// (engine-design.md 5.13): CRUSH_ENGINE when it names a mode, else
// options.engine from the config files and the workspace overlay, else
// EngineShared. It reads only the config files, without catalogs or
// providers, so the shared path can decide before it joins the engine.
func EngineMode(workingDir, dataDir string) (string, error) {
	if mode, ok := engineModeOf(os.Getenv("CRUSH_ENGINE")); ok {
		return mode, nil
	}
	cfg, _, err := loadFromConfigPaths(context.Background(), lookupConfigs(workingDir))
	if err != nil {
		return EngineShared, fmt.Errorf("failed to read the engine setting: %w", err)
	}
	cfg.setDefaults(workingDir, dataDir)
	_, _ = cfg.applyWorkspaceOverlay(workingDir, filepath.Join(cfg.Options.DataDirectory, appName+".json"))
	if cfg.Options != nil {
		if mode, ok := engineModeOf(cfg.Options.Engine); ok {
			return mode, nil
		}
	}
	return EngineShared, nil
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
