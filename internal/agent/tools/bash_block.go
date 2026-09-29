package tools

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

// CommandBlocked reports whether Crush's bash tool would refuse to run
// command, even in YOLO mode. It checks commands other shells will run (an
// agent CLI's own tools), so it looks at every command the line contains,
// including ones behind wrappers like `env` or `bash -c`. Anything it can't
// parse is blocked.
func CommandBlocked(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return true
	}
	blocked := false
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || blocked {
			return !blocked
		}
		args := make([]string, 0, len(call.Args))
		for _, w := range call.Args {
			args = append(args, wordText(w))
		}
		blocked = argsBlocked(args)
		return !blocked
	})
	return blocked
}

// wrappers run the command that follows them.
var wrappers = []string{"env", "exec", "command", "nohup", "nice", "time", "timeout", "xargs", "stdbuf", "setsid", "builtin"}

func argsBlocked(args []string) bool {
	// Skip wrappers and their options (and env assignments).
	for len(args) > 0 {
		name := filepath.Base(args[0])
		if !slices.Contains(wrappers, name) {
			break
		}
		args = args[1:]
		for len(args) > 0 && (strings.HasPrefix(args[0], "-") || strings.Contains(args[0], "=") || (name == "timeout" && isDuration(args[0]))) {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return false
	}
	args = append([]string{filepath.Base(args[0])}, args[1:]...)
	for _, block := range blockFuncs() {
		if block(args) {
			return true
		}
	}
	// A nested shell runs its -c script.
	switch args[0] {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		if i := slices.Index(args, "-c"); i >= 0 && i+1 < len(args) {
			return CommandBlocked(args[i+1])
		}
	}
	return false
}

func isDuration(s string) bool {
	return strings.TrimRight(s, "0123456789.smhd") == "" && s != ""
}

// wordText is a word's literal text, with quotes removed and expansions
// left out.
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

// ForegroundWaitLimit is the longest wait protected from automatic backgrounding.
const ForegroundWaitLimit = 10 * time.Second

// SleepRefusal tells an agent why its sleep didn't run.
const SleepRefusal = "Blocked: a `sleep` longer than 10 seconds in the foreground only stalls you. Run the wait in the background, or loop on a check for what you're waiting on (until <check>; do sleep 2; done)."

// LeadingSleep reports whether command starts by sleeping for more than
// 10 seconds in the foreground, which only stalls the agent. Like Claude Code,
// Crush refuses it; a wait belongs in the background or in a loop that
// checks for what it's waiting on.
func LeadingSleep(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		return false
	}
	stmt := file.Stmts[0]
	cmd := stmt.Cmd
	// The first command of a `sleep 30 && ...` chain.
	for {
		bin, ok := cmd.(*syntax.BinaryCmd)
		if !ok || stmt.Background {
			break
		}
		if bin.Op == syntax.Pipe || bin.Op == syntax.PipeAll {
			return false
		}
		stmt = bin.X
		cmd = stmt.Cmd
	}
	call, ok := cmd.(*syntax.CallExpr)
	if !ok || stmt.Background || len(call.Args) != 2 || wordText(call.Args[0]) != "sleep" {
		return false
	}
	arg := wordText(call.Args[1])
	d, err := time.ParseDuration(arg)
	if err != nil {
		secs, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			return false
		}
		d = time.Duration(secs * float64(time.Second))
	}
	return d > ForegroundWaitLimit
}
