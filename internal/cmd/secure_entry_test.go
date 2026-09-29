package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestSecureEntryCommandSendsMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(agent.TasksDirEnv, dir)
	t.Setenv(agent.TasksSessionEnv, "test-session")
	requests := make(chan agent.TaskRequest, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			paths, _ := filepath.Glob(filepath.Join(dir, "*.req"))
			if len(paths) == 0 {
				continue
			}
			data, err := os.ReadFile(paths[0])
			if err != nil {
				continue
			}
			var req agent.TaskRequest
			if json.Unmarshal(data, &req) != nil {
				continue
			}
			requests <- req
			_ = os.WriteFile(paths[0][:len(paths[0])-4]+".ack", []byte("{}"), 0o600)
			return
		}
	}()
	var output bytes.Buffer
	secureEntryCmd.SetOut(&output)
	secureEntryCmd.SetIn(bytes.NewBufferString("dummy-must-not-read-stdin"))
	t.Cleanup(func() { secureEntryCmd.SetOut(nil); secureEntryCmd.SetIn(nil) })
	for key, value := range map[string]string{"file": "template.env", "label": "API key", "placeholder": "%s", "occurrence": "1"} {
		flag := secureEntryCmd.Flags().Lookup(key)
		original := flag.Value.String()
		t.Cleanup(func() { _ = flag.Value.Set(original) })
		require.NoError(t, flag.Value.Set(value))
	}
	require.NoError(t, secureEntryCmd.RunE(secureEntryCmd, nil))
	req := <-requests
	require.Equal(t, "test-session", req.Session)
	require.NotNil(t, req.SecureEntry)
	require.True(t, filepath.IsAbs(req.SecureEntry.File))
	require.Equal(t, "%s", req.SecureEntry.Placeholder)
	require.Empty(t, req.Ask)
	require.Empty(t, req.Prompt)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		require.NoError(t, err)
		require.NotContains(t, string(data), "dummy-must-not-read-stdin")
	}
	require.NotContains(t, output.String(), "dummy-must-not-read-stdin")
	require.Contains(t, output.String(), "saved/cancelled")
}
