package cliupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/stretchr/testify/require"
)

func TestStateProcessHelper(t *testing.T) {
	path := os.Getenv("CRUSH_TEST_UPDATE_STATE")
	if path == "" {
		return
	}
	statePath = func() string { return path }
	action := os.Getenv("CRUSH_TEST_UPDATE_ACTION")
	require.NoError(t, os.WriteFile(path+"."+action+".ready", nil, 0o600))
	if action[:5] == "check" {
		require.NoError(t, os.WriteFile(path+"."+action+".result", []byte(fmt.Sprint(StartAutoCheck())), 0o600))
	} else {
		Decline([]Update{{Bin: action, Latest: "1.2.3"}})
	}
}

func TestConcurrentStateChangesAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli-updates.json")
	saved := statePath
	t.Cleanup(func() { statePath = saved })
	statePath = func() string { return path }
	release, err := lock.File(t.Context(), path+".lock")
	require.NoError(t, err)
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	executable, err := os.Executable()
	require.NoError(t, err)

	actions := []string{"codex", "grok-cli", "claude", "check1", "check2", "check3"}
	results := make(chan error, len(actions))
	for _, action := range actions {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStateProcessHelper$")
		cmd.Env = append(os.Environ(), "CRUSH_TEST_UPDATE_STATE="+path, "CRUSH_TEST_UPDATE_ACTION="+action)
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		require.NoError(t, cmd.Start())
		go func() {
			err := cmd.Wait()
			if err != nil {
				err = fmt.Errorf("%s: %w: %s", action, err, output.String())
			}
			results <- err
		}()
	}
	require.Eventually(t, func() bool {
		for _, action := range actions {
			if _, err := os.Stat(path + "." + action + ".ready"); err != nil {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	// Both operations must wait for the cross-process lock, not just serialize
	// goroutines in one window.
	select {
	case err := <-results:
		t.Fatalf("state changed while another process held the lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "state must not be published while the lock is held")
	release()
	release = nil
	for range actions {
		require.NoError(t, <-results)
	}
	s := loadState()
	require.Equal(t, map[string]string{"codex": "1.2.3", "grok-cli": "1.2.3", "claude": "1.2.3"}, s.Declined)
	require.WithinDuration(t, time.Now(), s.CheckedAt, time.Minute)
	var checks int
	for _, action := range actions[3:] {
		data, err := os.ReadFile(path + "." + action + ".result")
		require.NoError(t, err)
		if string(data) == "true" {
			checks++
		}
	}
	require.Equal(t, 1, checks, "only one concurrent startup check is due")
}

func TestStateWriteUsesUniqueTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli-updates.json")
	saved := statePath
	t.Cleanup(func() { statePath = saved })
	statePath = func() string { return path }
	require.NoError(t, os.WriteFile(path+".tmp", []byte("another writer"), 0o600))
	Decline([]Update{{Bin: "codex", Latest: "1.2.3"}})
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, json.Valid(data))
	data, err = os.ReadFile(path + ".tmp")
	require.NoError(t, err)
	require.Equal(t, "another writer", string(data))
}

func TestConcurrentStateReadersSeeCompleteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli-updates.json")
	saved := statePath
	t.Cleanup(func() { statePath = saved })
	statePath = func() string { return path }
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	var wg sync.WaitGroup
	errors := make(chan error, 1)
	wg.Go(func() {
		for range 30 {
			data, err := os.ReadFile(path)
			if err != nil {
				errors <- err
				return
			}
			if !json.Valid(data) {
				errors <- fmt.Errorf("state writer published partial JSON")
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	for range 30 {
		Decline([]Update{{Bin: "codex", Latest: strings.Repeat("version", 100_000)}})
	}
	wg.Wait()
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
}
