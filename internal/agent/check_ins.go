package agent

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
)

// Check-ins: while background jobs or sub-agents run, Crush wakes the
// session's agent every options.check_in_minutes (default 2) with one
// progress note listing all of them, so it can tell the user how far along
// the work is. Each note is delivered like a finished job's result. The next
// one waits until the agent has had the previous one, so they never pile up
// behind a busy agent. Running services are named in one line at the end;
// when only services run, the note still comes, serviceOnlyCheckIns times,
// then stops until other work starts.

// checkInTailLines and checkInTotalBytes bound the output quoted in one
// check-in; several running items share them.
const (
	checkInTailLines  = 15
	checkInTotalBytes = 3000
	// serviceOnlyCheckIns is how many notes in a row a session gets while
	// only services run (owner, 2026-10-19: three, then quiet).
	serviceOnlyCheckIns = 3
)

func (h *taskHub) checkInInterval() time.Duration {
	if h == nil {
		return 0
	}
	if h.checkInEvery > 0 {
		return h.checkInEvery
	}
	if h.c == nil || h.c.cfg == nil {
		return 0
	}
	return h.c.cfg.Config().Options.GetCheckInInterval()
}

func (h *taskHub) isClosing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closing
}

// checkInWork is one running job, service or sub-agent a session's
// check-ins list.
type checkInWork struct {
	id, name string
	started  time.Time
	// service work is only named, without its output.
	service bool
	// ctx is the work's own; a check-in is sent under one still live.
	ctx      context.Context
	progress func() string
}

// sessionCheckIns is the one check-in loop of a session with running work.
// done closes when the last work is removed.
type sessionCheckIns struct {
	work map[int]checkInWork
	done chan struct{}
	// serviceNotes counts the notes sent in a row while only services ran;
	// other work starting resets it.
	serviceNotes int
}

// addCheckIn lists work in its session's check-ins until remove is called,
// starting the session's check-in loop if none runs.
func (h *taskHub) addCheckIn(sessionID string, w checkInWork) (remove func()) {
	if h.checkInInterval() <= 0 {
		return func() {}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.checkIns == nil {
		h.checkIns = make(map[string]*sessionCheckIns)
	}
	s := h.checkIns[sessionID]
	if s == nil {
		s = &sessionCheckIns{work: make(map[int]checkInWork), done: make(chan struct{})}
		h.checkIns[sessionID] = s
		go h.runCheckIns(sessionID, s)
	}
	h.checkInSeq++
	key := h.checkInSeq
	s.work[key] = w
	if !w.service {
		s.serviceNotes = 0
	}
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := s.work[key]; !ok {
			return
		}
		delete(s.work, key)
		if len(s.work) == 0 {
			close(s.done)
			if h.checkIns[sessionID] == s {
				delete(h.checkIns, sessionID)
			}
		}
	}
}

// runCheckIns sends the session one note about all its running work every
// check-in interval until the work is done or Crush shuts down.
func (h *taskHub) runCheckIns(sessionID string, s *sessionCheckIns) {
	defer func() {
		// Work added later starts a fresh loop.
		h.mu.Lock()
		if h.checkIns[sessionID] == s {
			delete(h.checkIns, sessionID)
		}
		h.mu.Unlock()
	}()
	var sendCtx context.Context
	note := func(time.Duration) string {
		var work []checkInWork
		work, sendCtx = h.checkInSnapshot(s)
		if !h.serviceNoteDue(s, work) {
			return ""
		}
		return checkInNotification(work, time.Now())
	}
	send := func(_ context.Context, prompt string) {
		if prompt == "" || sendCtx == nil {
			return
		}
		if _, err := h.c.Run(sendCtx, sessionID, prompt); err != nil {
			slog.Warn("Progress check-in failed", "session", sessionID, "error", err)
		}
	}
	checkInLoop(context.Background(), h.checkInInterval(), s.done, h.isClosing, note, send)
}

// checkInSnapshot lists the session's running work, oldest first, and a
// context of one that is still live.
func (h *taskHub) checkInSnapshot(s *sessionCheckIns) ([]checkInWork, context.Context) {
	h.mu.Lock()
	work := slices.Collect(maps.Values(s.work))
	h.mu.Unlock()
	slices.SortFunc(work, func(a, b checkInWork) int { return a.started.Compare(b.started) })
	for _, w := range work {
		if w.ctx.Err() == nil {
			return work, w.ctx
		}
	}
	return work, nil
}

// serviceNoteDue reports whether a note about work is sent: always while
// other work runs, and serviceOnlyCheckIns times in a row while only
// services do.
func (h *taskHub) serviceNoteDue(s *sessionCheckIns, work []checkInWork) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if slices.ContainsFunc(work, func(w checkInWork) bool { return !w.service }) {
		s.serviceNotes = 0
		return true
	}
	if s.serviceNotes >= serviceOnlyCheckIns {
		return false
	}
	s.serviceNotes++
	return true
}

