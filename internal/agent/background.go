package agent

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
)

// Process is a command an agent CLI started that is still running: a shell
// its tools opened, or anything that outlived one. The CLIs themselves and
// their helpers (MCP servers, search tools) aren't listed.
type Process struct {
	PID     int
	Command string
	Started time.Time
}

// proc is one process from the process table.
type proc struct {
	pid, ppid     int
	sid           int // session ID
	args          []string
	started       time.Time
	nativeShellID string
	// busService is set on services a D-Bus daemon started on demand for a
	// command (portals, secret stores); they aren't commands the agent ran.
	busService bool
}

// markedProcs lists the processes carrying one of the hubs' environment
// marker, which every agent CLI of theirs, and everything those start,
// inherits. It is nil where the process table can't be read.
var markedProcs = func(markers []string) map[int]proc { return nil }

func hubMarkers() []string {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	markers := make([]string, len(hubs))
	for i, h := range hubs {
		markers[i] = TasksDirEnv + "=" + h.dir
	}
	return markers
}

// BackgroundProcesses lists what the main agents' CLIs left running.
func BackgroundProcesses() []Process {
	markers := hubMarkers()
	if len(markers) == 0 {
		return nil
	}
	procs := markedProcs(markers)
	var out []Process
	for _, p := range procs {
		if !p.busService && isCommandRoot(p, procs) && !isForegroundCommand(p, procs) {
			out = append(out, Process{PID: p.pid, Command: commandText(p.args), Started: p.started})
		}
	}
	slices.SortFunc(out, func(a, b Process) int { return a.Started.Compare(b.Started) })
	return out
}

// isForegroundCommand checks the owning CLI's pending tools before listing
// a command as background. Include the unwrapped shell script so Claude's
// quoting does not prevent a match.
func isForegroundCommand(p proc, procs map[int]proc) bool {
	if p.nativeShellID != "" && tools.IsForegroundShell(p.nativeShellID) {
		return true
	}
	args := append(slices.Clone(p.args), commandText(p.args))
	for parent := p.ppid; parent > 0; {
		if cliagent.IsForegroundCommand(parent, args, p.started) {
			return true
		}
		q, ok := procs[parent]
		if !ok || q.ppid == parent {
			break
		}
		parent = q.ppid
	}
	return false
}

// isCommandRoot reports whether p is where a command starts: the shell a
// CLI tool opened, or a process whose parent is gone (a command that
// detached or outlived its shell).
func isCommandRoot(p proc, procs map[int]proc) bool {
	parent, marked := procs[p.ppid]
	if !marked {
		if p.ppid == os.Getpid() && p.sid == p.pid && p.nativeShellID != "" {
			return true
		}
		// The CLI itself is Crush's child; anything else was orphaned.
		return p.ppid != os.Getpid()
	}
	if p.sid == p.pid && parent.ppid == os.Getpid() {
		// Codex starts commands in sessions of their own, without a shell
		// in between; the CLIs' helpers don't do that.
		return true
	}
	return isShell(p.args) && !isShell(parent.args)
}

func isShell(args []string) bool {
	if len(args) == 0 {
		return false
	}
	name := args[0][strings.LastIndexByte(args[0], '/')+1:]
	name = strings.TrimPrefix(name, "-") // login shells
	return slices.Contains([]string{"sh", "bash", "zsh", "dash", "ksh", "fish"}, name)
}

// claudeEval pulls the command out of Claude Code's shell wrapper.
var claudeEval = regexp.MustCompile(`eval '((?:[^']|'\\'')*)'`)

// commandText is what a user would call the command: the script a shell
// runs rather than the shell, without the CLIs' wrappers.
func commandText(args []string) string {
	if isShell(args) {
		for i, a := range args[1:] {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c") && i+2 < len(args) {
				script := args[i+2]
				if m := claudeEval.FindStringSubmatch(script); m != nil {
					script = strings.ReplaceAll(m[1], `'\''`, "'")
				}
				return strings.TrimSpace(script)
			}
		}
	}
	return strings.Join(args, " ")
}

// KillProcess ends a listed background process and everything it started:
// SIGTERM first, then SIGKILL for whatever is left a few seconds later.
func KillProcess(pid int) error {
	markers := hubMarkers()
	procs := markedProcs(markers)
	root, ok := procs[pid]
	if !ok || !isCommandRoot(root, procs) {
		return fmt.Errorf("process %d is not one of the agents' background processes", pid)
	}
	tree := []int{pid}
	for i := 0; i < len(tree); i++ {
		for _, p := range procs {
			if p.ppid == tree[i] {
				tree = append(tree, p.pid)
			}
		}
	}
	for _, p := range tree {
		signal(p, syscall.SIGTERM)
	}
	go func() {
		time.Sleep(3 * time.Second)
		// Only what is still the same process: check it still carries
		// the marker before killing, since PIDs get reused.
		left := markedProcs(markers)
		for _, p := range tree {
			if q, ok := left[p]; ok && q.started.Equal(procs[p].started) {
				signal(p, syscall.SIGKILL)
			}
		}
	}()
	return nil
}

func signal(pid int, sig syscall.Signal) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(sig)
	}
}
