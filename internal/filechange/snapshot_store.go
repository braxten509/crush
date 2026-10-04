package filechange

import (
	"io/fs"
	"os"
	"sync"

	"github.com/charmbracelet/crush/internal/secureentry"
)

// Pending invocations share a bounded pool. Captures belong to their trackers,
// including invocations whose tool call has not been reported yet. Only the
// owners handed off by End (or by the native shell's Finish) release capacity.
type snapshotStore struct {
	mutex            sync.Mutex
	closed           bool
	remaining        int
	restoreRemaining int
	text             map[string]string
	captures         map[string][]*snapshotCapture
}

type snapshotCapture struct {
	value  entry
	bytes  int
	owners map[*Tracker]bool
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{remaining: maxTextTotal, restoreRemaining: MaxRestoreTotal, text: map[string]string{}, captures: map[string][]*snapshotCapture{}}
}

func (s *snapshotStore) read(owner *Tracker, path string, info fs.FileInfo) entry {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return entry{info: info, state: State{Size: info.Size(), Mode: uint32(info.Mode()), Digest: StatDigest(info), Omitted: "File review already finished", RestoreOmitted: "File review already finished"}}
	}
	var value entry
	if !secureentry.ReviewFile(path, func() { value = s.readLocked(owner, path, info) }) {
		return entry{info: info, state: State{Omitted: "Secure entry destination"}}
	}
	return value
}

func (s *snapshotStore) readLocked(owner *Tracker, path string, info fs.FileInfo) entry {
	for _, capture := range s.captures[path] {
		previous := capture.value
		if previous.state.RestoreOmitted == "" && os.SameFile(previous.info, info) && previous.info.Size() == info.Size() && previous.info.Mode() == info.Mode() && previous.info.ModTime().Equal(info.ModTime()) && previous.changeTime == changeTime(info) {
			capture.owners[owner] = true
			return previous
		}
	}
	budget := maxTextSize
	before := s.restoreRemaining
	value := readReviewEntry(path, info, &budget, &s.restoreRemaining)
	if value.state.Content != "" {
		if content, ok := s.text[value.state.Digest]; ok {
			value.state.Content = content
		} else if len(value.state.Content) <= s.remaining {
			s.text[value.state.Digest] = value.state.Content
			s.remaining -= len(value.state.Content)
		} else {
			value.state.Content = ""
			value.state.Omitted = "Text preview unavailable (file review memory limit)"
		}
	}
	value = spillRestore(value)
	// Failed restore captures still own preview text, but cannot be reused:
	// a later capture may have room, or the file may become readable.
	if value.restore == nil {
		s.restoreRemaining = before
	}
	s.captures[path] = append(s.captures[path], &snapshotCapture{value: value, bytes: before - s.restoreRemaining, owners: map[*Tracker]bool{owner: true}})
	return value
}

func (s *snapshotStore) release(owners ...*Tracker) { s.releaseCaptures(false, owners...) }

func (s *snapshotStore) releaseAll() { s.releaseCaptures(true) }

func (s *snapshotStore) releaseCaptures(all bool, owners ...*Tracker) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if all {
		s.closed = true
	}
	for path, captures := range s.captures {
		kept := captures[:0]
		for _, capture := range captures {
			for _, owner := range owners {
				delete(capture.owners, owner)
			}
			if all || len(capture.owners) == 0 {
				s.restoreRemaining += capture.bytes
			} else {
				kept = append(kept, capture)
			}
		}
		clear(captures[len(kept):])
		if len(kept) == 0 {
			delete(s.captures, path)
		} else {
			s.captures[path] = kept
		}
	}
	// Rebuild the preview intern table from pending captures as well.
	s.text = map[string]string{}
	s.remaining = maxTextTotal
	for _, captures := range s.captures {
		for _, capture := range captures {
			state := capture.value.state
			if _, ok := s.text[state.Digest]; !ok && state.Content != "" {
				s.text[state.Digest] = state.Content
				s.remaining -= len(state.Content)
			}
		}
	}
}
