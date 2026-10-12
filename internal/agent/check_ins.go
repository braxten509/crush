package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
)

// Check-ins: while a background job or sub-agent runs, Crush wakes the
// session's agent every options.check_in_minutes (default 2) with a short
// progress note, so it can tell the user how far along the work is. Each
// note is delivered like a finished job's result. The next one waits until
// the agent has had the previous one, so they never pile up behind a busy
// agent. Services never get check-ins.

// checkInTailLines and checkInTailBytes bound the job output quoted in a
// check-in.
const (
	checkInTailLines = 15
	checkInTailBytes = 1500
)

func (h *taskHub) checkInInterval() time.Duration {
	if h == nil || h.c == nil || h.c.cfg == nil {
		return 0
	}
	return h.c.cfg.Config().Options.GetCheckInInterval()
}

func (h *taskHub) isClosing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closing
}

// runCheckIns sends note to the session every check-in interval until done
// is closed, the context ends, or Crush shuts down.
func (h *taskHub) runCheckIns(ctx context.Context, sessionID string, done <-chan struct{}, note func(elapsed time.Duration) string) {
	send := func(ctx context.Context, prompt string) error {
		_, err := h.c.Run(ctx, sessionID, prompt)
		return err
	}
	if err := checkInLoop(ctx, h.checkInInterval(), done, h.isClosing, note, send); err != nil {
		slog.Warn("Progress check-in failed", "session", sessionID, "error", err)
	}
}

// checkInLoop waits interval, sends a note, and repeats; each wait starts
// after the previous note was handled. It returns when done is closed, the
// context ends, closing reports true, or a send fails (that error).
func checkInLoop(ctx context.Context, interval time.Duration, done <-chan struct{}, closing func() bool, note func(elapsed time.Duration) string, send func(context.Context, string) error) error {
	if interval <= 0 {
		return nil
	}
	started := time.Now()
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		case <-timer.C:
		}
		select {
		case <-done:
			return nil
		default:
		}
		if closing() {
			return nil
		}
		if err := send(ctx, note(time.Since(started).Round(time.Second))); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		timer.Reset(interval)
	}
}

// checkInNotification is a task-result message with status running, so the
// chat shows it as a one-line notice like other job messages.
func checkInNotification(id, name string, elapsed time.Duration, progress string) string {
	progress = strings.TrimSpace(progress)
	if progress == "" {
		progress = "(no output yet)"
	}
	return fmt.Sprintf("<%s>\n<task-id>%s</task-id>\n<name>%s</name>\n<status>%s</status>\n<result>\nAutomatic progress check: still running after %s. This is not a finished result.\nLatest progress:\n%s\n</result>\n</%s>",
		TaskNotificationTag, id, name, TaskRunning, elapsed, progress, TaskNotificationTag)
}

// backgroundJobCheckIn quotes the end of a running job's output.
func backgroundJobCheckIn(job *shell.BackgroundShell, elapsed time.Duration) string {
	stdout, stderr, _, _ := job.GetOutput()
	name := BackgroundProcessName
	if job.Description != "" {
		name += ": " + job.Description
	}
	return checkInNotification(job.ID, name, elapsed, tailText(strings.TrimSpace(stdout+"\n"+stderr)))
}

// subAgentCheckIn quotes the sub-agent's latest reply so far.
func (h *taskHub) subAgentCheckIn(ctx context.Context, t Task, elapsed time.Duration) string {
	var latest string
	if h.c != nil && h.c.messages != nil {
		if msgs, err := h.c.messages.List(ctx, t.ChildID); err == nil {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == message.Assistant {
					if text := strings.TrimSpace(msgs[i].Content().Text); text != "" {
						latest = text
						break
					}
				}
			}
		}
	}
	return checkInNotification(t.ID, t.Name, elapsed, tailText(latest))
}

// tailText keeps the last checkInTailLines lines, at most checkInTailBytes.
func tailText(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > checkInTailLines {
		lines = lines[len(lines)-checkInTailLines:]
	}
	s = strings.Join(lines, "\n")
	if len(s) > checkInTailBytes {
		s = "…" + s[len(s)-checkInTailBytes:]
	}
	return s
}

// checkInInstructions tells the agent what check-ins are and how to answer
// them. It is empty when check-ins are off.
func (h *taskHub) checkInInstructions() string {
	return fmtCheckIn(h.checkInInterval())
}

func fmtCheckIn(interval time.Duration) string {
	if interval <= 0 {
		return ""
	}
	every := "every minute"
	if minutes := int(interval / time.Minute); minutes != 1 {
		every = fmt.Sprintf("every %d minutes", minutes)
	}
	return fmt.Sprintf(` While your background jobs (not services) and sub-agents run, Crush also sends you a <%s> message with status %s %s: an automatic progress check with the work's latest output. Answer each one with a short progress update for the user (percent of the agreed work done and what is running now), then keep working or end your turn. Don't stop, restart or poll the work because of a check-in; act only if its output shows the work is broken.`, TaskNotificationTag, TaskRunning, every)
}
