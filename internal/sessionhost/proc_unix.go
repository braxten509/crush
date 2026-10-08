//go:build !windows

package sessionhost

import (
	"os"
	"syscall"
)

// terminate asks a session's Crush to exit. Crush's crash guard passes
// SIGTERM on to the Crush it watches, which saves and shuts down.
func terminate(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
}

// kill ends a session that didn't exit in time, with everything still in
// its terminal: the session leads its own process group.
func kill(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}
