package agent

import (
	"os"
	"slices"
	"sync"
	"time"
)

// Remember helper identities while their CLI is alive. Reparenting must not
// turn an MCP or language server into a job and start another agent turn.
var backgroundHelpers = helperProcesses{seen: make(map[detachedKey]time.Time)}

type helperProcesses struct {
	sync.Mutex
	seen map[detachedKey]time.Time
}

func (h *helperProcesses) classify(table map[int]proc) {
	h.Lock()
	defer h.Unlock()
	now := time.Now()
	// Scans may cover different hubs. Retain identities across those scans,
	// but let disappeared helpers expire rather than keeping a PID forever.
	for key, last := range h.seen {
		if now.Sub(last) > time.Minute {
			delete(h.seen, key)
		}
	}
	checked := make(map[int]bool)
	var helper func(proc) bool
	helper = func(p proc) bool {
		if checked[p.pid] {
			return table[p.pid].cliHelper
		}
		checked[p.pid] = true
		if p.nativeShellID != "" || p.managedJobID != "" || p.ppid == os.Getpid() || isForegroundCommand(p, table) {
			return false
		}
		key := detachedKey{p.pid, p.started}
		_, remembered := h.seen[key]
		parent, attached := table[p.ppid]
		stdio := slices.Contains(p.args, "--stdio") || slices.Contains(p.args, "-stdio")
		isHelper := remembered || (attached && helper(parent))
		if !isHelper && !isShell(p.args) {
			isHelper = (!attached && stdio) || (attached && parent.ppid == os.Getpid() && (p.sid != p.pid || stdio))
		}
		if isHelper {
			p.cliHelper = true
			table[p.pid] = p
			h.seen[key] = now
		}
		return isHelper
	}
	for _, p := range table {
		helper(p)
	}
}
