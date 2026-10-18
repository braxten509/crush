package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// fakeUpdateCLI writes a stand-in AI tool: `--version` prints current, and
// any other call appends its arguments to the returned log. Its updater
// arguments are args.
func fakeUpdateCLI(t *testing.T, name, current, latest string, args ...string) (cliupdate.CLI, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, strings.ToLower(strings.ReplaceAll(name, " ", "-")))
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo %q; exit 0; fi\necho \"$@\" >> %q\n", name+" "+current, log)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return cliupdate.CLI{
		Name:       name,
		Bin:        bin,
		Latest:     func(context.Context, string) (string, error) { return latest, nil },
		UpdateArgs: args,
	}, log
}

// useUpdateCLIs makes clis the only AI tools the update check knows, with
// the update state in a fresh folder, for the rest of the test.
func useUpdateCLIs(t *testing.T, clis ...cliupdate.CLI) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	saved := cliupdate.CLIs
	cliupdate.CLIs = clis
	t.Cleanup(func() { cliupdate.CLIs = saved })
}

// TestCLIUpdateInstallUsesLastCheck: installing a bin from the last check
// runs its own updater with its private arguments, and the results reach
// only the app that asked (engine-design.md 5.9 d).
func TestCLIUpdateInstallUsesLastCheck(t *testing.T) {
	fake, fakeLog := fakeUpdateCLI(t, "Fake CLI", "1.0.0", "1.2.0", "self-update", "--channel", "stable")
	other, otherLog := fakeUpdateCLI(t, "Other CLI", "2.0.0", "2.1.0", "upgrade")
	useUpdateCLIs(t, fake, other)

	b := liveTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	asker, bystander := newClientID(t), newClientID(t)
	ws.clientsMu.Lock()
	ws.clients[asker] = &clientState{}
	ws.clients[bystander] = &clientState{}
	ws.clientsMu.Unlock()
	askerEvents, err := b.SubscribeClient(t.Context(), ws.ID, asker)
	require.NoError(t, err)
	bystanderEvents, err := b.SubscribeClient(t.Context(), ws.ID, bystander)
	require.NoError(t, err)

	check := b.CheckCLIUpdates(t.Context(), true)
	require.False(t, check.Skipped)
	require.Empty(t, check.Errors)
	require.Equal(t, []proto.CLIUpdate{
		{Name: "Fake CLI", Bin: fake.Bin, Path: fake.Bin, Current: "1.0.0", Latest: "1.2.0"},
		{Name: "Other CLI", Bin: other.Bin, Path: other.Bin, Current: "2.0.0", Latest: "2.1.0"},
	}, check.Updates)

	require.NoError(t, b.InstallCLIUpdates(asker, []string{fake.Bin}))

	var ev EngineEvent
	select {
	case got := <-askerEvents:
		ev = got.(EngineEvent)
	case <-time.After(30 * time.Second):
		t.Fatal("no cli_updates_installed event")
	}
	require.Equal(t, pubsub.PayloadTypeCLIUpdatesInstalled, ev.Type)
	require.Equal(t, proto.CLIUpdatesInstalled{Results: []proto.CLIUpdateResult{{Name: "Fake CLI", Latest: "1.2.0"}}}, ev.Payload)

	calls, err := os.ReadFile(fakeLog)
	require.NoError(t, err)
	require.Equal(t, "self-update --channel stable\n", string(calls), "the updater ran with the check's private arguments")
	require.NoFileExists(t, otherLog, "a bin that was not asked for is left alone")
	require.Empty(t, engineEvents(bystanderEvents), "only the app that asked gets the results")

	require.ErrorIs(t, b.InstallCLIUpdates(newClientID(t), []string{fake.Bin}), ErrClientNotAttached)
	require.ErrorIs(t, b.InstallCLIUpdates("not-a-uuid", []string{fake.Bin}), ErrInvalidClientID)
}

// TestCLIUpdateInstallChecksWhenStale: with no check in the last 10 minutes,
// install checks again first.
func TestCLIUpdateInstallChecksWhenStale(t *testing.T) {
	fake, fakeLog := fakeUpdateCLI(t, "Fake CLI", "1.0.0", "1.2.0", "update")
	useUpdateCLIs(t, fake)

	b := liveTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	asker := newClientID(t)
	ws.clientsMu.Lock()
	ws.clients[asker] = &clientState{}
	ws.clientsMu.Unlock()
	events, err := b.SubscribeClient(t.Context(), ws.ID, asker)
	require.NoError(t, err)

	require.NoError(t, b.InstallCLIUpdates(asker, []string{fake.Bin}))
	select {
	case got := <-events:
		ev := got.(EngineEvent)
		require.Equal(t, proto.CLIUpdatesInstalled{Results: []proto.CLIUpdateResult{{Name: "Fake CLI", Latest: "1.2.0"}}}, ev.Payload)
	case <-time.After(30 * time.Second):
		t.Fatal("no cli_updates_installed event")
	}
	calls, err := os.ReadFile(fakeLog)
	require.NoError(t, err)
	require.Equal(t, "update\n", string(calls))
}
