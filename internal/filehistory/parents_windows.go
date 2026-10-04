package filehistory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows parent handles omit FILE_SHARE_DELETE. Once opened and checked,
// each component cannot be renamed/replaced until this target closes. Denying
// write sharing also blocks changing an open directory into a reparse point. The
// walk keeps ALL ancestors locked, including during missing-parent creation.
type restoreTarget struct {
	parents   []*os.File
	directory string
	name      string
}

func windowsOpen(path string, access uint32, share uint32) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		if err == nil {
			err = errors.New("path now contains a reparse point")
		}
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func openRestoreTarget(path string, create bool) (*restoreTarget, error) {
	return walkRestoreParents(path, create, nil)
}
func walkRestoreParents(path string, create bool, opened func(string)) (_ *restoreTarget, err error) {
	if err := safePath(path); err != nil {
		return nil, err
	}
	target := &restoreTarget{directory: filepath.Dir(path), name: filepath.Base(path)}
	defer func() {
		if err != nil {
			target.close()
		}
	}()
	volume := filepath.VolumeName(path) + string(filepath.Separator)
	current := volume
	parts := append([]string{""}, strings.Split(strings.TrimPrefix(target.directory, volume), string(filepath.Separator))...)
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, part)
		}
		f, openErr := windowsOpen(current, windows.GENERIC_READ, windows.FILE_SHARE_READ)
		if os.IsNotExist(openErr) && create {
			if err = os.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
				return nil, err
			}
			f, openErr = windowsOpen(current, windows.GENERIC_READ, windows.FILE_SHARE_READ)
		}
		if openErr != nil {
			return nil, openErr
		}
		target.parents = append(target.parents, f)
		info, statErr := f.Stat()
		if statErr != nil {
			return nil, statErr
		}
		if !info.IsDir() {
			return nil, errors.New("unsafe restore parent")
		}
		if opened != nil {
			opened(current)
		}
	}
	return target, nil
}
func (t *restoreTarget) close() {
	for i := len(t.parents) - 1; i >= 0; i-- {
		t.parents[i].Close()
	}
}
func (t *restoreTarget) open() (*os.File, error) {
	return windowsOpen(filepath.Join(t.directory, t.name), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
}
func (t *restoreTarget) createTemp(name string) (*os.File, error) {
	return os.OpenFile(filepath.Join(t.directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}
func (t *restoreTarget) rename(name string) error {
	return os.Rename(filepath.Join(t.directory, name), filepath.Join(t.directory, t.name))
}
func (t *restoreTarget) remove(name string) error {
	f, err := windowsOpen(filepath.Join(t.directory, name), windows.DELETE|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path is no longer a regular file")
	}
	disposition := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileDispositionInfo, &disposition, uint32(unsafe.Sizeof(disposition)))
}

// Windows does not support fsync on these read-only directory handles. The
// replacement file itself was flushed before rename.
func (t *restoreTarget) sync() error { return nil }
