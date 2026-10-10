package agent

import (
	"os"
	"slices"
	"strconv"
	"strings"
)

func init() { listeningPorts = linuxListeningPorts }

// linuxListeningPorts matches the processes' open sockets against the
// listening sockets in /proc/net.
func linuxListeningPorts(pids []int) []int {
	inodes := map[string]bool{}
	for _, pid := range pids {
		dir := "/proc/" + strconv.Itoa(pid) + "/fd/"
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			link, err := os.Readlink(dir + e.Name())
			if err != nil {
				continue
			}
			if inode, ok := strings.CutPrefix(link, "socket:["); ok {
				inodes[strings.TrimSuffix(inode, "]")] = true
			}
		}
	}
	if len(inodes) == 0 {
		return nil
	}
	var ports []int
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, port := range listeningInTable(string(data), inodes) {
			if !slices.Contains(ports, port) {
				ports = append(ports, port)
			}
		}
	}
	slices.Sort(ports)
	return ports
}

// listeningInTable reads a /proc/net/tcp table for the ports of listening
// sockets among inodes that take connections from this computer: bound to
// every address or to a loopback one.
func listeningInTable(table string, inodes map[string]bool) []int {
	const listen = "0A"
	var ports []int
	for _, line := range strings.Split(table, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != listen || !inodes[fields[9]] {
			continue
		}
		address, portHex, ok := strings.Cut(fields[1], ":")
		if !ok || !localAddress(address) {
			continue
		}
		if port, err := strconv.ParseInt(portHex, 16, 32); err == nil {
			ports = append(ports, int(port))
		}
	}
	return ports
}

// localAddress reports whether a hex address from /proc/net/tcp, as the
// kernel writes it (32-bit words in host order), is every address or a
// loopback one.
func localAddress(hex string) bool {
	switch hex {
	case "00000000", // 0.0.0.0
		"00000000000000000000000000000000", // ::
		"00000000000000000000000001000000": // ::1
		return true
	}
	switch len(hex) {
	case 8: // 127.x.x.x; the first byte is last in little-endian order.
		return strings.HasSuffix(hex, "7F")
	case 32: // ::ffff:127.x.x.x
		return hex[:24] == "0000000000000000FFFF0000" && strings.HasSuffix(hex, "7F")
	}
	return false
}
