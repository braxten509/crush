package cliagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"regexp"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/config"
)

// Sub-agents start without the user's MCP add-on servers unless spawned
// with --mcp ([Model.NoMCP]). Each CLI has its own way to leave them off:
// Claude through --strict-mcp-config (startClaude), the others below.
// OpenCode's ACP sessions start none of their own, and AGY has no switch
// for them.

// grokEnv is the environment for a Grok turn. Without add-ons it skips the
// servers Grok imports from Claude's, Cursor's and its managed config;
// Grok has no switch for the ones in its own config.toml.
func grokEnv(m *Model, env []string) []string {
	// Crush hands every CLI the shared memory; Grok's own stays off.
	env = append(slices.Clone(env), "GROK_MEMORY=0")
	if m.NoMCP {
		env = append(env, "GROK_CLAUDE_MCPS_ENABLED=false", "GROK_CURSOR_MCPS_ENABLED=false", "GROK_MANAGED_MCPS_ENABLED=false")
	}
	return append(env, config.AccountEnv(m.Kind, m.Account)...)
}

// codexMCPName is a name Codex accepts for an MCP server, and so one that
// is safe in a -c key.
var codexMCPName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// codexNoMCPArgs are the app-server arguments that turn off every MCP
// server Codex would start in dir, and its ChatGPT apps. Codex merges -c
// tables into its config rather than replacing them, so each server is
// turned off by name; its own command or URL keeps the entry valid when a
// plugin, not the config, defines it.
func codexNoMCPArgs(dir string, env []string) []string {
	args := []string{"--disable", "apps"}
	for _, s := range codexMCPServers(dir, env) {
		if !codexMCPName.MatchString(s.Name) {
			continue
		}
		key, value := "command", s.Transport.Command
		if s.Transport.Type != "stdio" {
			key, value = "url", s.Transport.URL
		}
		quoted, _ := json.Marshal(value) // a JSON string is a TOML basic string
		args = append(args, "-c", "mcp_servers."+s.Name+"={"+key+"="+string(quoted)+",enabled=false}")
	}
	return args
}

type codexMCPServer struct {
	Name      string `json:"name"`
	Transport struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		URL     string `json:"url"`
	} `json:"transport"`
}

// codexMCPServers lists the MCP servers Codex would start in dir, from its
// config and plugins alike.
func codexMCPServers(dir string, env []string) []codexMCPServer {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "mcp", "list", "--json")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.Output()
	var servers []codexMCPServer
	if err == nil {
		err = json.Unmarshal(out, &servers)
	}
	if err != nil {
		slog.Warn("Could not list Codex's MCP servers; the sub-agent may start them", "error", err)
	}
	return servers
}
