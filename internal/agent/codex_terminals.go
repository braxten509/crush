package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// One result owed to the chat that backgrounded this Codex command.
type codexCommand struct {
	session, command string
	pid              int
	cancel           context.CancelFunc
	delivering       bool
}

func (h *taskHub) watchCodexCommand(session, command string, pid int) func(string, string) {
	parent, release := h.reserveFollowUp(session)
	ctx, cancel := context.WithCancel(parent)
	job := &codexCommand{session: session, command: command, pid: pid, cancel: cancel}
	h.mu.Lock()
	if h.codexCommands == nil {
		h.codexCommands = make(map[*codexCommand]bool)
	}
	h.codexCommands[job] = true
	if h.closing {
		cancel()
	}
	h.mu.Unlock()
	done := sync.OnceFunc(func() {
		h.mu.Lock()
		delete(h.codexCommands, job)
		h.mu.Unlock()
		release()
	})
	unwatch := context.AfterFunc(ctx, done)
	return func(status, output string) {
		go func() {
			defer done()
			defer cancel()
			if !unwatch() || ctx.Err() != nil {
				return
			}
			accept := h.acceptHandoff(session, func() { job.delivering = true })
			defer accept.Close()
			body := fmt.Sprintf("Codex background command: %s\n%s", command, output)
			if len(body) > taskResultLimit {
				body = body[:taskResultLimit] + "\n[result truncated]"
			}
			notice := taskResult{Name: BackgroundProcessName, Status: status, Body: strings.TrimSpace(body)}.String()
			if _, err := h.c.RunAccepted(ctx, accept, session, notice); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Reporting a Codex background command", "session", session, "error", err)
			}
		}()
	}
}

// Closing a chat suppresses all its follow-ups. Stopping one process already
// sends the user's stop notice, so only that command's follow-up is silenced.
func (h *taskHub) silenceCodexCommands(session string, pid int, command string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var match *codexCommand
	matches := 0
	for job := range h.codexCommands {
		if job.session != session {
			continue
		}
		if pid == 0 {
			job.cancel()
		} else if job.pid == pid {
			job.cancel()
			return
		} else if job.pid == 0 && command != "" && job.command == command {
			match = job
			matches++
		}
	}
	// Some Codex versions omit the OS pid. Identical commands in other
	// terminals must still report their results.
	if matches == 1 {
		match.cancel()
	}
}
