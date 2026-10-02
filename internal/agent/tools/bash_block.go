package tools

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
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
			if !literalWord(w) {
				blocked = true
				return false
			}
			argsForWord, err := expand.Fields(&expand.Config{}, w)
			if err != nil || len(argsForWord) != 1 {
				blocked = true
				return false
			}
			args = append(args, argsForWord[0])
		}
		blocked = argsBlocked(args)
		return !blocked
	})
	return blocked
}

// wrappers run the command that follows them.
var wrappers = []string{"env", "exec", "command", "nohup", "nice", "time", "timeout", "xargs", "stdbuf", "setsid", "builtin"}

func argsBlocked(args []string) bool {
	for len(args) > 0 && slices.Contains(wrappers, filepath.Base(args[0])) {
		var ok bool
		args, ok = unwrapCommand(filepath.Base(args[0]), args[1:])
		if !ok {
			return true
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
	// Shell scripts and unknown shell options cannot be resolved safely.
	switch args[0] {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		return shellBlocked(args[0], args[1:])
	}
	return false
}

func unwrapCommand(name string, args []string) ([]string, bool) {
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			args = args[1:]
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			break
		}
		option, _, attached := strings.Cut(arg, "=")
		operand, flag := false, false
		switch name {
		case "env":
			operand = slices.Contains([]string{"-u", "--unset", "-C", "--chdir", "--argv0"}, option)
			flag = slices.Contains([]string{"-i", "--ignore-environment", "-0", "--null", "-v", "--debug"}, arg)
			if strings.HasPrefix(arg, "-u") && len(arg) > 2 && !strings.HasPrefix(arg, "--") {
				flag = true
			}
		case "exec":
			operand = option == "-a"
			flag = arg == "-c" || arg == "-l" || arg == "-cl" || arg == "-lc"
		case "command":
			if arg == "-v" || arg == "-V" {
				return nil, true // Lookup only; it does not execute the operand.
			}
			flag = arg == "-p"
		case "nice":
			operand = option == "-n" || option == "--adjustment"
			if _, err := strconv.Atoi(strings.TrimPrefix(arg, "-")); err == nil {
				flag = true
			}
		case "timeout":
			operand = slices.Contains([]string{"-s", "--signal", "-k", "--kill-after"}, option)
			flag = slices.Contains([]string{"--foreground", "--preserve-status", "-v", "--verbose"}, arg)
		case "time":
			operand = slices.Contains([]string{"-f", "--format", "-o", "--output"}, option)
			flag = slices.Contains([]string{"-p", "--portability", "-a", "--append", "-v", "--verbose", "-q", "--quiet"}, arg)
		case "stdbuf":
			operand = slices.Contains([]string{"-i", "--input", "-o", "--output", "-e", "--error"}, option)
			if len(arg) > 2 && arg[0] == '-' && strings.ContainsRune("ioe", rune(arg[1])) {
				flag = true
			}
		case "setsid":
			flag = slices.Contains([]string{"-c", "--ctty", "-f", "--fork", "-w", "--wait"}, arg)
		}
		if operand {
			if attached {
				args = args[1:]
			} else if len(args) > 1 {
				args = args[2:]
			} else {
				return nil, false
			}
		} else if flag {
			args = args[1:]
		} else {
			return nil, false
		}
	}
	if name == "timeout" {
		if len(args) < 2 || !isDuration(args[0]) {
			return nil, false
		}
		args = args[1:]
	}
	if name == "env" {
		for len(args) > 0 && strings.Contains(args[0], "=") {
			args = args[1:]
		}
	}
	// xargs can obtain executable arguments from uninspected stdin.
	if name == "xargs" {
		return nil, false
	}
	return args, true
}

func shellBlocked(name string, args []string) bool {
	command := false
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if name == "fish" && arg == "--command" {
			return len(args) == 0 || CommandBlocked(args[0])
		}
		if name == "fish" && strings.HasPrefix(arg, "--command=") {
			return CommandBlocked(strings.TrimPrefix(arg, "--command="))
		}
		if slices.Contains([]string{"--noprofile", "--norc", "--posix", "--login"}, arg) {
			continue
		}
		if slices.Contains([]string{"--rcfile", "--init-file", "-o", "-O"}, arg) {
			if len(args) == 0 {
				return true
			}
			// Startup files execute additional, uninspected commands.
			if strings.HasPrefix(arg, "--") {
				return true
			}
			args = args[1:]
			continue
		}
		if arg == "--" {
			return !command || len(args) == 0 || CommandBlocked(args[0])
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return !command || CommandBlocked(arg)
		}
		if strings.HasPrefix(arg, "--") {
			return true
		}
		flags := strings.TrimPrefix(arg, "-")
		if strings.ContainsAny(flags, "oO") || strings.Trim(flags, "abcefhiklmnpstuvxBCEHPT") != "" {
			return true
		}
		if strings.ContainsRune(flags, 'c') {
			command = true
		}
	}
	return true
}

func literalWord(w *syntax.Word) bool {
	return literalParts(w.Parts, false)
}

func literalParts(parts []syntax.WordPart, quoted bool) bool {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if !quoted && strings.ContainsAny(part.Value, "*?[~") {
				return false
			}
		case *syntax.SglQuoted:
		case *syntax.DblQuoted:
			if !literalParts(part.Parts, true) {
				return false
			}
		default:
			return false
		}
	}
	return true
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
