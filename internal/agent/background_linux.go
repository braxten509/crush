package agent

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() { markedProcs = linuxMarkedProcs }

// linuxMarkedProcs reads the process table from /proc.
func linuxMarkedProcs(markers []string) map[int]proc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	boot := bootTime()
	procs := map[int]proc{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := "/proc/" + e.Name()
		env, err := os.ReadFile(dir + "/environ")
		if err != nil || !hasMarker(env, markers) {
			continue
		}
		stat, err := os.ReadFile(dir + "/stat")
		if err != nil {
			continue
		}
		// Fields after the command name, which may hold spaces and parens.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		ticks, _ := strconv.ParseInt(fields[19], 10, 64)
		cmdline, _ := os.ReadFile(dir + "/cmdline")
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if len(cmdline) == 0 {
			continue // A zombie or kernel thread.
		}
		// ponytail: assumes USER_HZ is 100, true on every Linux in use.
		procs[pid] = proc{pid: pid, ppid: ppid, args: args, started: boot.Add(time.Duration(ticks) * 10 * time.Millisecond)}
	}
	return procs
}

func hasMarker(env []byte, markers []string) bool {
	for _, v := range bytes.Split(env, []byte{0}) {
		for _, m := range markers {
			if string(v) == m {
				return true
			}
		}
	}
	return false
}

func bootTime() time.Time {
	data, _ := os.ReadFile("/proc/stat")
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return time.Unix(sec, 0)
		}
	}
	return time.Time{}
}
