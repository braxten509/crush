// Package procs reads the operating system's process table: the processes
// carrying an environment marker, and the TCP ports processes listen on.
// Where the table can't be read (anything but Linux) it reports nothing.
package procs

import "time"

// Entry is one process from the process table.
type Entry struct {
	PID, PPID int
	// SID is the process's session ID.
	SID     int
	Args    []string
	Started time.Time
	// Env is the process's environment, one "KEY=value" per entry.
	Env [][]byte
}

// markedProcs and listeningPorts are replaced by the platform's readers.
var (
	markedProcs    = func(markers []string) map[int]Entry { return nil }
	listeningPorts = func(pids []int) []int { return nil }
)

// Marked lists the processes whose environment holds one of markers
// exactly. It is nil where the process table can't be read.
func Marked(markers []string) map[int]Entry { return markedProcs(markers) }

// ListeningPorts returns the ports the given processes listen on for
// connections from this computer. It is nil where sockets can't be read.
func ListeningPorts(pids []int) []int { return listeningPorts(pids) }
