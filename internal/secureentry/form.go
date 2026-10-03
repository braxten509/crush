package secureentry

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/charmbracelet/crush/internal/question"
)

// Form is a question form with secure fields, from `crush ask`. It reaches
// only the local TUI, never the question service or remote clients. The
// form holds metadata only: values stay in the TUI's field buffers until
// the final Submit writes them to their files.
type Form struct {
	Session string
	// Request holds the questions, secure ones included, as metadata.
	Request question.Request

	mu           sync.Mutex
	fields       map[string]*Field
	groups       []*fileGroup
	closed       bool
	done         chan FormResult
	closedSignal chan struct{}
}

// FormResult carries the ordinary answers and a fixed status per secure
// question. It never carries a value.
type FormResult struct {
	// Answers holds every question's answer, in order; a secure question's
	// answer has only its QuestionID.
	Answers []question.Answer
	// Statuses maps each secure question ID to StatusSaved or
	// StatusCancelled.
	Statuses  map[string]string
	Cancelled bool
}

// Field is one prepared secure slot in a form.
type Field struct {
	Spec
	QuestionID string

	form   *Form
	group  *fileGroup
	offset int
	saved  bool
}

func (*Field) String() string { return "secure-entry field" }

// Saved reports whether this field's value was written to its file.
func (f *Field) Saved() bool {
	f.form.mu.Lock()
	defer f.form.mu.Unlock()
	return f.saved
}

func (f *Field) Available() bool {
	f.form.mu.Lock()
	defer f.form.mu.Unlock()
	return !f.form.closed && !f.group.written
}

// fileGroup is every slot of one destination file, written in one go so
// several keys in a file are saved together.
type fileGroup struct {
	root        *os.Root
	name        string
	fingerprint [32]byte
	info        os.FileInfo
	fields      []*Field // by offset
	written     bool
	staged      string
}

// FieldError names the secure question a fixed save error belongs to.
type FieldError struct {
	QuestionID string
	Err        error
}

func (e *FieldError) Error() string { return e.Err.Error() }
func (e *FieldError) Unwrap() error { return e.Err }

var errChanged = errors.New("destination changed; cancel and ask again")

func (*Form) String() string { return "secure question form" }

func (f *Form) abort() { f.Cancel() }

// AttachForms connects the local TUI for secure question forms.
func AttachForms(deliver func(*Form)) func() {
	broker.Lock()
	broker.deliverForm = deliver
	broker.Unlock()
	return func() {
		broker.Lock()
		broker.deliverForm = nil
		pending := broker.pending
		broker.Unlock()
		if form, ok := pending.(*Form); ok {
			form.Cancel()
		}
	}
}

// OpenForm prepares every secure slot, then shows the form in the local TUI.
// specs maps each secure question ID to its metadata. Errors are fixed text
// and never include file contents.
func OpenForm(session string, req question.Request, specs map[string]Spec) (*Form, error) {
	broker.Lock()
	defer broker.Unlock()
	if broker.deliverForm == nil {
		return nil, errors.New("secure entry requires the local Crush terminal")
	}
	if broker.pending != nil {
		return nil, errors.New("a secure entry is already open")
	}
	form, err := prepareForm(session, req, specs)
	if err != nil {
		return nil, err
	}
	broker.pending = form
	go broker.deliverForm(form)
	return form, nil
}

func prepareForm(session string, req question.Request, specs map[string]Spec) (*Form, error) {
	form := &Form{Session: session, Request: req, fields: map[string]*Field{}, done: make(chan FormResult, 1), closedSignal: make(chan struct{})}
	ready := false
	defer func() {
		if !ready {
			form.closeFiles()
		}
	}()
	groups := map[string]*fileGroup{}
	for _, q := range req.Questions {
		if q.Type != question.TypeSecureEntry {
			continue
		}
		spec, ok := specs[q.ID]
		if !ok {
			return nil, errors.New("a secure_entry question has no destination")
		}
		spec, err := spec.normalize()
		if err != nil {
			return nil, err
		}
		group := groups[spec.File]
		if group == nil {
			// Register before the form can save, including for existing trackers.
			markSensitive(spec.File)
			root, err := os.OpenRoot(filepath.Dir(spec.File))
			if err != nil {
				return nil, errors.New("cannot open destination directory")
			}
			group = &fileGroup{root: root, name: filepath.Base(spec.File)}
			groups[spec.File] = group
			form.groups = append(form.groups, group)
		}
		field := &Field{Spec: spec, QuestionID: q.ID, form: form, group: group}
		group.fields = append(group.fields, field)
		form.fields[q.ID] = field
	}
	if len(form.fields) == 0 || len(form.fields) != len(specs) {
		return nil, errors.New("secure entry metadata does not match the questions")
	}
	for i, group := range form.groups {
		if err := group.locate(); err != nil {
			return nil, err
		}
		for _, other := range form.groups[:i] {
			if os.SameFile(other.info, group.info) {
				return nil, errors.New("give every secure entry in one file the same path")
			}
		}
	}
	ready = true
	return form, nil
}

