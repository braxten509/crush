package filechange

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

type commandContextKey struct{}

// CommandReview is shared by the native shell's redirects and child processes.
// It is attached after permission approval; it never changes command dispatch.
type CommandReview struct {
	mutex     sync.Mutex
	root      string
	exclude   []string
	direct    *Tracker
	directAt  map[string]int64
	changes   []Change
	deletions bool
	store     *snapshotStore
	finished  *Review
	done      bool
}

func WithCommandReview(ctx context.Context, root string, exclude ...string) (context.Context, *CommandReview) {
	report := &CommandReview{root: root, exclude: exclude, store: newSnapshotStore()}
	return context.WithValue(ctx, commandContextKey{}, report), report
}

// CommandReviewFromContext returns the report owned by this shell job.
func CommandReviewFromContext(ctx context.Context) *CommandReview {
	report, _ := ctx.Value(commandContextKey{}).(*CommandReview)
	return report
}

func BeforeOpen(ctx context.Context, path string, flags int) {
	report, _ := ctx.Value(commandContextKey{}).(*CommandReview)
	if report == nil || flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) == 0 {
		return
	}
	path = canonicalParent(path)
	report.mutex.Lock()
	defer report.mutex.Unlock()
	if report.done {
		return
	}
	if report.direct == nil {
		report.direct, _ = New(context.Background(), report.root, report.exclude...)
		if report.direct != nil {
			report.direct.store = report.store
		}
		report.directAt = map[string]int64{}
	}
	if report.direct != nil && !report.direct.excluded(path) {
		if !report.direct.Contains(path) {
			report.directAt[path] = time.Now().UnixNano()
			report.direct.Track(path)
		}
	}
}

// StartCommand uses normal exec when there is no review context (e.g. hooks).
func StartCommand(ctx context.Context, cmd *exec.Cmd) (*ProcessReview, error) {
	report, _ := ctx.Value(commandContextKey{}).(*CommandReview)
	if report == nil {
		return nil, cmd.Start()
	}
	observer := newProcessReview(report.root, report.exclude)
	observer.store = report.store
	observer.observeRoot = true
	observer.calls["command"] = &processCall{words: cmd.Args}
	observer.report = report
	if err := startObservedProcess(cmd, observer); err != nil {
		return nil, err
	}
	return observer, nil
}

func (r *CommandReview) Finish() *Review {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.done {
		return r.finished
	}
	r.done = true
	defer func() {
		// The report's private pool includes completed subprocess trackers and the
		// finalization reader (nil owner). Closing the pool also prevents a
		// detached child from allocating again after the shell has returned.
		r.store.releaseAll()
		r.direct = nil
		r.changes = nil
	}()
	if r.direct != nil {
		if direct, err := r.direct.Checkpoint(context.Background()); err == nil {
			r.deletions = r.deletions || direct.Deletions
			for _, change := range direct.Changes {
				change.Order = r.directAt[change.Path]
				r.changes = append(r.changes, change)
			}
		}
	}
	if len(r.changes) == 0 && !r.deletions {
		return nil
	}
	// Multiple subprocesses and shell redirections may touch the same file.
	// Keep its earliest before image and read its final state exactly once.
	earliest := map[string]Change{}
	for _, change := range mergeChanges(slices.Clone(r.changes)) {
		if previous, ok := earliest[change.Path]; !ok || change.Order < previous.Order {
			earliest[change.Path] = change
		}
	}
	changes := make([]Change, 0, len(earliest))
	restoreBudget := MaxRestoreTotal
	for path, change := range earliest {
		if change.Before == nil && change.After != nil && (change.Transfer == nil || change.Transfer.Baseline == nil) && (change.After.Omitted == "Copied content" || change.After.Omitted == "Generated build artifact") {
			changes = append(changes, change)
			continue
		}
		baseline := change.ImportedState()
		if info, err := os.Lstat(path); err == nil {
			value := r.store.read(nil, path, info)
			state := value.reviewState(&restoreBudget)
			change.After = &state
		} else if os.IsNotExist(err) {
			change.After = nil
		}
		if change.Before != nil && change.After == nil {
			r.deletions = true
			continue
		}
		if change.Transfer != nil && change.Transfer.Baseline == nil && baseline != nil && change.After != nil && *baseline != *change.After {
			transfer := *change.Transfer
			transfer.Baseline = baseline
			change.Transfer = &transfer
		}
		changes = append(changes, change)
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	r.finished = &Review{Root: r.root, Changes: changes, Deletions: r.deletions}
	return r.finished
}
