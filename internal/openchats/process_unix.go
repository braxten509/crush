//go:build !windows

package openchats

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// lock takes an exclusive lock on path until unlock.
func lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// processStart is field 22 of /proc/PID/stat (the start time in clock
// ticks after boot), or "" where there is no /proc.
func processStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name in parentheses may hold spaces; fields follow it.
	s := string(b)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(s[end+1:])
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// alive reports whether pid runs and, when start is known, is the same
// process that signed in.
func alive(pid int, start string) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if start == "" {
		return true
	}
	now := processStart(pid)
	return now == "" || now == start
}
