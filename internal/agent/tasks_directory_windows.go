//go:build windows

package agent

import (
	"errors"
	"os"
)

func privateTaskBase(path string) error {
	// Windows ownership and ACL checks are not implemented: fail closed.
	return errors.New("private task directory ownership cannot be verified on Windows")
}

func privateTaskDirectory(info os.FileInfo) bool { return false }
func taskProcessAlive(pid int) bool              { return true }
