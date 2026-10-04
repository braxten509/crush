package filehistory

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/google/uuid"
)

func safeParents(path string) error {
	target, err := openRestoreTarget(path, true)
	if err != nil {
		return err
	}
	target.close()
	return nil
}

func (t *restoreTarget) current(expected ...State) (State, []byte, error) {
	f, err := t.open()
	if os.IsNotExist(err) {
		return State{}, nil, nil
	}
	if err != nil {
		return State{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return State{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return State{}, nil, errors.New("path is no longer a regular file")
	}
	if info.Size() > filechange.MaxRestoreSize {
		return State{}, nil, errors.New("Too large to restore (over 10 MB)")
	}
	data, err := io.ReadAll(io.LimitReader(f, filechange.MaxRestoreSize+1))
	if err != nil {
		return State{}, nil, err
	}
	if len(data) > filechange.MaxRestoreSize {
		return State{}, nil, errors.New("Too large to restore (over 10 MB)")
	}
	value := State{Exists: true, Digest: hash(data), Mode: uint32(info.Mode())}
	if len(expected) > 0 && strings.HasPrefix(expected[0].Digest, "stat:") {
		value.Digest = filechange.StatDigest(info)
	}
	return value, data, nil
}

var errRestoreChanged = errors.New("file changed during restore")

// Replacement/unlink use the same pinned parent as both content checks.
// The final check detects ordinary concurrent edits; rename/unlink are not a
// compare-and-swap with another writer. Neither operation follows a leaf link.
func (t *restoreTarget) replace(data []byte, mode os.FileMode, before State) error {
	name := ".crush-restore-" + uuid.NewString()
	f, err := t.createTemp(name)
	if err != nil {
		return err
	}
	defer t.remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(mode.Perm() | mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if current, _, err := t.current(before); err != nil || !same(current, before) {
		return errRestoreChanged
	}
	if err := t.rename(name); err != nil {
		return err
	}
	return t.sync()
}

func (t *restoreTarget) delete(before State) error {
	if current, _, err := t.current(before); err != nil || !same(current, before) {
		return errRestoreChanged
	}
	if err := t.remove(t.name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return t.sync()
}
