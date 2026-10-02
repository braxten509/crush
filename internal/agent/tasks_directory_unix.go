//go:build !windows

package agent

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func privateTaskBase(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !privateTaskDirectory(info) {
		return fmt.Errorf("task base must be a private directory owned by the current user")
	}
	return nil
}

func privateTaskDirectory(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o077 == 0 && stat.Uid == uint32(os.Getuid())
}

func taskProcessAlive(pid int) bool {
	// Permission errors and unknown failures must preserve the directory.
	return !errors.Is(unix.Kill(pid, 0), unix.ESRCH)
}
