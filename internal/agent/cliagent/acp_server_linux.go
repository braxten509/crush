//go:build linux

package cliagent

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// openCodeSteerServer finds the local server an ACP turn starts:
// `opencode acp` launches `opencode serve --stdio`, whose port is its
// listening socket and whose random password is in its environment. The
// values stay in memory and are never logged.
func openCodeSteerServer(p *proc) (string, string) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return "", ""
	}
	pid := p.cmd.Process.Pid
	children, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	if err != nil {
		return "", ""
	}
	for _, field := range strings.Fields(string(children)) {
		child, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", field, "cmdline"))
		if err != nil || !bytes.Contains(cmdline, []byte("serve")) || !bytes.Contains(cmdline, []byte("--stdio")) {
			continue
		}
		password := procEnv(child, "OPENCODE_PASSWORD")
		if password == "" {
			password = procEnv(child, "OPENCODE_SERVER_PASSWORD")
		}
		if password == "" {
			continue
		}
		port := listeningPort(child)
		if port == 0 {
			continue
		}
		return "http://127.0.0.1:" + strconv.Itoa(port), password
	}
	return "", ""
}

// procEnv reads one value from a process's environment.
func procEnv(pid int, key string) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, entry := range strings.Split(string(data), "\x00") {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// listeningPort returns the port of pid's listening TCP socket, or 0.
func listeningPort(pid int) int {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(dir, fd.Name()))
		if err != nil || !strings.HasPrefix(link, "socket:[") {
			continue
		}
		inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
	}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" || !inodes[fields[9]] {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			port, err := strconv.ParseInt(parts[1], 16, 32)
			if err == nil && port > 0 {
				return int(port)
			}
		}
	}
	return 0
}
