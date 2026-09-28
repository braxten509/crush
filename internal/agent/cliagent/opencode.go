package cliagent

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"time"
)

// opencodeBash replaces OpenCode's bash tool with one Ctrl+B can move to the
// background (OpenCode has no way to do it itself). OpenCode loads it from
// OPENCODE_CONFIG_DIR, on top of the user's own config.
//
//go:embed opencode/tools/bash.ts
var opencodeBash []byte

// opencodeEnv readies Crush's OpenCode config dir and returns the env for an
// OpenCode turn, plus the file the tool watches for Ctrl+B.
func opencodeEnv() (env []string, signal string) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, ""
	}
	dir := filepath.Join(cache, "crush", "opencode")
	tool := filepath.Join(dir, "tools", "bash.ts")
	if old, _ := os.ReadFile(tool); !bytes.Equal(old, opencodeBash) {
		if os.MkdirAll(filepath.Dir(tool), 0o700) != nil || os.WriteFile(tool, opencodeBash, 0o600) != nil {
			return nil, "" // OpenCode's own bash tool still works
		}
	}
	f, err := os.CreateTemp("", "crush-ctrl-b-*")
	if err != nil {
		return nil, ""
	}
	signal = f.Name()
	f.Close()
	// Created in the past, so the tool doesn't read it as a press.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(signal, old, old)
	return []string{"OPENCODE_CONFIG_DIR=" + dir, "CRUSH_BG_SIGNAL=" + signal}, signal
}

// pressCtrlB tells OpenCode's bash tool to move its running commands to the
// background.
func pressCtrlB(signal string) bool {
	now := time.Now()
	return os.Chtimes(signal, now, now) == nil
}
