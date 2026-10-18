package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/google/uuid"
)

const managedJobEnv = "CRUSH_MANAGED_BACKGROUND_JOB"

// BackgroundRequest uses the same shell tools and policies as native agents.
type BackgroundRequest struct {
	Service    bool   `json:"service,omitempty"`
	Command    string `json:"command,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	Name       string `json:"name,omitempty"`
	OutputID   string `json:"output_id,omitempty"`
	StopID     string `json:"stop_id,omitempty"`
}

func (h *taskHub) background(req TaskRequest) TaskReply {
	ctx, release := h.reserveFollowUp(req.Session)
	transferred := false
	defer func() {
		if !transferred {
			release()
		}
	}()
	fail := func(err error) TaskReply { return TaskReply{Error: err.Error()} }
	if h.c == nil || h.c.sessions == nil || req.Session == "" {
		return fail(fmt.Errorf("a valid Crush session is required"))
	}
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return fail(fmt.Errorf("unknown session %q", req.Session))
	}
	params := req.Background
	operations := 0
	for _, value := range []string{params.Command, params.OutputID, params.StopID} {
		if strings.TrimSpace(value) != "" {
			operations++
		}
	}
	if operations != 1 {
		return fail(fmt.Errorf("provide exactly one command, output ID, or stop ID"))
	}
	if params.Service && strings.TrimSpace(params.Command) == "" {
		return fail(fmt.Errorf("service requires a command to start"))
	}
	options := h.c.cfg.Config().Options
	var tool fantasy.AgentTool
	var input any
	id := params.OutputID
	if params.StopID != "" {
		id = params.StopID
	}
	if id != "" {
		h.mu.Lock()
		owner := h.backgroundOwners[id]
		h.mu.Unlock()
		if owner != req.Session {
			return fail(fmt.Errorf("no background job %q in this session", id))
		}
		if params.StopID != "" {
			tool = shelltool.NewJobKillTool()
			input = tools.JobKillParams{ShellID: id}
		} else {
			tool = shelltool.NewJobOutputTool(options.DataDirectory)
			input = tools.JobOutputParams{ShellID: id}
		}
	} else {
		tool = shelltool.NewBashTool(h.c.permissions, h.c.cfg.WorkingDir(), options.DataDirectory, options.Attribution, "", reportStartedJob)
		input = tools.BashParams{Command: params.Command, WorkingDir: params.WorkingDir, Description: params.Name, RunInBackground: true}
	}
	tool = guardedTool{tool}
	if configured := h.c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(configured) > 0 {
		tool = newHookedTool(tool, hooks.NewRunner(configured, h.c.cfg.WorkingDir(), h.c.cfg.WorkingDir()))
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, req.Session)
	ctx = tools.WithWatchedJob(ctx)
	callID := uuid.NewString()
	ctx = context.WithValue(ctx, tools.ShellEnvContextKey, append(h.env(req.Session), managedJobEnv+"="+callID))
	data, err := json.Marshal(input)
	if err != nil {
		return fail(err)
	}
	call := fantasy.ToolCall{ID: callID, Name: tool.Info().Name, Input: string(data)}
	response, err := tool.Run(ctx, call)
	if err != nil {
		return fail(err)
	}
	if id == "" {
		// Save the tool result so edits made after the calling CLI turn ends
		// still receive the normal file-change review.
		h.recordBackgroundTool(ctx, req.Session, call, response)
	}
	if id != "" || response.IsError {
		return TaskReply{Background: &response}
	}
	var metadata tools.BashResponseMetadata
	if err := json.Unmarshal([]byte(response.Metadata), &metadata); err != nil {
		return fail(err)
	}
	if metadata.ShellID != "" {
		job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
		if !ok {
			return fail(fmt.Errorf("background job %q disappeared", metadata.ShellID))
		}
		response.Content = fmt.Sprintf("Started background job %s. Use crush bg --output %s to read output or crush bg --stop %s to stop it. Crush will report completion; keep working without waiting.", job.ID, job.ID, job.ID)
		if params.Service {
			response.Content += " This is a service; it will not delay completion notifications."
		}
		transferred = true
		h.watchJob(ctx, release, req.Session, callID, job, params.Service)
	}
	return TaskReply{Background: &response}
}

// reportStartedJob has the hub of the turn that started a background job
// report on it until it ends. It is the bash tool's [tools.JobStarted].
func reportStartedJob(env []string, sessionID, marker string, job *shell.BackgroundShell) bool {
	h := hubForJob(env, sessionID)
	if h == nil {
		return false
	}
	ctx, release := h.reserveFollowUp(sessionID)
	h.watchJob(ctx, release, sessionID, marker, job, false)
	return true
}

// hubForJob finds the hub of the Crush whose agent turn started the job,
// from the shell environment that Crush gave the turn. Sub-agent turns
// carry no session there, so their jobs aren't reported to a chat that
// has already finished.
func hubForJob(env []string, sessionID string) *taskHub {
	var dir, session string
	for _, variable := range env {
		if value, ok := strings.CutPrefix(variable, TasksDirEnv+"="); ok {
			dir = value
		}
		if value, ok := strings.CutPrefix(variable, TasksSessionEnv+"="); ok {
			session = value
		}
	}
	if dir == "" || session == "" || session != sessionID {
		return nil
	}
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		if h.dir == dir && h.c != nil {
			return h
		}
	}
	return nil
}

// watchJob is the one place background jobs are tracked, whichever way they
// started: it lists the job as the session's, saves it so a restart can
// report it, sends progress check-ins, and tells the agent when it ends,
// unless the agent stopped it itself. marker is the environment value the
// job's processes carry. release is called once the job is reported.
func (h *taskHub) watchJob(ctx context.Context, release func(), sessionID, marker string, job *shell.BackgroundShell, service bool) {
	h.mu.Lock()
	if h.backgroundOwners == nil {
		h.backgroundOwners = make(map[string]string)
	}
	for old := range h.backgroundOwners {
		if _, exists := shell.GetBackgroundShellManager().Get(old); !exists {
			delete(h.backgroundOwners, old)
			delete(h.backgroundServices, old)
		}
	}
	h.backgroundOwners[job.ID] = sessionID
	if h.backgroundServices == nil {
		h.backgroundServices = make(map[string]bool)
	}
	h.backgroundServices[job.ID] = service
	if h.backgroundShells == nil {
		h.backgroundShells = make(map[string]*shell.BackgroundShell)
	}
	h.backgroundShells[marker] = job
	if !service {
		if h.jobHandoffs == nil {
			h.jobHandoffs = make(map[string]bool)
		}
		h.jobHandoffs[marker] = true
	}
	h.mu.Unlock()
	// The job's owner is recorded by the hook (the backend keeps it).
	workStarted(WorkStart{Kind: WorkJob, ID: job.ID, SessionID: sessionID})
	if h.saves(ctx) {
		h.rememberJob(job, sessionID, service)
	}
	spillDir := h.c.cfg.Config().Options.DataDirectory
	go func() {
		defer release()
		defer func() {
			h.mu.Lock()
			delete(h.backgroundShells, marker)
			delete(h.backgroundServices, job.ID)
			delete(h.jobHandoffs, marker)
			delete(h.interrupting, job.ID)
			h.mu.Unlock()
		}()
		removeCheckIn := func() {}
		if !service {
			removeCheckIn = h.addCheckIn(sessionID, backgroundJobWork(ctx, job))
		}
		waited := job.WaitContext(ctx)
		removeCheckIn()
		stoppedByAgent := tools.StoppedByAgent(job.ID)
		if !waited {
			return
		}
		if stoppedByAgent {
			h.forgetJob(job.ID)
			return
		}
		// InterruptJob's kill counts as a shutdown of this one job.
		closing := func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.closing || h.interrupting[job.ID] }
		if closing() {
			// Shutdown or InterruptJob killed the job; the next resume
			// reports it.
			h.interruptJob(job)
			return
		}
		notice := backgroundJobNotification(job, spillDir)
		accept := h.acceptHandoff(sessionID, func() { delete(h.jobHandoffs, marker) })
		_, err := h.c.RunAccepted(ctx, accept, sessionID, notice)
		accept.Close()
		if err != nil {
			slog.Error("Background job notification failed", "job", job.ID, "error", err)
			if closing() {
				h.interruptJob(job)
				return
			}
		}
		h.forgetJob(job.ID)
	}()
}

func backgroundJobNotification(job *shell.BackgroundShell, spillDir string) string {
	stdout, stderr, _, err := job.GetOutput()
	status := "completed"
	if err != nil {
		status = "failed"
	}
	output := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil {
		output += "\n" + err.Error()
	}
	output = shelltool.TruncateOutput(output, spillDir)
	return taskResult{Name: BackgroundProcessName, Status: status, Body: fmt.Sprintf("Job %s (%s) ended with exit code %d.\n%s", job.ID, job.Description, shell.ExitCode(err), output)}.String()
}

func (h *taskHub) recordBackgroundTool(ctx context.Context, sessionID string, call fantasy.ToolCall, response fantasy.ToolResponse) {
	if h.c.messages == nil {
		return
	}
	_, err := h.c.messages.Create(ctx, sessionID, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: call.ID, Name: call.Name, Input: call.Input, Finished: true},
		message.Finish{Reason: message.FinishReasonToolUse, Time: time.Now().Unix()},
	}})
	if err != nil {
		slog.Error("Cannot record background command", "error", err)
		return
	}
	metadata, review := filechange.TakeReview(response.Metadata)
	result := message.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: response.Content, Metadata: metadata, Review: review, IsError: response.IsError}
	if _, err := h.c.messages.Create(ctx, sessionID, message.CreateMessageParams{Role: message.Tool, Parts: []message.ContentPart{result}}); err != nil {
		slog.Error("Cannot record background result", "error", err)
		return
	}
	shelltool.PersistBackgroundReview(ctx, h.c.messages, sessionID, result)
}

func managedBackgroundJob(marker string) *shell.BackgroundShell {
	if marker == "" {
		return nil
	}
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, hub := range hubs {
		hub.mu.Lock()
		job := hub.backgroundShells[marker]
		hub.mu.Unlock()
		if job != nil {
			return job
		}
	}
	return nil
}

func managedBackgroundProcesses() []Process {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	var processes []Process
	for _, hub := range hubs {
		hub.mu.Lock()
		for marker, job := range hub.backgroundShells {
			if !job.IsDone() {
				processes = append(processes, Process{JobID: job.ID, Command: job.Command, Name: job.Description, Started: job.Started, Session: hub.backgroundOwners[job.ID], marker: marker})
			}
		}
		hub.mu.Unlock()
	}
	return processes
}
