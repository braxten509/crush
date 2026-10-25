package agent

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Stray processes are what a chat's agent left running outside crush bg
// (adb starting its own server, a `nohup … &`). Crush lists them but had no
// check-ins for them (owner, 2026-10-20: "the 2 minute reminder never
// came"), so a hub scans for them and lists each in its chat's check-ins
// while it runs: a stray holding a local port as a service (named in
// other work's notes, never sent one alone), any other as work.

// strayScanInterval is how often a hub looks for stray processes.
const strayScanInterval = 10 * time.Second

// watchStrays keeps the hub's stray processes in their chats' check-ins
// until the hub closes.
func (h *taskHub) watchStrays() {
	markers := []string{TasksDirEnv + "=" + h.dir}
	known := strayCheckIns{}
	tick := time.NewTicker(strayScanInterval)
	defer tick.Stop()
	for range tick.C {
		if _, err := os.Stat(h.dir); err != nil {
			known.removeAll()
			return
		}
		servers, rest := FindServers(strayProcesses(markers))
		h.syncStrays(known, servers, rest)
	}
}

// strayCheckIns holds each listed stray's remove function.
type strayCheckIns map[detachedKey]func()

func (k strayCheckIns) removeAll() {
	for key, remove := range k {
		remove()
		delete(k, key)
	}
}

// syncStrays lists new strays in their chats' check-ins and takes out the
// ones a scan no longer finds. A server with several ports is listed once,
// by its first.
func (h *taskHub) syncStrays(known strayCheckIns, servers []Server, rest []Process) {
	found := map[detachedKey]strayItem{}
	for _, s := range servers {
		key := detachedKey{s.Process.PID, s.Process.Started}
		if _, ok := found[key]; !ok {
			work := strayWork(s.Process)
			work.name = fmt.Sprintf("%s · %d", s.Title(), s.Port)
			work.service = true
			found[key] = work
		}
	}
	for _, p := range rest {
		found[detachedKey{p.PID, p.Started}] = strayWork(p)
	}
	for key, remove := range known {
		if _, ok := found[key]; !ok {
			remove()
			delete(known, key)
		}
	}
	for key, work := range found {
		if _, ok := known[key]; ok || work.session == "" {
			continue
		}
		known[key] = h.addCheckIn(work.session, work.checkInWork)
	}
}

// strayItem is a stray as check-ins list it, with the chat it belongs to.
type strayItem struct {
	checkInWork
	session string
}

func strayWork(p Process) strayItem {
	return strayItem{session: p.Session, checkInWork: checkInWork{
		id: fmt.Sprintf("PID %d", p.PID), name: p.Command, started: p.Started, ctx: context.Background(),
		progress: func() string {
			return fmt.Sprintf("(Crush can't show its output: it was not started with crush bg. Stop it with kill %d if it is no longer needed.)", p.PID)
		},
	}}
}
