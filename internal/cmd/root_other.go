//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"
)

func detachProcess(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
}

// relaunch replaces this process with a fresh Crush started in dir.
func relaunch(dir string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return syscall.Exec(exe, append([]string{exe}, args...), os.Environ())
}
