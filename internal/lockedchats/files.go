package lockedchats

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// copyFile copies src to dst (made with its folders), syncs it, and returns
// the copy's sha256, checked by reading the copy back.
func copyFile(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, sum), in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	want := hex.EncodeToString(sum.Sum(nil))
	got, err := fileSum(dst)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("the copy of %s does not match it", src)
	}
	return want, syncDir(filepath.Dir(dst))
}

// fileSum is a file's sha256.
func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// writeFileSync writes data to path through a temporary file, syncs both
// the file and its folder, and only then replaces path.
func writeFileSync(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir makes a folder's entries (a new or renamed file) durable.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

// overwrite writes random bytes over an open file's whole length and syncs
// them. On btrfs the old blocks are only freed, not rewritten (PLAN §6).
func overwrite(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	buf := make([]byte, 64*1024)
	for off := int64(0); off < size; off += int64(len(buf)) {
		n := int64(len(buf))
		if size-off < n {
			n = size - off
		}
		if _, err := rand.Read(buf[:n]); err != nil {
			return err
		}
		if _, err := f.WriteAt(buf[:n], off); err != nil {
			return err
		}
	}
	return f.Sync()
}

// wipeFile overwrites a file with random bytes, syncs, empties and removes
// it. A missing file is already wiped.
func wipeFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := overwrite(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// wipeDir wipes every file in a folder, then removes the folder.
func wipeDir(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range files {
		if err := wipeFile(path); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

// copyDir copies every regular file under src into dst and returns each
// copied file's path relative to src with its sha256.
func copyDir(src, dst string) (map[string]string, error) {
	sums := map[string]string{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		sum, err := copyFile(path, filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		sums[rel] = sum
		return nil
	})
	return sums, err
}

// rewriteShared replaces a file another program shares (a CLI's history)
// with the text change returns, then overwrites the old copy's blocks
// through a handle kept on it, so the replaced text is not left readable.
// A file that changed while it was read is read again.
func rewriteShared(path string, change func([]byte) ([]byte, bool, error)) error {
	for range 5 {
		old, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		before, err := old.Stat()
		if err != nil {
			old.Close()
			return err
		}
		data, err := io.ReadAll(old)
		if err != nil {
			old.Close()
			return err
		}
		next, changed, err := change(data)
		if err != nil || !changed {
			old.Close()
			return err
		}
		if now, err := os.Stat(path); err != nil || !os.SameFile(before, now) || now.Size() != before.Size() || !now.ModTime().Equal(before.ModTime()) {
			old.Close()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err := writeFileSyncMode(path, next, before.Mode().Perm()); err != nil {
			old.Close()
			return err
		}
		err = overwriteReplaced(old, path)
		old.Close()
		return err
	}
	return fmt.Errorf("%s kept changing while it was cleaned", path)
}

// overwriteReplaced overwrites the file old was opened on, but only when
// path is now another file (the old one was replaced), so a live file is
// never overwritten.
func overwriteReplaced(old *os.File, path string) error {
	was, err := old.Stat()
	if err != nil {
		return err
	}
	if now, err := os.Stat(path); err == nil && os.SameFile(was, now) {
		return nil
	}
	f, err := os.OpenFile("/proc/self/fd/"+fmt.Sprint(old.Fd()), os.O_WRONLY, 0)
	if err != nil {
		return nil // the old copy is gone already
	}
	defer f.Close()
	return overwrite(f)
}

func writeFileSyncMode(path string, data []byte, mode os.FileMode) error {
	if err := writeFileSync(path, data); err != nil {
		return err
	}
	if mode != 0 {
		return os.Chmod(path, mode)
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ensureDir(dir string) error { return os.MkdirAll(dir, 0o700) }
