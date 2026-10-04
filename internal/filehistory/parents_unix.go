//go:build !windows

package filehistory

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

// Walk from the filesystem root using directory descriptors. O_NOFOLLOW
// rejects every symlink, including a parent swapped after safePath returned.
func safeParents(path string) error {
	if err := safePath(path); err != nil {
		return err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { unix.Close(fd) }()
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if os.IsNotExist(err) {
			if err := unix.Mkdirat(fd, part, 0755); err != nil && !os.IsExist(err) {
				return err
			}
			next, err = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			return err
		}
		unix.Close(fd)
		fd = next
	}
	return nil
}
