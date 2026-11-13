package cliagent

import (
	"bytes"
	"cmp"
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// opencodeBash replaces OpenCode's bash tool with one Ctrl+B can move to the
// background (OpenCode has no way to do it itself). OpenCode loads it from
// OPENCODE_CONFIG_DIR, on top of the user's own config.
//
//go:embed opencode/tools/bash.ts
var opencodeBash []byte

// opencodeEnv readies Crush's OpenCode config dir and returns the env for an
// OpenCode turn, plus the file the tool watches for Ctrl+B. noAutoCompact
// stops OpenCode compacting the chat by itself, for the free DeepSeek's
// whole-context switch. after is the user's time before a command moves to
// the background (options.auto_background_seconds; zero means the default),
// which the tool reads from CRUSH_BASH_AUTO_BACKGROUND_MS (owner,
// 2026-11-12: OpenCode chats kept their own 11 seconds whatever the setting).
func opencodeEnv(noAutoCompact bool, after time.Duration) (env []string, signal string) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, ""
	}
	// Each switch setting has its own dir: chats can start OpenCode at the
	// same time, and one turn's config must not change another's.
	dir := filepath.Join(cache, "crush", "opencode")
	if noAutoCompact {
		dir += "-whole-context"
	}
	tool := filepath.Join(dir, "tools", "bash.ts")
	if old, _ := os.ReadFile(tool); !bytes.Equal(old, opencodeBash) {
		if os.MkdirAll(filepath.Dir(tool), 0o700) != nil || os.WriteFile(tool, opencodeBash, 0o600) != nil {
			return nil, "" // OpenCode's own bash tool still works
		}
	}
	writeCompactionSwitch(dir, noAutoCompact)
	f, err := os.CreateTemp("", "crush-ctrl-b-*")
	if err != nil {
		return nil, ""
	}
	signal = f.Name()
	f.Close()
	// Created in the past, so the tool doesn't read it as a press.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(signal, old, old)
	after = cmp.Or(after, autoBackgroundAfter)
	return []string{"OPENCODE_CONFIG_DIR=" + dir, "CRUSH_BG_SIGNAL=" + signal, "CRUSH_BASH_AUTO_BACKGROUND_MS=" + strconv.FormatInt(after.Milliseconds(), 10)}, signal
}

// opencodeNoCompaction is the config that turns OpenCode's own compaction
// off. OpenCode layers this dir on top of the user's own config, so nothing
// of the user's is replaced.
var opencodeNoCompaction = []byte("{\n  \"compaction\": {\n    \"auto\": false\n  }\n}\n")

// writeCompactionSwitch leaves OpenCode's config as the switch asks: its own
// compaction off while Crush gives the model its whole window, and the file
// gone otherwise (older Crush wrote it into the shared dir). Only a file
// Crush wrote is removed.
func writeCompactionSwitch(dir string, noAutoCompact bool) {
	file := filepath.Join(dir, "opencode.json")
	old, _ := os.ReadFile(file)
	if !noAutoCompact {
		if bytes.Equal(old, opencodeNoCompaction) {
			_ = os.Remove(file)
		}
		return
	}
	if bytes.Equal(old, opencodeNoCompaction) {
		return
	}
	if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(file, opencodeNoCompaction, 0o600) != nil {
		slog.Debug("Writing OpenCode's compaction switch failed")
	}
}

// pressCtrlB tells OpenCode's bash tool to move its running commands to the
// background.
func pressCtrlB(signal string) bool {
	now := time.Now()
	return os.Chtimes(signal, now, now) == nil
}
