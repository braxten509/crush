package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/remote"
	"github.com/stretchr/testify/require"
)

// TestLauncherTargetDesktop: the launcher reads options.remote_launch_target
// at each launch; "desktop" runs `crush-desktop --project <home>` with the
// launch ID in CRUSH_REMOTE_LAUNCH, "terminal" keeps the terminal command
// (G-17). Nothing is started.
func TestLauncherTargetDesktop(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	data := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_DATA", data)
	t.Setenv("CRUSH_LAUNCH_COMMAND", "echo terminal")
	bin := t.TempDir()
	desktop := filepath.Join(bin, remote.DesktopAppCommand)
	require.NoError(t, os.WriteFile(desktop, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := t.TempDir()
	setTarget := func(target string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(data, "crush.json"),
			[]byte(`{"options":{"remote_launch_target":"`+target+`"}}`), 0o600))
	}
	launch := remote.DefaultLaunchCommand("/bin/crush", home, remoteLaunchTarget)

	setTarget("desktop")
	cmd, err := launch("launch-1")
	require.NoError(t, err)
	require.Equal(t, desktop, cmd.Path)
	require.Equal(t, []string{desktop, "--project", home}, cmd.Args)
	require.Equal(t, home, cmd.Dir)
	require.Contains(t, cmd.Env, remote.LaunchEnv+"=launch-1")

	// Read again at the next launch.
	setTarget("terminal")
	cmd, err = launch("launch-2")
	require.NoError(t, err)
	require.Equal(t, []string{"sh", "-c", "echo terminal"}, cmd.Args)
	require.Contains(t, cmd.Env, remote.LaunchEnv+"=launch-2")

	// No setting is the terminal.
	require.NoError(t, os.Remove(filepath.Join(data, "crush.json")))
	cmd, err = launch("launch-3")
	require.NoError(t, err)
	require.Equal(t, []string{"sh", "-c", "echo terminal"}, cmd.Args)

	// Without the app, the phone hears why.
	setTarget("desktop")
	require.NoError(t, os.Remove(desktop))
	_, err = launch("launch-4")
	require.EqualError(t, err, "The Crush desktop app isn't installed on this computer")
}
