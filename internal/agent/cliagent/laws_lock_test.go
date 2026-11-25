package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/stretchr/testify/require"
)

func TestEnsureAGYGuardHookAt(t *testing.T) {
	t.Parallel()

	t.Run("creates new file and preserves existing user hooks", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config", "hooks.json")
		require.NoError(t, ensureAGYGuardHookAt(path, "'/usr/bin/crush' secrets-guard"))

		var got map[string]any
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &got))
		require.Contains(t, got, "crush-secrets-guard")

		// Add a user hook and confirm re-running preserves it.
		got["user-hook"] = map[string]any{"enabled": true}
		raw, err := json.Marshal(got)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0o600))

		require.NoError(t, ensureAGYGuardHookAt(path, "'/usr/bin/crush' secrets-guard"))
		data, err = os.ReadFile(path)
		require.NoError(t, err)
		got = nil
		require.NoError(t, json.Unmarshal(data, &got))
		require.Contains(t, got, "user-hook")
		require.Contains(t, got, "crush-secrets-guard")
	})

	t.Run("handles json null without panicking", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "hooks.json")
		require.NoError(t, os.WriteFile(path, []byte("null\n"), 0o600))
		require.NoError(t, ensureAGYGuardHookAt(path, "'/usr/bin/crush' secrets-guard"))

		var got map[string]any
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(data, &got))
		require.Contains(t, got, "crush-secrets-guard")
	})

	t.Run("returns error on invalid json without overwriting", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "hooks.json")
		original := []byte("{broken json")
		require.NoError(t, os.WriteFile(path, original, 0o600))
		err := ensureAGYGuardHookAt(path, "'/usr/bin/crush' secrets-guard")
		require.Error(t, err)
		after, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, original, after)
	})
}

func TestACPBlocksLawsMultiFile(t *testing.T) {
	t.Parallel()

	parts := []acpContent{
		{Type: "diff", Path: "LAWS.bend", OldText: "a", NewText: "b"},
		{Type: "diff", Path: "src/main.rs", OldText: "c", NewText: "d"},
	}
	require.True(t, acpBlocksLaws(acpUpdate{Kind: "edit", Title: "patch"}, parts, "/repo"))

	// Relative approved inside tools/laws dir is caught too.
	relParts := []acpContent{
		{Type: "diff", Path: "approved", OldText: "a", NewText: "b"},
		{Type: "diff", Path: "other.txt", OldText: "c", NewText: "d"},
	}
	require.True(t, acpBlocksLaws(acpUpdate{Kind: "edit", Title: "patch"}, relParts, "/repo/tools/laws"))

	// Read-only location on LAWS.bend is allowed.
	readCall := acpUpdate{
		Kind:  "read",
		Title: "read_file",
		Locations: []struct {
			Path string `json:"path"`
		}{{Path: "/repo/LAWS.bend"}},
	}
	require.False(t, acpBlocksLaws(readCall, nil, "/repo"))

	// Edit location on LAWS.bend is blocked even with no diff content.
	editCall := acpUpdate{
		Kind:  "edit",
		Title: "edit_file",
		Locations: []struct {
			Path string `json:"path"`
		}{{Path: "/repo/LAWS.bend"}},
	}
	require.True(t, acpBlocksLaws(editCall, nil, "/repo"))
}

func TestACPTerminalBlocksLawsWithDir(t *testing.T) {
	t.Parallel()

	terms := newACPTerminals("/repo")
	_, err := terms.create("./approve", nil, nil, "/repo/tools/laws", 0)
	require.EqualError(t, err, secretguard.LawsReason)

	_, err = terms.create("rm", []string{"approved"}, nil, "/repo/tools/laws", 0)
	require.EqualError(t, err, secretguard.LawsReason)
}
