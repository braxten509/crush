package filechange

import (
	"cmp"
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
	invocations map[uint64]*invocation
	executions  map[uint64]*invocation
	store       *snapshotStore
	nextCall    uint64
	next        uint64
	observeRoot bool
	report      *CommandReview
}

type processCall struct {
	command string
	words   []string
	quoted  []string
	order   uint64
	root    *invocation
	ended   bool
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
	return &ProcessReview{root: root, exclude: exclude, calls: map[string]*processCall{}, invocations: map[uint64]*invocation{}, executions: map[uint64]*invocation{}, store: newSnapshotStore()}
}

func (p *ProcessReview) Begin(id, command string) {
	if p == nil || command == "" {
		return
	}
	words, _ := shell.Fields(command, func(string) string { return "" })
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.nextCall++
	p.calls[id] = &processCall{command: command, words: words, quoted: quotedForms(command), order: p.nextCall}
	p.bindCalls()
}

// Each executed process retains its invocation chain, so a shell tool can be
// matched even when a CLI reports its call only after the command has run.
// Only mutations are captured; neither reads nor folders are inventoried.
type invocation struct {
	number   uint64
	args     []string
	parent   *invocation
	owner    *processCall
	tracker  *Tracker
	captured map[string]int64
	moves    map[string]moveSource
}

func (p *ProcessReview) executed(parent *invocation, args []string) *invocation {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.next++
	record := &invocation{number: p.next, args: args, parent: parent}
	p.executions[record.number] = record
	p.bindCalls()
	return record
}

// Bind one command occurrence, then follow its process subtree. Late tool
// reports claim the earliest unclaimed occurrence, never every argv match.
func (p *ProcessReview) bindCalls() {
	calls := make([]*processCall, 0, len(p.calls))
	for _, call := range p.calls {
		if call.root == nil {
			calls = append(calls, call)
		}
	}
	slices.SortFunc(calls, func(a, b *processCall) int { return cmp.Compare(a.order, b.order) })
	for _, call := range calls {
		for _, record := range p.executions {
			if record.call() != nil || !call.matches(record.args) {
				continue
			}
			if call.root == nil || record.number < call.root.number {
				call.root = record
			}
		}
		if call.root != nil {
			call.root.owner = call
		}
	}
}

func (record *invocation) call() *processCall {
	for current := record; current != nil; current = current.parent {
		if current.owner != nil {
			return current.owner
		}
	}
	return nil
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
	path = canonicalParent(path)
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if call := record.call(); call != nil && call.ended {
		return
	}
	if record.tracker == nil {
		tracker, err := New(context.Background(), p.root, p.exclude...)
		if err != nil {
			return
		}
		tracker.store = p.store
		tracker.imported = transferKind(record.args) != ""
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
	call.ended = true
	var records []*invocation
	for number, record := range p.invocations {
		if record.call() == call {
			records = append(records, record)
			delete(p.invocations, number)
		}
	}
	for number, record := range p.executions {
		if record.call() == call {
			delete(p.executions, number)
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
	// Keep every capture charged until all subprocess checkpoints are assembled.
	// Native shell reports own these captures until their final combined review.
	var owners []*Tracker
	defer func() {
		if p.report == nil {
			p.store.release(owners...)
		}
	}()
	for _, record := range records {
		owners = append(owners, record.tracker)
		changes, err := record.tracker.Checkpoint(context.Background())
		if err != nil {
			continue
		}
		for _, change := range changes.Changes {
			change.Order = record.captured[change.Path]
			if kind := transferKind(record.args); kind != "" && change.Before == nil && change.After != nil {
				change.Transfer = &Transfer{Kind: kind}
			}
			if change.Before == nil && change.After != nil && change.After.Omitted == "Generated build artifact" {
				change.Transfer = &Transfer{Kind: "generated"}
			}
			if source, ok := record.moves[change.Path]; ok && change.Before == nil && change.After != nil && source.movedTo(change.Path) {
				change.Transfer = &Transfer{Kind: "move", Source: source.path, Baseline: source.state}
			}
			review.Changes = append(review.Changes, change)
		}
		record.tracker = nil
		record.captured = nil
	}
	if len(review.Changes) == 0 {
		return nil
	}
	review.Changes = mergeChanges(review.Changes)
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

// Wait collects the command's exit, then adds its changes to the command report.
func (p *ProcessReview) Wait(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if p != nil && p.report != nil {
		if review := p.End("command"); review != nil {
			p.report.mutex.Lock()
			if !p.report.done {
				p.report.changes = append(p.report.changes, review.Changes...)
			}
			p.report.mutex.Unlock()
		}
	}
	return err
}