// locate fingerprints the file and finds every slot in its contents as
// prepared, before anything is written.
func (g *fileGroup) locate() error {
	data, info, err := readFile(g.root, g.name)
	if err != nil {
		return err
	}
	defer clear(data)
	for _, field := range g.fields {
		if field.offset, err = findSlot(data, field.Placeholder, field.Occurrence); err != nil {
			return err
		}
	}
	slices.SortFunc(g.fields, func(a, b *Field) int { return a.offset - b.offset })
	for i := 1; i < len(g.fields); i++ {
		previous := g.fields[i-1]
		if g.fields[i].offset < previous.offset+len(previous.Placeholder) {
			return errors.New("two secure entries share a slot; give each a distinct placeholder or occurrence")
		}
	}
	g.fingerprint = sha256.Sum256(data)
	g.info = info
	return nil
}

// Field returns the prepared slot of a secure question, or nil.
func (f *Form) Field(questionID string) *Field {
	return f.fields[questionID]
}

// Closed reports whether the form was submitted or cancelled.
func (f *Form) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// Result returns the channel that receives the form's single result.
func (f *Form) Result() <-chan FormResult { return f.done }

// Done broadcasts closure separately from the single-consumer answer result.
func (f *Form) Done() <-chan struct{} { return f.closedSignal }

// Submit writes every entered value to its file, then resolves the form with
// the answers. values maps secure question IDs to entered values; the caller
// clears them. A question without a value is reported cancelled. On error
// nothing is resolved, so the user can fix the entry and submit again.
func (f *Form) Submit(answers []question.Answer, values map[string][]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrClosed
	}
	for _, q := range f.Request.Questions {
		field := f.fields[q.ID]
		if field == nil || field.saved || len(values[q.ID]) == 0 {
			continue
		}
		if field.group.written {
			return &FieldError{QuestionID: q.ID, Err: errors.New("this file was already saved; request this entry again")}
		}
		if err := checkValue(values[q.ID]); err != nil {
			return &FieldError{QuestionID: q.ID, Err: err}
		}
	}
	// Stage every file before replacing any, so a failure changes nothing.
	var staged []*fileGroup
	discard := func(groups []*fileGroup) {
		for _, group := range groups {
			_ = group.root.Remove(group.staged)
			group.staged = ""
		}
	}
	for _, group := range f.groups {
		if group.written {
			continue
		}
		var slots []*Field
		for _, field := range group.fields {
			if len(values[field.QuestionID]) > 0 {
				slots = append(slots, field)
			}
		}
		if len(slots) == 0 {
			continue
		}
		if err := group.stage(slots, values); err != nil {
			discard(staged)
			return &FieldError{QuestionID: slots[0].QuestionID, Err: err}
		}
		staged = append(staged, group)
	}
	for i, group := range staged {
		if group.root.Rename(group.staged, group.name) != nil {
			discard(staged[i:])
			return &FieldError{QuestionID: group.fields[0].QuestionID, Err: errors.New("cannot replace destination file")}
		}
		group.staged = ""
		// The file changed, so its other slots can't be written by this form.
		group.written = true
		for _, field := range group.fields {
			field.saved = len(values[field.QuestionID]) > 0
		}
	}
	out := make([]question.Answer, len(answers))
	for i, answer := range answers {
		if f.fields[answer.QuestionID] != nil {
			// Nothing but the ID ever comes back for a secure question.
			answer = question.Answer{QuestionID: answer.QuestionID}
		}
		out[i] = answer
	}
	f.finishLocked(out, false)
	return nil
}

// stage writes the file with the slots filled to a private temporary file
// next to it, and checks the original didn't change meanwhile.
func (g *fileGroup) stage(slots []*Field, values map[string][]byte) error {
	data, info, err := readFile(g.root, g.name)
	if err != nil {
		return err
	}
	defer clear(data)
	if !os.SameFile(g.info, info) || sha256.Sum256(data) != g.fingerprint {
		return errChanged
	}
	name := ".crush-secure-" + randomName()
	file, err := g.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create private destination file")
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = g.root.Remove(name)
		}
	}()
	position := 0
	for _, slot := range slots {
		for _, part := range [][]byte{data[position:slot.offset], values[slot.QuestionID]} {
			if _, err := file.Write(part); err != nil {
				return errors.New("cannot write destination file")
			}
		}
		position = slot.offset + len(slot.Placeholder)
	}
	if _, err := file.Write(data[position:]); err != nil {
		return errors.New("cannot write destination file")
	}
	if file.Sync() != nil || file.Close() != nil {
		return errors.New("cannot finish writing destination file")
	}
	// Detect edits made during the write as well as while entering keys.
	current, currentInfo, err := readFile(g.root, g.name)
	if err != nil {
		return err
	}
	defer clear(current)
	if !os.SameFile(info, currentInfo) || sha256.Sum256(current) != g.fingerprint {
		return errChanged
	}
	keep = true
	g.staged = name
	return nil
}

// Cancel resolves the form without writing anything more.
func (f *Form) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.finishLocked(nil, true)
	}
}

func (f *Form) finishLocked(answers []question.Answer, cancelled bool) {
	f.closed = true
	statuses := make(map[string]string, len(f.fields))
	for id, field := range f.fields {
		statuses[id] = StatusCancelled
		if field.saved {
			statuses[id] = StatusSaved
		}
	}
	f.closeFiles()
	broker.Lock()
	if pending, ok := broker.pending.(*Form); ok && pending == f {
		broker.pending = nil
	}
	broker.Unlock()
	f.done <- FormResult{Answers: answers, Statuses: statuses, Cancelled: cancelled}
	close(f.closedSignal)
}

func (f *Form) closeFiles() {
	for _, group := range f.groups {
		if group.root != nil {
			_ = group.root.Close()
			group.root = nil
		}
	}
}
