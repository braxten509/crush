package secureentry

import (
	"path/filepath"
	"sync"
)

var sensitiveFiles = struct {
	sync.RWMutex
	paths map[string]bool
}{paths: map[string]bool{}}

func sensitivePath(path string) string {
	path, _ = filepath.Abs(path)
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	return filepath.Clean(path)
}

func markSensitive(path string) {
	path = sensitivePath(path)
	sensitiveFiles.Lock()
	defer sensitiveFiles.Unlock()
	sensitiveFiles.paths[path] = true
}

// Sensitive reports destinations reserved for local secret entry. They remain
// sensitive after the dialog closes, including across later agent turns.
func Sensitive(path string) bool {
	path = sensitivePath(path)
	sensitiveFiles.RLock()
	defer sensitiveFiles.RUnlock()
	return sensitiveFiles.paths[path]
}

// ReviewFile serializes snapshot reads against secret registration. Once a
// destination is registered, no file-review path can read its contents.
func ReviewFile(path string, capture func()) bool {
	path = sensitivePath(path)
	sensitiveFiles.RLock()
	defer sensitiveFiles.RUnlock()
	if sensitiveFiles.paths[path] {
		return false
	}
	capture()
	return true
}
