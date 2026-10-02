// Package secureentry keeps secret entry local to the TUI and file writer.
// Requests and results contain metadata only; secrets never enter the broker.
package secureentry

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

const maxFileSize = 8 << 20

// Spec is safe to transmit through the agent's request channel.
type Spec struct {
	File        string `json:"file"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Occurrence  int    `json:"occurrence"`
}

// Request contains no file contents or entered value.
type Request struct {
	Spec
	Session string
	target  *Target
	done    chan string
	once    sync.Once
}

func (r *Request) String() string { return "secure-entry request" }

// Save writes locally and returns only a fixed, non-secret error.
func (r *Request) Save(value []byte) error { return r.target.Save(value) }

// Finish resolves the request with a fixed status, never arbitrary text.
func (r *Request) Finish(saved bool) {
	r.once.Do(func() {
		r.target.Close()
		status := "cancelled"
		if saved {
			status = "saved"
		}
		r.done <- status
	})
}

var broker struct {
	sync.Mutex
	deliver func(*Request)
	pending *Request
}

// Attach connects only the local TUI, bypassing shared app/remote events.
func Attach(deliver func(*Request)) func() {
	broker.Lock()
	broker.deliver = deliver
	broker.Unlock()
	return func() {
		broker.Lock()
		broker.deliver = nil
		pending := broker.pending
		broker.Unlock()
		if pending != nil {
			pending.Finish(false)
		}
	}
}

// Open reserves the single secure dialog. It fails without a local TUI.
func Open(session string, spec Spec) (<-chan string, error) {
	broker.Lock()
	defer broker.Unlock()
	if broker.deliver == nil {
		return nil, errors.New("secure entry requires the local Crush terminal")
	}
	if broker.pending != nil {
		return nil, errors.New("a secure entry is already open")
	}
	target, err := Prepare(spec)
	if err != nil {
		return nil, err
	}
	r := &Request{Spec: target.spec, Session: session, target: target, done: make(chan string, 1)}
	broker.pending = r
	result := make(chan string, 1)
	go func() {
		status := <-r.done
		broker.Lock()
		if broker.pending == r {
			broker.pending = nil
		}
		broker.Unlock()
		result <- status
	}()
	go broker.deliver(r)
	return result, nil
}

// Target retains only a fingerprint and offset, never the template contents.
type Target struct {
	mu          sync.Mutex
	spec        Spec
	root        *os.Root
	name        string
	fingerprint [32]byte
	info        os.FileInfo
	offset      int
}

func (t *Target) String() string { return "secure-entry target" }

func Prepare(spec Spec) (*Target, error) {
	if !filepath.IsAbs(spec.File) {
		return nil, errors.New("the destination must be an absolute path")
	}
	if strings.IndexFunc(spec.File+spec.Label+spec.Placeholder, unicode.IsControl) >= 0 {
		return nil, errors.New("entry metadata cannot contain control characters")
	}
	if spec.Placeholder == "" {
		spec.Placeholder = "%s"
	}
	if spec.Occurrence < 1 {
		return nil, errors.New("occurrence must be at least 1")
	}
	if spec.Label == "" {
		spec.Label = "API key"
	}
	spec.File = filepath.Clean(spec.File)
	// Register before the dialog can save, including for existing trackers.
	markSensitive(spec.File)
	root, err := os.OpenRoot(filepath.Dir(spec.File))
	if err != nil {
		return nil, errors.New("cannot open destination directory")
	}
	t := &Target{spec: spec, root: root, name: filepath.Base(spec.File)}
	data, info, err := t.read()
	if err != nil {
		root.Close()
		return nil, err
	}
	defer clear(data)
	offset := 0
	for range spec.Occurrence {
		index := bytes.Index(data[offset:], []byte(spec.Placeholder))
		if index < 0 {
			root.Close()
			return nil, errors.New("the requested placeholder was not found")
		}
		offset += index + len(spec.Placeholder)
	}
	t.offset = offset - len(spec.Placeholder)
	t.fingerprint = sha256.Sum256(data)
	t.info = info
	return t, nil
}

func (t *Target) read() ([]byte, os.FileInfo, error) {
	info, err := t.root.Lstat(t.name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("destination must be an existing regular file, not a symlink")
	}
	file, err := t.root.Open(t.name)
	if err != nil {
		return nil, nil, errors.New("cannot open destination file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil, errors.New("destination changed; open secure entry again")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil || len(data) > maxFileSize {
		clear(data)
		return nil, nil, errors.New("cannot read destination, or file exceeds 8 MiB")
	}
	return data, info, nil
}

func (t *Target) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.root != nil {
		t.root.Close()
		t.root = nil
	}
}

// Save replaces exactly one literal placeholder and atomically installs a
// private (0600) file. Existing keys stay local and are never formatted/logged.
func (t *Target) Save(value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.root == nil {
		return errors.New("secure entry is closed")
	}
	if len(value) == 0 {
		return errors.New("enter a value before saving")
	}
	if len(value) > 64<<10 {
		return errors.New("value exceeds 64 KiB")
	}
	if bytes.ContainsAny(value, "\x00\r\n") {
		return errors.New("enter a single-line value without control characters")
	}
	data, info, err := t.read()
	if err != nil {
		return err
	}
	defer clear(data)
	if !os.SameFile(t.info, info) || sha256.Sum256(data) != t.fingerprint {
		return errors.New("destination changed; cancel and open secure entry again")
	}
	// Create in the pinned directory, so rename cannot cross filesystems.
	name := ".crush-secure-" + randomName()
	file, err := t.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create private destination file")
	}
	defer t.root.Remove(name)
	defer file.Close()
	for _, part := range [][]byte{data[:t.offset], value, data[t.offset+len(t.spec.Placeholder):]} {
		if _, err := file.Write(part); err != nil {
			return errors.New("cannot write destination file")
		}
	}
	if file.Sync() != nil || file.Close() != nil {
		return errors.New("cannot finish writing destination file")
	}
	// Detect edits made during the write as well as while entering the key.
	current, currentInfo, err := t.read()
	if err != nil {
		return err
	}
	defer clear(current)
	if !os.SameFile(info, currentInfo) || sha256.Sum256(current) != t.fingerprint {
		return errors.New("destination changed; cancel and open secure entry again")
	}
	if t.root.Rename(name, t.name) != nil {
		return errors.New("cannot replace destination file")
	}
	return nil
}
