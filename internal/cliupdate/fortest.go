package cliupdate

import (
	"context"
	"slices"
	"sync"
)

// testUpdates, when set, stand in for the network checks and the CLIs'
// own updaters (SetUpdatesForTest).
var (
	testMu      sync.Mutex
	testUpdates testState
)

type testState struct {
	set     bool
	updates []Update
	install func(Update) error
}

// SetUpdatesForTest makes Check report updates, with no CLI run and no
// network asked, and Install answer with install, until restore runs. The
// crushtest seam `cli_updates` uses it (desktop design 5-6, G-22).
func SetUpdatesForTest(updates []Update, install func(Update) error) (restore func()) {
	testMu.Lock()
	defer testMu.Unlock()
	saved := testUpdates
	testUpdates = testState{set: true, updates: slices.Clone(updates), install: install}
	return func() {
		testMu.Lock()
		defer testMu.Unlock()
		testUpdates = saved
	}
}

// testCheck returns the test's updates when SetUpdatesForTest set them.
func testCheck(context.Context) ([]Update, bool) {
	testMu.Lock()
	defer testMu.Unlock()
	return slices.Clone(testUpdates.updates), testUpdates.set
}

// testInstall runs the test's install when SetUpdatesForTest set one.
func testInstall(u Update) (bool, error) {
	testMu.Lock()
	install, set := testUpdates.install, testUpdates.set
	testMu.Unlock()
	if !set {
		return false, nil
	}
	if install == nil {
		return true, nil
	}
	return true, install(u)
}
