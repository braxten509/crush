package agent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/shell"
)

// Interrupted background jobs. A `crush bg` job runs inside Crush, so it ends
// when Crush quits or crashes. An interactive Crush saves each running job in
// the project's data directory until its completion reaches the agent. The
// next Crush in that project tells each owning session which jobs were cut
// off, so the agent checks on them and starts them again instead of waiting
// for a completion that will never come.

const savedJobsFile = "background-jobs.json"

// Limits on what an interrupted job's report repeats.
const (
	savedJobOutput  = 4000
	savedJobCommand = 600
)

type savedJob struct {
	Service    bool      `json:"service,omitempty"`
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Name       string    `json:"name,omitempty"`
	Command    string    `json:"command"`
	WorkingDir string    `json:"working_dir,omitempty"`
	Started    time.Time `json:"started"`
	// Owner is the process ID of the Crush running the job; job IDs restart
	// with every Crush, so a job is the pair of both.
	Owner int `json:"owner"`
	// Interrupted is when Crush shut down with the job running; it stays
	// zero after a crash.
	Interrupted time.Time `json:"interrupted,omitzero"`
	// Output is the end of what the job printed before shutdown.
	Output string `json:"output,omitempty"`
}

func (h *taskHub) editSavedJobs(edit func([]savedJob) []savedJob) error {
	return editTaskFile(h.savedDataPath(savedJobsFile), edit)
}

func ownJob(id string) func(savedJob) bool {
	self := os.Getpid()
	return func(s savedJob) bool { return s.Owner == self && s.ID == id }
}

// rememberJob saves a running job of the session.
func (h *taskHub) rememberJob(job *shell.BackgroundShell, sessionID string, service bool) {
	entry := savedJob{
		ID: job.ID, SessionID: sessionID, Name: job.Description, Command: job.Command,
		WorkingDir: job.WorkingDir, Started: job.Started, Owner: os.Getpid(), Service: service,
	}
	err := h.editSavedJobs(func(saved []savedJob) []savedJob {
		return append(slices.DeleteFunc(saved, ownJob(job.ID)), entry)
	})
	if err != nil {
		slog.Warn("Failed to save a running background job", "job", job.ID, "error", err)
	}
}

// forgetJob drops a job whose completion was handed over.
func (h *taskHub) forgetJob(id string) {
	if err := h.editSavedJobs(func(saved []savedJob) []savedJob {
		return slices.DeleteFunc(saved, ownJob(id))
	}); err != nil {
		slog.Warn("Failed to drop a finished background job", "job", id, "error", err)
	}
}

// interruptJob records that shutdown cut the job off, with its last output.
func (h *taskHub) interruptJob(job *shell.BackgroundShell) {
	stdout, stderr, _, _ := job.GetOutput()
	output := strings.TrimSpace(stdout + "\n" + stderr)
	if len(output) > savedJobOutput {
		output = "…" + strings.ToValidUTF8(output[len(output)-savedJobOutput:], "")
	}
	now := time.Now()
	err := h.editSavedJobs(func(saved []savedJob) []savedJob {
		for i := range saved {
			if ownJob(job.ID)(saved[i]) {
				saved[i].Interrupted = now
				saved[i].Output = output
			}
		}
		return saved
	})
	if err != nil {
		slog.Warn("Failed to mark an interrupted background job", "job", job.ID, "error", err)
	}
}

// claimInterruptedJobs takes the saved jobs whose Crush is gone, dropping
// them from the file so only one Crush reports them, along with ones too old
// to matter. Jobs of a Crush that still runs, this one included, stay.
func (h *taskHub) claimInterruptedJobs() ([]savedJob, error) {
	self := os.Getpid()
	var claimed []savedJob
	err := h.editSavedJobs(func(saved []savedJob) []savedJob {
		kept := saved[:0]
		for _, s := range saved {
			switch {
			case s.Owner == self || taskProcessAlive(s.Owner):
				kept = append(kept, s)
			case time.Since(cmp.Or(s.Interrupted, s.Started)) > resumeWindow:
				slog.Info("Not reporting an old interrupted background job", "job", s.ID, "name", s.Name)
			default:
				claimed = append(claimed, s)
			}
		}
		return kept
	})
	return claimed, err
}

// reportInterruptedJobs tells each session which of its jobs an earlier
// Crush cut off, one message per session.
func (h *taskHub) reportInterruptedJobs(ctx context.Context) {
	claimed, err := h.claimInterruptedJobs()
	if err != nil {
		slog.Warn("Failed to read interrupted background jobs", "error", err)
		return
	}
	bySession := map[string][]savedJob{}
	var order []string
	for _, s := range claimed {
		if _, seen := bySession[s.SessionID]; !seen {
			order = append(order, s.SessionID)
		}
		bySession[s.SessionID] = append(bySession[s.SessionID], s)
	}
	for _, sessionID := range order {
		jobs := bySession[sessionID]
		if _, err := h.c.sessions.Get(ctx, sessionID); err != nil {
			slog.Warn("Dropping interrupted background jobs of a missing session", "session", sessionID, "error", err)
			continue
		}
		go func() {
			if _, err := h.c.Run(ctx, sessionID, interruptedJobsNotification(jobs)); err != nil {
				slog.Error("Failed to report interrupted background jobs", "session", sessionID, "error", err)
				return
			}
			slog.Info("Reported interrupted background jobs", "session", sessionID, "jobs", len(jobs))
		}()
	}
}

func interruptedJobsNotification(jobs []savedJob) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Crush stopped while %d of your crush bg jobs were still running, which ended them. Crush will never report their completion:\n", len(jobs))
	for _, s := range jobs {
		name := cmp.Or(s.Name, "no name")
		command := s.Command
		if len(command) > savedJobCommand {
			command = strings.ToValidUTF8(command[:savedJobCommand], "") + "…"
		}
		fmt.Fprintf(&b, "\n- Job %s (%s), started %s", s.ID, name, s.Started.Local().Format("2006-01-02 15:04"))
		if s.Interrupted.IsZero() {
			b.WriteString(", ended when Crush stopped unexpectedly")
		} else {
			fmt.Fprintf(&b, ", ended when Crush closed at %s", s.Interrupted.Local().Format("15:04"))
		}
		if s.Service {
			fmt.Fprint(&b, "\n  Long-running service; use crush bg --service if restarting it.")
		}
		if s.WorkingDir != "" {
			fmt.Fprintf(&b, "\n  Directory: %s", s.WorkingDir)
		}
		fmt.Fprintf(&b, "\n  Command:\n%s", indentLines(command, "    "))
		if s.Output != "" {
			fmt.Fprintf(&b, "\n  Last output:\n%s", indentLines(s.Output, "    "))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nWork these jobs started outside Crush, such as a systemd unit or a detached process, may still be running or may already have finished. Check the real state and results of each job now. Start again with crush bg whatever still needs to run, or start a crush bg job that waits for the outside work, so Crush reports its completion. Don't end your turn waiting for these jobs to report.")
	return fmt.Sprintf("<%s>\n<name>%s</name>\n<status>%s</status>\n<result>\n%s\n</result>\n</%s>", TaskNotificationTag, BackgroundProcessName, TaskStopped, b.String(), TaskNotificationTag)
}

func indentLines(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
