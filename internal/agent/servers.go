package agent

import "slices"

// Server is a background process listening on a TCP port of this computer.
type Server struct {
	Port    int
	Process Process
}

// listeningPorts returns the ports the given processes listen on for
// connections from this computer. It is nil where sockets can't be read.
var listeningPorts = func(pids []int) []int { return nil }

// FindServers splits procs into the servers among them, one per port, and
// the rest.
func FindServers(procs []Process) (servers []Server, rest []Process) {
	if len(procs) == 0 {
		return nil, nil
	}
	all := markedProcs(hubMarkers())
	for _, p := range procs {
		var ports []int
		if pids := processTree(p, all); len(pids) > 0 {
			ports = listeningPorts(pids)
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
func processTree(p Process, procs map[int]proc) []int {
	var tree []int
	if p.JobID != "" {
		for pid, q := range procs {
			if p.marker != "" && q.managedJobID == p.marker {
				tree = append(tree, pid)
			}
		}
		return tree
	}
	if _, ok := procs[p.PID]; !ok {
		return nil
	}
	tree = []int{p.PID}
	for i := 0; i < len(tree); i++ {
		for _, q := range procs {
			if q.ppid == tree[i] {
				tree = append(tree, q.pid)
			}
		}
	}
	return tree
}
