package agent

import (
	"bytes"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"os"
	"strconv"
	"strings"
	"sync"
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
		sid, _ := strconv.Atoi(fields[3])
		ticks, _ := strconv.ParseInt(fields[19], 10, 64)
		cmdline, _ := os.ReadFile(dir + "/cmdline")
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if len(cmdline) == 0 {
			continue // A zombie or kernel thread.
		}
		// ponytail: assumes USER_HZ is 100, true on every Linux in use.
		nativeShellID := ""
		for _, variable := range bytes.Split(env, []byte{0}) {
			if value, ok := strings.CutPrefix(string(variable), tools.NativeShellEnv+"="); ok {
				nativeShellID = value
				break
			}
		}
		procs[pid] = proc{pid: pid, ppid: ppid, sid: sid, args: args, started: boot.Add(time.Duration(ticks) * 10 * time.Millisecond), nativeShellID: nativeShellID}
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

var bootTime = sync.OnceValue(func() time.Time {
	// Uptime retains subsecond precision for matching command start times;
	// /proc/stat's btime truncates to a whole second. Cache it so process
	// identity checks see the same start time on successive scans.
	data, _ := os.ReadFile("/proc/uptime")
	up, _, _ := strings.Cut(string(data), " ")
	seconds, err := strconv.ParseFloat(up, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(seconds * float64(time.Second)))
})
