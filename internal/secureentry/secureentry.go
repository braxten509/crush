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
	"unicode/utf8"
)

const maxFileSize = 8 << 20

// Spec is safe to transmit through the agent's request channel. A spec names
// either a prepared File with a placeholder, or a KDE Wallet entry.
type Spec struct {
	File        string      `json:"file"`
	Label       string      `json:"label"`
	Placeholder string      `json:"placeholder"`
	Occurrence  int         `json:"occurrence"`
	Wallet      *WalletSpec `json:"wallet,omitempty"`
}

// WalletSpec is a KDE Wallet entry a value is saved into (owner,
// 2026-11-24). An entry that already exists is refused unless Replace.
type WalletSpec struct {
	Folder  string `json:"folder"`
	Entry   string `json:"entry"`
	Replace bool   `json:"replace,omitempty"`
}

// Multiline reports whether the value may hold line breaks and tabs: only a
// wallet entry, which has no file format to break.
func (spec Spec) Multiline() bool { return spec.Wallet != nil }

// The only statuses that ever leave secure entry.
const (
	StatusSaved     = "saved"
	StatusCancelled = "cancelled"
)

// broker holds the single open secure question form.
var broker struct {
	sync.Mutex
	deliverForm func(*Form)
	pending     *Form
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

// normalize checks the metadata and fills in its defaults.
func (spec Spec) normalize() (Spec, error) {
	if spec.Wallet != nil {
		return spec.normalizeWallet()
	}
	if !filepath.IsAbs(spec.File) {
		return spec, errors.New("the destination must be an absolute path")
	}
	if strings.IndexFunc(spec.File+spec.Label+spec.Placeholder, unicode.IsControl) >= 0 {
		return spec, errors.New("entry metadata cannot contain control characters")
	}
	if spec.Placeholder == "" {
		spec.Placeholder = "%s"
	}
	if spec.Occurrence < 1 {
		return spec, errors.New("occurrence must be at least 1")
	}
	if spec.Label == "" {
		spec.Label = "API key"
	}
	spec.File = filepath.Clean(spec.File)
	return spec, nil
}

// findSlot returns the offset of the occurrence-th literal placeholder.
func findSlot(data []byte, placeholder string, occurrence int) (int, error) {
	offset := 0
	for range occurrence {
		index := bytes.Index(data[offset:], []byte(placeholder))
		if index < 0 {
			return 0, errors.New("the requested placeholder was not found")
		}
		offset += index + len(placeholder)
	}
	return offset - len(placeholder), nil
}

func Prepare(spec Spec) (*Target, error) {
	if spec.Wallet != nil {
		return nil, errors.New("a KDE Wallet entry needs a question form")
	}
	spec, err := spec.normalize()
	if err != nil {
		return nil, err
	}
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
	t.offset, err = findSlot(data, spec.Placeholder, spec.Occurrence)
	if err != nil {
		root.Close()
		return nil, err
	}
	t.fingerprint = sha256.Sum256(data)
	t.info = info
	return t, nil
}

func (t *Target) read() ([]byte, os.FileInfo, error) { return readFile(t.root, t.name) }

// readFile reads a regular file in root without following a final symlink.
// Callers clear the returned contents; errors never include them.
func readFile(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("destination must be an existing regular file, not a symlink")
	}
	file, err := root.Open(name)
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

// ErrClosed is returned once the entry was saved or cancelled.
var ErrClosed = errors.New("secure entry is closed")

// checkValue returns a fixed error for values that can't be saved. A
// multiline value (a wallet entry) may hold line breaks and tabs.
func checkValue(value []byte, multiline bool) error {
	if len(value) == 0 {
		return errors.New("enter a value before saving")
	}
	if len(value) > MaxValueSize {
		return errors.New("value exceeds 64 KiB")
	}
	if multiline {
		if !utf8.Valid(value) || bytes.ContainsRune(value, 0) {
			return errors.New("enter text without null characters")
		}
		return nil
	}
	if bytes.ContainsAny(value, "\x00\r\n") {
		return errors.New("enter a single-line value without control characters")
	}
	return nil
}

// MaxValueSize bounds an entered value.
const MaxValueSize = 64 << 10

// Save replaces exactly one literal placeholder and atomically installs a
// private (0600) file. Existing keys stay local and are never formatted/logged.
func (t *Target) Save(value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.root == nil {
		return ErrClosed
	}
	if err := checkValue(value, t.spec.Multiline()); err != nil {
		return err
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
