package filechange

import (
	"io/fs"
	"sync"
)

// A command and all its subprocesses share one preview allocation budget.
// Immutable content is interned so repeated baselines retain one copy.
type snapshotStore struct {
	mutex            sync.Mutex
	remaining        int
	restoreRemaining int
	text             map[string]string
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{remaining: maxTextTotal, restoreRemaining: MaxRestoreTotal, text: map[string]string{}}
}

func (s *snapshotStore) read(path string, info fs.FileInfo) entry {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	budget := maxTextSize
	value := readEntry(path, info, &budget, &s.restoreRemaining)
	if value.state.Content == "" {
		return value
	}
	if content, ok := s.text[value.state.Digest]; ok {
		value.state.Content = content
	} else if len(value.state.Content) <= s.remaining {
		s.text[value.state.Digest] = value.state.Content
		s.remaining -= len(value.state.Content)
	} else {
		value.state.Content = ""
		value.state.Omitted = "Text preview unavailable (file review memory limit)"
	}
	return value
}
