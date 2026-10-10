package agent

import (
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The reaper stops what a Crush started when that Crush dies without its
// shutdown: a crash, a force-kill, or anything else that skips it. It is a
// small copy of Crush holding the read end of a pipe whose write end only
// Crush holds (the write end is close-on-exec, so nothing Crush starts
// inherits it). The pipe reads end-of-file once Crush is gone, however it
// ended; the reaper then stops every process still carrying the hub's
// marker, which everything the hub's agents start inherits. After a normal
// shutdown there is usually nothing left to stop.
const reaperArgument = "__crush-reaper"

// reapGrace is how long processes get to exit after SIGTERM before the
// reaper kills what is left.
const reapGrace = 3 * time.Second

func init() {
	if len(os.Args) == 3 && os.Args[1] == reaperArgument {
		_, _ = io.Copy(io.Discard, os.NewFile(3, "owner"))
		reap(os.Args[2])
		os.Exit(0)
	}
}

// startReaper starts the reaper for marker and returns the pipe's write end.
// The caller must keep it referenced for as long as it lives: closing it,
// or letting it be garbage collected, starts the reaping.
func startReaper(marker string) (*os.File, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer read.Close()
	cmd := exec.Command("/proc/self/exe", reaperArgument, marker)
	// No marker in its own environment, so it never stops itself and isn't
	// listed as background work.
	cmd.Env = []string{}
	cmd.ExtraFiles = []*os.File{read}
	// Its own session keeps the terminal's signals, and the session list's
	// group kill, away from it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		write.Close()
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return write, nil
}

// reap asks every process carrying marker to exit, then kills whatever is
// still there, including anything started in the meantime. Services D-Bus
// started on demand are left alone, as in the background list: they carry
// the marker but belong to the desktop.
func reap(marker string) {
	markers := []string{marker}
	running := func() map[int]proc {
		procs := markedProcs(markers)
		for pid, p := range procs {
			if p.busService {
				delete(procs, pid)
			}
		}
		return procs
	}
	procs := running()
	for pid := range procs {
		signal(pid, syscall.SIGTERM)
	}
	for deadline := time.Now().Add(reapGrace); len(procs) > 0 && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
		procs = running()
	}
	for pid := range running() {
		signal(pid, syscall.SIGKILL)
	}
}