// checkInLoop waits interval, sends a note, and repeats; each wait starts
// after the previous note was handled. A failed send waits for the next
// round, since the work it reports on is still running. It returns when
// done is closed, the context ends, or closing reports true.
func checkInLoop(ctx context.Context, interval time.Duration, done <-chan struct{}, closing func() bool, note func(elapsed time.Duration) string, send func(context.Context, string)) {
	if interval <= 0 {
		return
	}
	started := time.Now()
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-timer.C:
		}
		select {
		case <-done:
			return
		default:
		}
		if closing() {
			return
		}
		send(ctx, note(time.Since(started).Round(time.Second)))
		timer.Reset(interval)
	}
}

// checkInNotification is one task-result message with status running
// about all of a session's running work, so the chat shows it as a
// one-line notice like other job messages. Services are named in one line
// at the end. It is empty without work.
func checkInNotification(work []checkInWork, now time.Time) string {
	var tasks, services []checkInWork
	for _, w := range work {
		if w.service {
			services = append(services, w)
		} else {
			tasks = append(tasks, w)
		}
	}
	if len(tasks) == 0 {
		return serviceNotification(services, now)
	}
	// The work shares one budget, so the note stays the same size however
	// much is running.
	lines := max(1, checkInTailLines/len(tasks))
	bytes := max(1, checkInTotalBytes/len(tasks))
	var b strings.Builder
	name, id := fmt.Sprintf("%d background tasks", len(tasks)), ""
	if len(tasks) == 1 {
		name, id = tasks[0].name, tasks[0].id
		b.WriteString("Automatic progress check: still running. This is not a finished result.\n")
	} else {
		fmt.Fprintf(&b, "Automatic progress check: %d still running. These are not finished results.\n", len(tasks))
	}
	for _, w := range tasks {
		progress := tailText(strings.TrimSpace(w.progress()), lines, bytes)
		if progress == "" {
			progress = "(no output yet)"
		}
		fmt.Fprintf(&b, "\n[%s] %s, running %s\nLatest progress:\n%s\n", w.id, w.name, now.Sub(w.started).Round(time.Second), progress)
	}
	if len(services) > 0 {
		fmt.Fprintf(&b, "\n%s still running in the background: %s\n", servicesCount(len(services)), serviceList(services, now))
	}
	return taskResult{ID: id, Name: name, Status: string(TaskRunning), Body: strings.TrimSuffix(b.String(), "\n")}.String()
}

// serviceNotification is the note while only services run.
func serviceNotification(services []checkInWork, now time.Time) string {
	if len(services) == 0 {
		return ""
	}
	name, id := servicesCount(len(services)), ""
	if len(services) == 1 {
		name, id = services[0].name, services[0].id
	}
	stop := "Stop any you no longer need with crush bg --stop <ID>."
	if slices.ContainsFunc(services, func(w checkInWork) bool { return strings.HasPrefix(w.id, "PID ") }) {
		// Strays have no job to stop; see stray_check_ins.go.
		stop = "Stop any you no longer need with crush bg --stop <ID>, or kill <PID> for one listed by PID."
	}
	body := fmt.Sprintf("Automatic check: %s still running in the background: %s\n%s", servicesCount(len(services)), serviceList(services, now), stop)
	return taskResult{ID: id, Name: name, Status: string(TaskRunning), Body: body}.String()
}

func servicesCount(n int) string {
	if n == 1 {
		return "1 service"
	}
	return fmt.Sprintf("%d services", n)
}

// serviceList names each service and how long it has run.
func serviceList(services []checkInWork, now time.Time) string {
	names := make([]string, len(services))
	for i, w := range services {
		names[i] = fmt.Sprintf("[%s] %s, up %s", w.id, w.name, now.Sub(w.started).Round(time.Second))
	}
	return strings.Join(names, "; ")
}

// backgroundJobWork is a running job as its session's check-ins list it.
func backgroundJobWork(ctx context.Context, job *shell.BackgroundShell, service bool) checkInWork {
	name := BackgroundProcessName
	if job.Description != "" {
		name += ": " + job.Description
	}
	return checkInWork{id: "Job " + job.ID, name: name, started: job.Started, service: service, ctx: ctx, progress: func() string {
		stdout, stderr, _, _ := job.GetOutput()
		return stdout + "\n" + stderr
	}}
}

// subAgentProgress is the sub-agent's latest reply so far.
func (h *taskHub) subAgentProgress(ctx context.Context, t Task) string {
	if h.c == nil || h.c.messages == nil {
		return ""
	}
	msgs, err := h.c.messages.List(ctx, t.ChildID)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.Assistant {
			if text := strings.TrimSpace(msgs[i].Content().Text); text != "" {
				return text
			}
		}
	}
	return ""
}

// tailText keeps the last maxLines lines, at most maxBytes.
func tailText(s string, maxLines, maxBytes int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	s = strings.Join(lines, "\n")
	if len(s) > maxBytes {
		s = "…" + strings.ToValidUTF8(s[len(s)-maxBytes:], "")
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
	return fmt.Sprintf(` While your background jobs and sub-agents run, Crush also sends you one <%s> message with status %s %s that lists all of them: an automatic progress check with each one's latest output. Answer each one with a short progress update for the user (percent of the agreed work done and what is running now), then keep working or end your turn. Don't stop, restart or poll the work because of a check-in; act only if its output shows the work is broken. The same message names your running services; while only services run it comes %d times, then stops until other work starts. Stop each service with crush bg --stop <ID> once it is no longer needed.`, TaskNotificationTag, TaskRunning, every, serviceOnlyCheckIns)
}
