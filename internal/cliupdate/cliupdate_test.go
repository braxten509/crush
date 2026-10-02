package cliupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.1.286", "2.1.285", true},
		{"2.1.285", "2.1.286", false},
		{"2.1.285", "2.1.285", false},
		{"0.159.10", "0.159.9", true},
		{"1.0.0", "1.0.0-alpha.2", true},
		{"1.0.0-alpha.2", "1.0.0", false},
		{"v1.18.34", "1.18.32", true},
		{"2.0", "1.9.9", true},
	}
	for _, c := range cases {
		require.Equal(t, c.want, Newer(c.a, c.b), "%s > %s", c.a, c.b)
	}
}

// fakeCLI writes an executable script that prints version on --version and
// records its update call in a marker file.
func fakeCLI(t *testing.T, dir, name, version string) string {
	t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo '" + name + " " + version + " (fake)'; exit 0; fi\n" +
		"echo \"$@\" > " + filepath.Join(dir, name+".updated") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
	return filepath.Join(dir, name)
}

func TestCheckAndInstall(t *testing.T) {
	dir := t.TempDir()
	fakeCLI(t, dir, "old", "1.2.3")
	fakeCLI(t, dir, "current", "4.5.6")
	t.Setenv("PATH", dir)

	latest := func(v string) func(context.Context, string) (string, error) {
		return func(context.Context, string) (string, error) { return v, nil }
	}
	saved := CLIs
	t.Cleanup(func() { CLIs = saved })
	CLIs = []CLI{
		{Name: "Old", Bin: "old", Latest: latest("1.3.0"), UpdateArgs: []string{"update"}},
		{Name: "Current", Bin: "current", Latest: latest("4.5.6"), UpdateArgs: []string{"update"}},
		{Name: "Missing", Bin: "missing", Latest: latest("9.9.9"), UpdateArgs: []string{"update"}},
	}

	updates, errs := Check(t.Context())
	require.Empty(t, errs)
	require.Len(t, updates, 1)
	require.Equal(t, "Old", updates[0].Name)
	require.Equal(t, "1.2.3", updates[0].Current)
	require.Equal(t, "1.3.0", updates[0].Latest)

	require.NoError(t, Install(t.Context(), updates[0]))
	got, err := os.ReadFile(filepath.Join(dir, "old.updated"))
	require.NoError(t, err)
	require.Equal(t, "update\n", string(got))
}

func TestDeclineAndAutoCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cli-updates.json")
	saved := statePath
	t.Cleanup(func() { statePath = saved })
	statePath = func() string { return path }

	require.True(t, StartAutoCheck())
	require.False(t, StartAutoCheck(), "second check within the interval")

	u := []Update{{Name: "Codex", Bin: "codex", Current: "0.159.2", Latest: "0.159.3"}}
	require.Len(t, WithoutDeclined(u), 1)
	Decline(u)
	require.Empty(t, WithoutDeclined(u))

	// A newer release than the declined one is offered again.
	u[0].Latest = "0.160.0"
	require.Len(t, WithoutDeclined(u), 1)

	// Declining kept the last check time.
	s := loadState()
	require.WithinDuration(t, time.Now(), s.CheckedAt, time.Minute)
}
