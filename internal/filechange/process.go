package filechange

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/shell"
)

// ProcessReview observes files a command actually attempts to mutate. It does
// not search directories or infer filenames from source code or command output.
type ProcessReview struct {
	mutex       sync.Mutex
	root        string
	exclude     []string
	calls       map[string]*processCall
	detached    chan struct{}
	invocations map[uint64]*invocation
	next        uint64
	observeRoot bool
	report      *CommandReview
}

type processCall struct {
	command string
	words   []string
	quoted  []string
}

// quotedForms are the command as a wrapper script embeds it in single quotes,
// as in Claude Code's `eval '<command>'`. Both common escapes for an inner
// quote are accepted; the surrounding quotes keep a shorter call from matching.
func quotedForms(command string) []string {
	forms := []string{}
	for _, escape := range []string{`'"'"'`, `'\''`} {
		form := "'" + strings.ReplaceAll(command, "'", escape) + "'"
		if !slices.Contains(forms, form) {
			forms = append(forms, form)
		}
	}
	return forms
}

func newProcessReview(root string, exclude []string) *ProcessReview {
	return &ProcessReview{root: root, exclude: exclude, calls: map[string]*processCall{}, invocations: map[uint64]*invocation{}}
}

func (p *ProcessReview) Begin(id, command string) {
	if p == nil || command == "" {
		return
	}
	words, _ := shell.Fields(command, func(string) string { return "" })
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.calls[id] = &processCall{command: command, words: words, quoted: quotedForms(command)}
}

// Each executed process retains its invocation chain, so a shell tool can be
// matched even when a CLI reports its call only after the command has run.
// Only mutations are captured; neither reads nor folders are inventoried.
type invocation struct {
	number   uint64
	commands [][]string
	tracker  *Tracker
	captured map[string]int64
}

func (p *ProcessReview) executed(parent *invocation, args []string) *invocation {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.next++
	record := &invocation{number: p.next, commands: [][]string{args}}
	if parent != nil {
		record.commands = append(record.commands, parent.commands...)
	}
	return record
}

func (call *processCall) matches(args []string) bool {
	for _, arg := range args {
		if arg == call.command {
			return true
		}
		for _, quoted := range call.quoted {
			if strings.Contains(arg, quoted) {
				return true
			}
		}
	}
	words := call.words
	if len(words) != len(args) || len(words) == 0 || filepath.Base(words[0]) != filepath.Base(args[0]) {
		return false
	}
	return slices.Equal(words[1:], args[1:])
}

func (p *ProcessReview) before(record *invocation, path string) {
	if record == nil || path == "" || !filepath.IsAbs(path) || strings.HasSuffix(path, " (deleted)") {
		return
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if record.tracker == nil {
		tracker, err := New(context.Background(), p.root, p.exclude...)
		if err != nil {
			return
		}
		record.tracker = tracker
		record.captured = map[string]int64{}
	}
	if !record.tracker.Contains(path) {
		record.tracker.Track(path)
		record.captured[path] = time.Now().UnixNano()
	}
	if len(record.tracker.extra) > 0 {
		p.invocations[record.number] = record
	}
}

func (p *ProcessReview) End(id string) *Review {
	if p == nil {
		return nil
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	call := p.calls[id]
	delete(p.calls, id)
	if call == nil {
		return nil
	}
	var records []*invocation
	for number, record := range p.invocations {
		for _, command := range record.commands {
			if call.matches(command) {
				records = append(records, record)
				delete(p.invocations, number)
				break
			}
		}
	}
	slices.SortFunc(records, func(a, b *invocation) int {
		if a.number < b.number {
			return -1
		}
		if a.number > b.number {
			return 1
		}
		return 0
	})
	review := &Review{Root: p.root}
	paths := map[string]int{}
	earliest := map[string]int64{}
	for _, record := range records {
		changes, err := record.tracker.Checkpoint(context.Background())
		if err != nil {
			continue
		}
		for _, change := range changes.Changes {
			change.Order = record.captured[change.Path]
			if index, ok := paths[change.Path]; ok {
				review.Changes[index].After = change.After
				if record.captured[change.Path] < earliest[change.Path] {
					review.Changes[index].Before = change.Before
					earliest[change.Path] = record.captured[change.Path]
				}
			} else {
				paths[change.Path] = len(review.Changes)
				earliest[change.Path] = record.captured[change.Path]
				review.Changes = append(review.Changes, change)
			}
		}
		record.tracker = nil
		record.captured = nil
	}
	if len(review.Changes) == 0 {
		return nil
	}
	return review
}

// WithReview carries a local review alongside the tool's ordinary metadata.
// The agent extracts it before persisting the result; it is never model input.
func WithReview(metadata string, review *Review) string {
	if review == nil {
		return metadata
	}
	values := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(metadata), &values)
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	values["file_review"], _ = json.Marshal(review)
	data, _ := json.Marshal(values)
	return string(data)
}

func TakeReview(metadata string) (string, *Review) {
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(metadata), &values) != nil {
		return metadata, nil
	}
	raw, ok := values["file_review"]
	if !ok {
		return metadata, nil
	}
	var review Review
	if json.Unmarshal(raw, &review) != nil {
		return metadata, nil
	}
	delete(values, "file_review")
	data, _ := json.Marshal(values)
	return string(data), &review
}

// StartProcess preserves the command's arguments, environment, process group,
// and permission checks. The platform observer only inspects file mutations.
// Call the returned observer's Wait method, not cmd.Wait, to collect its exit.
func StartProcess(cmd *exec.Cmd, root string, exclude ...string) (*ProcessReview, error) {
	review := newProcessReview(root, exclude)
	if err := startObservedProcess(cmd, review); err != nil {
		return nil, err
	}
	return review, nil
}

// Wait avoids racing exec.Cmd's wait with ptrace's syscall-stop notifications.
// Only the tracer consumes stops; exec.Cmd still collects the real exit status.
func (p *ProcessReview) Wait(cmd *exec.Cmd) error {
	if p != nil && p.detached != nil {
		<-p.detached
	}
	err := cmd.Wait()
	if p != nil && p.report != nil {
		if review := p.End("command"); review != nil {
			p.report.mutex.Lock()
			p.report.changes = append(p.report.changes, review.Changes...)
			p.report.mutex.Unlock()
		}
	}
	return err
}
