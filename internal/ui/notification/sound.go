package notification

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Sound identifies an audible notification independently of desktop popups.
type Sound string

const (
	SoundQuestion Sound = "message-new-instant"
	SoundComplete Sound = "complete"
)

// PlaySound plays a short desktop sound without blocking the UI or requiring
// terminal bell support. Platforms without a sound player silently skip it.
func PlaySound(sound Sound) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		playSound(ctx, runtime.GOOS, sound, func(ctx context.Context, name string, args ...string) error {
			return exec.CommandContext(ctx, name, args...).Run()
		})
		return nil
	}
}

func playSound(ctx context.Context, platform string, sound Sound, run func(context.Context, string, ...string) error) {
	var commands [][]string
	switch platform {
	case "linux":
		// Prefer the desktop's sound theme; fall back to installed standard
		// sounds when Canberra or its display connection is unavailable.
		file := "/usr/share/sounds/freedesktop/stereo/" + string(sound) + ".oga"
		commands = [][]string{
			{"canberra-gtk-play", "--id", string(sound), "--description", "Crush"},
			{"pw-play", file},
			{"paplay", file},
		}
	case "darwin":
		commands = [][]string{{"afplay", "/System/Library/Sounds/Glass.aiff"}}
	}
	for _, command := range commands {
		if ctx.Err() != nil {
			return
		}
		if err := run(ctx, command[0], command[1:]...); err == nil {
			return
		} else {
			slog.Debug("Notification sound player unavailable", "player", command[0], "error", err)
		}
	}
}
