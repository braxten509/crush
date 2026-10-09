package notification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaySoundFallback(t *testing.T) {
	t.Parallel()
	for _, sound := range []Sound{SoundQuestion, SoundComplete} {
		t.Run(string(sound), func(t *testing.T) {
			t.Parallel()
			var calls [][]string
			playSound(context.Background(), "linux", sound, func(_ context.Context, name string, args ...string) error {
				calls = append(calls, append([]string{name}, args...))
				if name == "canberra-gtk-play" {
					return errors.New("no display")
				}
				return nil
			})
			require.Equal(t, [][]string{
				{"canberra-gtk-play", "--id", string(sound), "--description", "Crush"},
				{"pw-play", "/usr/share/sounds/freedesktop/stereo/" + string(sound) + ".oga"},
			}, calls)
		})
	}
}

func TestPlaySoundStopsAfterSuccess(t *testing.T) {
	t.Parallel()
	calls := 0
	playSound(context.Background(), "linux", SoundComplete, func(context.Context, string, ...string) error {
		calls++
		return nil
	})
	require.Equal(t, 1, calls)
}

func TestPlaySoundMissingPlayers(t *testing.T) {
	t.Parallel()
	calls := 0
	playSound(context.Background(), "linux", SoundComplete, func(context.Context, string, ...string) error {
		calls++
		return errors.New("player missing")
	})
	require.Equal(t, 3, calls)
}

func TestPlaySoundCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	playSound(ctx, "linux", SoundComplete, func(context.Context, string, ...string) error {
		t.Fatal("cancelled playback must not start a player")
		return nil
	})
}

func TestSoundCommandsStaySilentDuringTests(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "played")
	for _, name := range []string{"canberra-gtk-play", "pw-play", "paplay", "afplay"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho played > \"$CRUSH_TEST_SOUND_MARKER\"\n"), 0o755))
	}
	t.Setenv("PATH", dir)
	t.Setenv("CRUSH_TEST_SOUND_MARKER", marker)
	for _, sound := range []Sound{SoundQuestion, SoundComplete} {
		require.Nil(t, PlaySound(sound)())
	}
	require.NoFileExists(t, marker, "UI tests must never run a real sound player")
}
