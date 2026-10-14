// Package memlimit keeps every Crush window, and everything they start, in
// one shared memory box: a systemd user slice capped at the user's limit.
// When the box is full, Linux stops the biggest program in it, usually a
// test or build, instead of the system closing the whole terminal tab.
package memlimit

import (
	"bufio"
	"io"
	"strings"
)

// Slice is the shared box every Crush window joins.
const Slice = "crush.slice"

// Split divides a limit between RAM and swap so the two together never pass
// it. Swap is RAM too on zram systems, so a RAM-only cap would let Crush
// keep growing into swap. A limit of 0 means no limit.
func Split(limit uint64) (ram, swap uint64, limited bool) {
	if limit == 0 {
		return 0, 0, false
	}
	swap = limit / 5
	return limit - swap, swap, true
}

// cgroupPath returns the cgroup v2 path in /proc/self/cgroup's text.
func cgroupPath(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "0::"); ok {
			return rest
		}
	}
	return ""
}

// slicePath returns the path of slice within a process's cgroup path, or
// "" when the process isn't in it.
func slicePath(cgroup, slice string) string {
	before, _, ok := strings.Cut(cgroup, "/"+slice+"/")
	if !ok {
		return ""
	}
	return before + "/" + slice
}
