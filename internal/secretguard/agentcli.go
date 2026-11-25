package secretguard

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// AgentCLIReason is what an agent sees when it starts an agent CLI by
// command. Crush can only show and report helpers it starts itself: an Agy
// turn ran `crush bg -- 'codex exec ...'` for a review, and it showed as a
// plain background job instead of a sub-agent (owner, 2026-11-22).
const AgentCLIReason = "Blocked by Crush, not by the user: start helpers with `crush spawn --cli <cli> [--model <model>] [--effort <level>] [--read-only] --name \"<title>\"` and the task on stdin, never by running an agent CLI (claude, codex, grok, opencode, agy, gemini) yourself, in the foreground or through crush bg. Only crush spawn shows the helper as a sub-agent and brings its result back to this chat."

// agentRuns are, per agent CLI, the words that make a command a model run
// rather than housekeeping like `codex login` or `claude mcp list`. A CLI
// with no words at all also starts a run.
var agentRuns = map[string][]string{
	"claude":   {"-p", "--print", "-r", "--resume", "-c", "--continue"},
	"codex":    {"exec", "e", "review", "resume", "fork"},
	"opencode": {"run"},
	"grok":     {"-p", "--prompt"},
	"agy":      {"-p", "--prompt"},
	"gemini":   {"-p", "--prompt", "-i", "--prompt-interactive"},
}

// agentShellWrappers run the command that follows them.
var agentShellWrappers = []string{"env", "exec", "command", "nohup", "nice", "time", "timeout", "stdbuf", "setsid", "builtin", "systemd-run"}

// StartsAgentCLI reports whether a shell command starts an agent CLI run
// (Claude Code, Codex, Grok, OpenCode, AGY or Gemini), looking through
// variable prefixes, wrappers like `timeout` and `env`, and `bash -c`.
// Text it can't parse is let through: this check is about showing helpers
// as sub-agents, not a security boundary.
func StartsAgentCLI(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if found {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		args := make([]string, len(call.Args))
		for i, w := range call.Args {
			args[i] = wordText(w)
		}
		found = argsStartAgentCLI(args)
		return !found
	})
	return found
}

func argsStartAgentCLI(args []string) bool {
	for len(args) > 0 {
		name := filepath.Base(args[0])
		if !slices.Contains(agentShellWrappers, name) {
			break
		}
		// Skip the wrapper's own options and values (a duration, VAR=x)
		// up to the command it runs.
		args = args[1:]
		for len(args) > 0 && (strings.HasPrefix(args[0], "-") || strings.Contains(args[0], "=") || isDuration(args[0])) {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return false
	}
	name := filepath.Base(args[0])
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		for i, arg := range args[1:] {
			if (arg == "-c" || arg == "--command" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'c'))) && i+2 < len(args) {
				return StartsAgentCLI(args[i+2])
			}
		}
		return false
	}
	runs, ok := agentRuns[name]
	if !ok {
		return false
	}
	if len(args) == 1 {
		return true
	}
	for _, arg := range args[1:] {
		flag, _, _ := strings.Cut(arg, "=")
		if slices.Contains(runs, arg) || (strings.HasPrefix(arg, "--") && slices.Contains(runs, flag)) {
			return true
		}
	}
	return false
}

// BlocksAgentCLI reports whether a tool call's JSON input is a shell
// command that starts an agent CLI ([StartsAgentCLI]). It reads the
// input's "command", "cmd", or "CommandLine", a string or a list of words.
func BlocksAgentCLI(input []byte) bool {
	var fields struct {
		Command     json.RawMessage `json:"command"`
		Cmd         json.RawMessage `json:"cmd"`
		CommandLine json.RawMessage `json:"CommandLine"`
	}
	if json.Unmarshal(input, &fields) != nil {
		return false
	}
	raw := fields.Command
	if raw == nil {
		raw = fields.Cmd
	}
	if raw == nil {
		raw = fields.CommandLine
	}
	if raw == nil {
		return false
	}
	var command string
	if json.Unmarshal(raw, &command) == nil {
		return StartsAgentCLI(command)
	}
	var words []string
	if json.Unmarshal(raw, &words) != nil || len(words) == 0 {
		return false
	}
	return argsStartAgentCLI(words)
}

func isDuration(s string) bool {
	return s != "" && strings.TrimRight(s, "0123456789.smhd") == ""
}

// wordText is a word's literal text, with quotes removed and expansions
// left out, so `$HOME/bin/codex` still ends in codex.
func wordText(w *syntax.Word) string {
	var b strings.Builder
	var parts func([]syntax.WordPart)
	parts = func(ps []syntax.WordPart) {
		for _, p := range ps {
			switch p := p.(type) {
			case *syntax.Lit:
				b.WriteString(p.Value)
			case *syntax.SglQuoted:
				b.WriteString(p.Value)
			case *syntax.DblQuoted:
				parts(p.Parts)
			}
		}
	}
	parts(w.Parts)
	return b.String()
}
