//go:build !windows

package cliagent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ownGroup starts the CLI in its own session: its own process group, so
// stopping it also stops the commands it launched, and no controlling
// terminal, so it can't draw over Crush and runs as it would headless
// (AGY, for one, skips stopping its commands on SIGINT when it has a TTY).
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func (p *proc) kill() {
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// killCommands kills the process groups of the CLI's direct children that
// run one of the given calls: the commands a turn waits on, which some CLIs
// start in sessions of their own where killing the CLI's group doesn't reach
// them. A child runs a call when it or a process under it has the command in
// its arguments and started with the call or after (half a second covers a
// command starting just before its call is reported).
// ponytail: reads /proc, so Linux only; elsewhere the CLI must stop them.
func (p *proc) killCommands(calls []openCall) {
	if len(calls) == 0 {
		return
	}
	boot := bootTime()
	if boot.IsZero() {
		return
	}
	type info struct {
		ppid, pgrp int
		started    time.Time
		args       []string
	}
	procs := map[int]info{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Fields after the ")" closing the command name: state ppid pgrp ...
		fields := strings.Fields(string(data[bytes.LastIndexByte(data, ')')+1:]))
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		pgrp, _ := strconv.Atoi(fields[2])
		ticks, _ := strconv.ParseInt(fields[19], 10, 64)
		cmdline, _ := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		// ponytail: assumes USER_HZ is 100, true on every Linux in use.
		procs[pid] = info{ppid, pgrp, boot.Add(time.Duration(ticks) * 10 * time.Millisecond), strings.Split(string(cmdline), "\x00")}
	}
	runs := func(pid int, c openCall) bool {
		for {
			q, ok := procs[pid]
			if !ok || q.started.Before(c.at.Add(-500*time.Millisecond)) {
				return false
			}
			if c.runBy(q.args) {
				return true
			}
			if q.ppid == p.cmd.Process.Pid {
				return false
			}
			pid = q.ppid
		}
	}
	cli := p.cmd.Process.Pid
	for pid, q := range procs {
		for _, c := range calls {
			if !runs(pid, c) {
				continue
			}
			// Walk up to the CLI's direct child and end its group.
			for q.ppid != cli {
				pid = q.ppid
				if q = procs[pid]; q.ppid == 0 {
					break
				}
			}
			if q.ppid == cli && q.pgrp != cli {
				_ = syscall.Kill(-q.pgrp, syscall.SIGKILL)
			}
			break
		}
	}
}

// runBy reports whether a process with these arguments runs the call: as a
// shell's script, or run directly with the words the shell would give it.
func (c openCall) runBy(args []string) bool {
	if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, c.command) }) {
		return true
	}
	if len(c.words) == 0 || len(args) < len(c.words) {
		return false
	}
	return filepath.Base(args[0]) == filepath.Base(c.words[0]) && slices.Equal(args[1:len(c.words)], c.words[1:])
}

// bootTime is when the system started, from /proc/uptime: to the hundredth
// of a second, where /proc/stat's btime is to the second.
func bootTime() time.Time {
	data, _ := os.ReadFile("/proc/uptime")
	up, _, _ := strings.Cut(string(data), " ")
	sec, err := strconv.ParseFloat(up, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(sec * float64(time.Second)))
}
