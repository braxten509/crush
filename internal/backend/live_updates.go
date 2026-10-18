package backend

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/selfupdate"
)

// Update checks for the AI tools and for Crush itself (engine-design.md
// 5.9 d). They are about the computer, not the project: the state files are
// shared, so every engine gives the same answers.

const (
	// updateCheckTimeout bounds one check, as in the terminal.
	updateCheckTimeout = time.Minute
	// cliUpdateKeep is how long install reuses the last check's updates.
	cliUpdateKeep = 10 * time.Minute
)

// CheckCLIUpdates looks for newer releases of the installed AI tools. An
// automatic check runs at most hourly across every Crush (skipped
// otherwise) and leaves out declined versions; a manual one always runs.
func (b *Backend) CheckCLIUpdates(ctx context.Context, manual bool) proto.CLIUpdateCheck {
	if !manual && !cliupdate.StartAutoCheck() {
		return proto.CLIUpdateCheck{Skipped: true, Updates: []proto.CLIUpdate{}}
	}
	updates, errs := b.checkCLIs(ctx)
	if !manual {
		updates = cliupdate.WithoutDeclined(updates)
	}
	out := proto.CLIUpdateCheck{Updates: make([]proto.CLIUpdate, 0, len(updates))}
	for _, u := range updates {
		out.Updates = append(out.Updates, proto.CLIUpdate{Name: u.Name, Bin: u.Bin, Path: u.Path, Current: u.Current, Latest: u.Latest})
	}
	for _, err := range errs {
		out.Errors = append(out.Errors, err.Error())
	}
	return out
}

// checkCLIs runs one check and keeps its updates, with their updater
// arguments, for install and decline.
func (b *Backend) checkCLIs(ctx context.Context) ([]cliupdate.Update, []error) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	updates, errs := cliupdate.Check(ctx)
	e := b.liveEngine()
	e.updMu.Lock()
	e.lastCheck, e.checkedAt, e.checked = updates, time.Now(), true
	e.updMu.Unlock()
	return updates, errs
}

// lastCLIUpdates returns the last check's updates for bins, and whether
// the check is fresh enough to install from.
func (b *Backend) lastCLIUpdates(bins []string) (picked []cliupdate.Update, fresh bool) {
	e := b.liveEngine()
	e.updMu.Lock()
	defer e.updMu.Unlock()
	for _, u := range e.lastCheck {
		if slices.Contains(bins, u.Bin) {
			picked = append(picked, u)
		}
	}
	return picked, e.checked && time.Since(e.checkedAt) < cliUpdateKeep
}

// DeclineCLIUpdates skips the versions the last check offered for bins
// until newer ones come out. Bins that check did not offer are ignored.
func (b *Backend) DeclineCLIUpdates(bins []string) {
	if picked, _ := b.lastCLIUpdates(bins); len(picked) > 0 {
		cliupdate.Decline(picked)
	}
}

// InstallCLIUpdates starts installing the updates for bins, all at once,
// and returns at once. The app clientID gets the results as
// cli_updates_installed. It must be registered on a workspace of this
// engine.
func (b *Backend) InstallCLIUpdates(clientID string, bins []string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}
	if !b.clientKnown(clientID) {
		return ErrClientNotAttached
	}
	go b.installCLIUpdates(clientID, bins)
	return nil
}

func (b *Backend) installCLIUpdates(clientID string, bins []string) {
	picked, fresh := b.lastCLIUpdates(bins)
	if !fresh {
		b.checkCLIs(b.engineContext())
		picked, _ = b.lastCLIUpdates(bins)
	}
	results := make([]proto.CLIUpdateResult, len(picked))
	var wg sync.WaitGroup
	for i, u := range picked {
		wg.Go(func() {
			results[i] = proto.CLIUpdateResult{Name: u.Name, Latest: u.Latest}
			if err := cliupdate.Install(context.Background(), u); err != nil {
				results[i].Error = err.Error()
			}
		})
	}
	wg.Wait()
	ev := EngineEvent{Type: pubsub.PayloadTypeCLIUpdatesInstalled, Kind: pubsub.CreatedEvent, Payload: proto.CLIUpdatesInstalled{Results: results}}
	for _, ws := range b.workspaces.Seq2() {
		b.SendTo(ws.ID, clientID, ev)
	}
}

// clientKnown reports whether clientID is registered on any workspace.
func (b *Backend) clientKnown(clientID string) bool {
	for _, ws := range b.workspaces.Seq2() {
		if isRegistered(ws, clientID) {
			return true
		}
	}
	return false
}

// crushSource is where CheckCrushUpdate looks: the fork checkout, the
// newest stable release and an already built one.
type crushSource struct {
	dir    func() string
	check  func(ctx context.Context, dir string) (*selfupdate.Release, error)
	staged func(ctx context.Context, dir, tag string) string
}

var crushUpdates = struct {
	sync.Mutex
	src crushSource
}{src: crushSource{dir: selfupdate.Dir, check: selfupdate.Check, staged: selfupdate.Staged}}

// SetCrushUpdateSourceForTest makes CheckCrushUpdate read the checkout,
// the release and the built one from the given functions until restore
// runs, so a test asks no network.
func SetCrushUpdateSourceForTest(dir func() string, check func(ctx context.Context, dir string) (*selfupdate.Release, error), staged func(ctx context.Context, dir, tag string) string) (restore func()) {
	crushUpdates.Lock()
	defer crushUpdates.Unlock()
	saved := crushUpdates.src
	crushUpdates.src = crushSource{dir: dir, check: check, staged: staged}
	return func() {
		crushUpdates.Lock()
		defer crushUpdates.Unlock()
		crushUpdates.src = saved
	}
}

// declinedCrush is how a skipped Crush release is remembered, as the
// terminal remembers it (ui/model/selfupdate.go).
func declinedCrush(tag string) []cliupdate.Update {
	return []cliupdate.Update{{Name: "Crush", Bin: "crush", Latest: tag}}
}

// CheckCrushUpdate looks for a stable upstream release newer than this
// fork build. Merging, building and installing stay in `crush self-update`.
func (b *Backend) CheckCrushUpdate(ctx context.Context) proto.CrushUpdateCheck {
	crushUpdates.Lock()
	src := crushUpdates.src
	crushUpdates.Unlock()
	dir := src.dir()
	out := proto.CrushUpdateCheck{HasCheckout: dir != ""}
	if dir == "" {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	release, err := src.check(ctx, dir)
	switch {
	case err != nil:
		out.Error = err.Error()
	case release != nil:
		out.Release = &proto.CrushRelease{Tag: release.Tag, Current: release.Current, Dir: release.Dir}
		out.Staged = src.staged(context.WithoutCancel(ctx), release.Dir, release.Tag)
		out.Declined = len(cliupdate.WithoutDeclined(declinedCrush(release.Tag))) == 0
	}
	return out
}

// DeclineCrushUpdate skips the Crush release tag, as the terminal's No
// does.
func (b *Backend) DeclineCrushUpdate(tag string) {
	cliupdate.Decline(declinedCrush(tag))
}
