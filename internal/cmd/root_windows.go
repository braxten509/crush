//go:build windows
// +build windows

package cmd

import (
	"os"
	"os/exec"
)

// relaunch runs a fresh Crush in dir and exits with its status, since
// Windows cannot replace the running process.
func relaunch(dir string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, args...)
	c.Dir, c.Stdin, c.Stdout, c.Stderr = dir, os.Stdin, os.Stdout, os.Stderr
	err = c.Run()
	os.Exit(c.ProcessState.ExitCode())
	return err
}
