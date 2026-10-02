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
	mutex    sync.Mutex
	root     string
	exclude  []string
	direct   *Tracker
	directAt map[string]int64
	changes  []Change
}

func WithCommandReview(ctx context.Context, root string, exclude ...string) (context.Context, *CommandReview) {
	report := &CommandReview{root: root, exclude: exclude}
	return context.WithValue(ctx, commandContextKey{}, report), report
}

func BeforeOpen(ctx context.Context, path string, flags int) {
	report, _ := ctx.Value(commandContextKey{}).(*CommandReview)
	if report == nil || flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) == 0 {
		return
	}
	report.mutex.Lock()
	defer report.mutex.Unlock()
	if report.direct == nil {
		report.direct, _ = New(context.Background(), report.root, report.exclude...)
		report.directAt = map[string]int64{}
	}
	if report.direct != nil {
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
	if r.direct != nil {
		if direct, err := r.direct.Checkpoint(context.Background()); err == nil {
			for _, change := range direct.Changes {
				change.Order = r.directAt[change.Path]
				r.changes = append(r.changes, change)
			}
		}
	}
	if len(r.changes) == 0 {
		return nil
	}
	// Multiple subprocesses and shell redirections may touch the same file.
	// Keep its earliest before image and read its final state exactly once.
	earliest := map[string]Change{}
	for _, change := range r.changes {
		if previous, ok := earliest[change.Path]; !ok || change.Order < previous.Order {
			earliest[change.Path] = change
		}
	}
	changes := make([]Change, 0, len(earliest))
	budget := maxTextTotal
	for path, change := range earliest {
		if info, err := os.Lstat(path); err == nil {
			state := readEntry(path, info, &budget).state
			change.After = &state
		} else if os.IsNotExist(err) {
			change.After = nil
		}
		changes = append(changes, change)
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return &Review{Root: r.root, Changes: changes}
}
