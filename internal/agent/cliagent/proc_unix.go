//go:build !windows

package cliagent

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
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

// killCommands kills the process groups of the CLI's direct children: the
// commands it runs, which some CLIs start in sessions of their own where
// killing the CLI's group doesn't reach them.
// ponytail: reads /proc, so Linux only; elsewhere the CLI must stop them.
func (p *proc) killCommands() {
	pid := p.cmd.Process.Pid
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Fields after the ")" closing the command name: state ppid pgrp ...
		fields := strings.Fields(string(data[bytes.LastIndexByte(data, ')')+1:]))
		if len(fields) < 3 || fields[1] != strconv.Itoa(pid) {
			continue
		}
		if pgrp, err := strconv.Atoi(fields[2]); err == nil && pgrp != pid {
			_ = syscall.Kill(-pgrp, syscall.SIGKILL)
		}
	}
}
