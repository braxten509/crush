//go:build darwin

package server

import (
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// socketPeerUID reads the peer's user from the kernel (LOCAL_PEERCRED).
func socketPeerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return -1, err
	}
	if credErr != nil {
		return -1, credErr
	}
	return int(cred.Uid), nil
}

// fileOwner reports the user that owns the file info describes.
func fileOwner(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, false
	}
	return int(stat.Uid), true
}
