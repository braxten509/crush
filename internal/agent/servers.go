package agent

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/procs"
)

// Server is a background process listening on a TCP port of this computer.
type Server struct {
	Port    int
	Process Process
	// Owner is the program holding the port, which may be one Process
	// started rather than Process itself.
	Owner string
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
		var owners map[int]int
		if pids := processTree(p, all); len(pids) > 0 {
			owners = procs.ListeningPorts(pids)
		}
		ports := slices.Sorted(maps.Keys(owners))
		for _, port := range ports {
			if !slices.ContainsFunc(servers, func(s Server) bool { return s.Port == port }) {
				var owner string
				if args := all[owners[port]].args; len(args) > 0 {
					owner = filepath.Base(args[0])
				}
				servers = append(servers, Server{Port: port, Process: p, Owner: owner})
			}
		}
		if len(ports) == 0 {
			rest = append(rest, p)
		}
	}
	return servers, rest
}

// processTree returns p's processes: a job's are those carrying its marker,
// as a managed job or as a native shell job; any other's are p and
// everything it started.
func processTree(p Process, table map[int]proc) []int {
	var tree []int
	if p.JobID != "" {
		for pid, q := range table {
			if p.marker != "" && (q.managedJobID == p.marker || q.nativeShellID == p.marker) {
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
