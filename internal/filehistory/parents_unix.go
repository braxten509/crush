//go:build !windows

package filehistory

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// A restoreTarget pins the validated parent until the final syscall. Never
// reopen its absolute path after validation: an ancestor can be renamed.
type restoreTarget struct {
	directory *os.File
	name      string
}

func openRestoreTarget(path string, create bool) (*restoreTarget, error) {
	return walkRestoreParents(path, create, nil)
}

func walkRestoreParents(path string, create bool, opened func(string)) (*restoreTarget, error) {
	if err := safePath(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if fd >= 0 {
			unix.Close(fd)
		}
	}()
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if os.IsNotExist(err) && create {
			if err := unix.Mkdirat(fd, part, 0755); err != nil && !os.IsExist(err) {
				return nil, err
			}
			next, err = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			return nil, err
		}
		unix.Close(fd)
		fd = next
		current = filepath.Join(current, part)
		if opened != nil {
			opened(current)
		}
	}
	target := &restoreTarget{directory: os.NewFile(uintptr(fd), filepath.Dir(path)), name: filepath.Base(path)}
	fd = -1
	return target, nil
}

func (t *restoreTarget) close() { t.directory.Close() }
func (t *restoreTarget) open() (*os.File, error) {
	// NONBLOCK prevents a raced-in pipe/device from blocking before Stat rejects it.
	fd, err := unix.Openat(int(t.directory.Fd()), t.name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), t.name), nil
}
func (t *restoreTarget) createTemp(name string) (*os.File, error) {
	fd, err := unix.Openat(int(t.directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
func (t *restoreTarget) rename(name string) error {
	return unix.Renameat(int(t.directory.Fd()), name, int(t.directory.Fd()), t.name)
}
func (t *restoreTarget) remove(name string) error {
	return unix.Unlinkat(int(t.directory.Fd()), name, 0)
}
func (t *restoreTarget) sync() error { return t.directory.Sync() }
