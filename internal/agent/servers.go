package agent

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/procs"
)

// Server is a background process listening on a TCP port of this computer.
type Server struct {
	Port    int
	Process Process
}

// Title names the server: its job's name, or else the program it runs.
func (s Server) Title() string {
	if s.Process.Name != "" {
		return s.Process.Name
	}
	for field := range strings.FieldsSeq(s.Process.Command) {
		if !strings.Contains(field, "=") {
			return filepath.Base(field)
		}
	}
	return ""
}

// FindServers splits processes into the servers among them, one per port,
// and the rest.
func FindServers(processes []Process) (servers []Server, rest []Process) {
	if len(processes) == 0 {
		return nil, nil
	}
	all := markedProcs(hubMarkers())
	for _, p := range processes {
		var ports []int
		if pids := processTree(p, all); len(pids) > 0 {
			ports = procs.ListeningPorts(pids)
		}
		for _, port := range ports {
			if !slices.ContainsFunc(servers, func(s Server) bool { return s.Port == port }) {
				servers = append(servers, Server{Port: port, Process: p})
			}
		}
		if len(ports) == 0 {
			rest = append(rest, p)
		}
	}
	return servers, rest
}

// processTree returns p's processes: a managed job's are those carrying its
// marker; any other's are p and everything it started.
func processTree(p Process, table map[int]proc) []int {
	var tree []int
	if p.JobID != "" {
		for pid, q := range table {
			if p.marker != "" && q.managedJobID == p.marker {
				tree = append(tree, pid)
			}
		}
		return tree
	}
	if _, ok := table[p.PID]; !ok {
		return nil
	}
	tree = []int{p.PID}
	for i := 0; i < len(tree); i++ {
		for _, q := range table {
			if q.ppid == tree[i] {
				tree = append(tree, q.pid)
			}
		}
	}
	return tree
}
