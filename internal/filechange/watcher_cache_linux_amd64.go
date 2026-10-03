//go:build linux && amd64

package filechange

import (
	"fmt"
	"os"
)

const maxWatchedThreads = 64

type watchedThread struct {
	start, parent string
	pid           int
	record        *invocation
	memory        *os.File
	used          uint64
}

// thread reuses process ancestry and the memory descriptor while checking
// the kernel start time on every event. PID reuse, reparenting and exec never
// inherit another process's attribution or an obsolete address space.
func (w *watcher) thread(id int) *watchedThread {
	fields := procStat(id)
	if len(fields) < 20 {
		w.dropThread(id)
		return nil
	}
	w.tick++
	if cached := w.threads[id]; cached != nil && cached.start == fields[19] && cached.parent == fields[1] {
		cached.used = w.tick
		return cached
	}
	w.dropThread(id)
	pid := threadGroup(id)
	if pid <= 0 || pid == w.crush {
		return nil
	}
	memory, err := os.Open(fmt.Sprintf("/proc/%d/mem", id))
	if err != nil {
		return nil
	}
	if w.threads == nil {
		w.threads = map[int]*watchedThread{}
	}
	if len(w.threads) >= maxWatchedThreads {
		oldest, used := 0, ^uint64(0)
		for thread, cached := range w.threads {
			if cached.used < used {
				oldest, used = thread, cached.used
			}
		}
		w.dropThread(oldest)
	}
	cached := &watchedThread{start: fields[19], parent: fields[1], pid: pid, record: w.record(pid), memory: memory, used: w.tick}
	w.threads[id] = cached
	return cached
}

func (w *watcher) dropThread(id int) {
	if cached := w.threads[id]; cached != nil {
		_ = cached.memory.Close()
		delete(w.threads, id)
	}
}

func (w *watcher) dropProcess(pid int) {
	for id, cached := range w.threads {
		if cached.pid == pid {
			w.dropThread(id)
		}
	}
}
