package cliagent

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/shell"
	"mvdan.cc/sh/v3/syntax"
)

// foregroundCalls exposes each live CLI's pending shell calls to the
// background process list. Tool results remove calls once the command
// finishes or the CLI hands it back running in the background.
var foregroundCalls sync.Map // CLI process ID -> *openCalls

// IsForegroundCommand reports whether args belongs to a shell call cliPID
// is still waiting on. The start time distinguishes older background jobs
// that ran the same command from a new foreground call.
func IsForegroundCommand(cliPID int, args []string, started time.Time) bool {
	v, ok := foregroundCalls.Load(cliPID)
	if !ok {
		return false
	}
	for _, c := range v.(*openCalls).list() {
		if !started.Before(c.at.Add(-500*time.Millisecond)) && c.runBy(args) {
			return true
		}
	}
	return false
}

// runBy reports whether a process with these arguments runs the call: as a
// shell's script, or run directly with the words the shell would give it.
func (c openCall) runBy(args []string) bool {
	command := CommandText(args)
	if (command != strings.Join(args, " ") && strings.Contains(command, c.command)) || slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, c.command) }) {
		return true
	}
	if len(c.words) == 0 || len(args) < len(c.words) {
		return false
	}
	return filepath.Base(args[0]) == filepath.Base(c.words[0]) && slices.Equal(args[1:len(c.words)], c.words[1:])
}

// CommandText returns the script a shell runs, decoding the CLI's eval wrapper.
// Background listing and foreground cancellation use the same normalization.
func CommandText(args []string) string {
	if len(args) == 0 {
		return ""
	}
	name := strings.TrimPrefix(filepath.Base(args[0]), "-")
	if slices.Contains([]string{"sh", "bash", "zsh", "dash", "ksh", "fish"}, name) {
		for i, arg := range args[1:] {
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") && i+2 < len(args) {
				script := args[i+2]
				if file, err := syntax.NewParser().Parse(strings.NewReader(script), ""); err == nil {
					var decoded string
					syntax.Walk(file, func(node syntax.Node) bool {
						call, ok := node.(*syntax.CallExpr)
						if decoded != "" || !ok || len(call.Args) < 2 || call.Args[0].Lit() != "eval" {
							return decoded == ""
						}
						var word strings.Builder
						if syntax.NewPrinter().Print(&word, call.Args[1]) == nil {
							if fields, err := shell.Fields(word.String(), func(string) string { return "" }); err == nil && len(fields) == 1 {
								decoded = fields[0]
							}
						}
						return decoded == ""
					})
					if decoded != "" {
						script = decoded
					}
				}
				return strings.TrimSpace(script)
			}
		}
	}
	return strings.Join(args, " ")
}
