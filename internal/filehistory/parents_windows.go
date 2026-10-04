package filehistory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func safeParents(path string) error {
	if err := safePath(path); err != nil {
		return err
	}
	volume := filepath.VolumeName(path) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return err
	}
	defer func() { root.Close() }()
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), volume), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		info, err := root.Lstat(part)
		if os.IsNotExist(err) {
			if err = root.Mkdir(part, 0755); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe restore parent")
		}
		child, err := root.OpenRoot(part)
		if err != nil {
			return err
		}
		root.Close()
		root = child
	}
	return nil
}
