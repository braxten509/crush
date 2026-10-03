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
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/google/uuid"
)

const managedJobEnv = "CRUSH_MANAGED_BACKGROUND_JOB"

// BackgroundRequest uses the same shell tools and policies as native agents.
type BackgroundRequest struct {
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
			tool = tools.NewJobKillTool()
			input = tools.JobKillParams{ShellID: id}
		} else {
			tool = tools.NewJobOutputTool(options.DataDirectory)
			input = tools.JobOutputParams{ShellID: id}
		}
	} else {
		tool = tools.NewBashTool(h.c.permissions, h.c.cfg.WorkingDir(), options.DataDirectory, options.Attribution, "")
		input = tools.BashParams{Command: params.Command, WorkingDir: params.WorkingDir, Description: params.Name, RunInBackground: true}
	}
	tool = guardedTool{tool}
	if configured := h.c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(configured) > 0 {
		tool = newHookedTool(tool, hooks.NewRunner(configured, h.c.cfg.WorkingDir(), h.c.cfg.WorkingDir()))
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, req.Session)
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
		h.mu.Lock()
		if h.backgroundOwners == nil {
			h.backgroundOwners = make(map[string]string)
		}
		for old := range h.backgroundOwners {
			if _, exists := shell.GetBackgroundShellManager().Get(old); !exists {
				delete(h.backgroundOwners, old)
			}
		}
		h.backgroundOwners[job.ID] = req.Session
		if h.backgroundShells == nil {
			h.backgroundShells = make(map[string]*shell.BackgroundShell)
		}
		h.backgroundShells[callID] = job
		h.mu.Unlock()
		response.Content = fmt.Sprintf("Started background job %s. Use crush bg --output %s to read output or crush bg --stop %s to stop it. Crush will report completion; keep working without waiting.", job.ID, job.ID, job.ID)
		transferred = true
		go func() {
			defer release()
			defer func() { h.mu.Lock(); delete(h.backgroundShells, callID); h.mu.Unlock() }()
			if !job.WaitContext(ctx) {
				return
			}
			notice := backgroundJobNotification(job, options.DataDirectory)
			if _, err := h.c.Run(ctx, req.Session, notice); err != nil {
				slog.Error("Background job notification failed", "job", job.ID, "error", err)
			}
		}()
	}
	return TaskReply{Background: &response}
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
	output = tools.TruncateOutput(output, spillDir)
	return fmt.Sprintf("<%s>\n<name>%s</name>\n<status>%s</status>\n<result>\nJob %s (%s) ended with exit code %d.\n%s\n</result>\n</%s>", TaskNotificationTag, BackgroundProcessName, status, job.ID, job.Description, shell.ExitCode(err), output, TaskNotificationTag)
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
	tools.PersistBackgroundReview(ctx, h.c.messages, sessionID, result)
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
		for _, job := range hub.backgroundShells {
			if !job.IsDone() {
				processes = append(processes, Process{JobID: job.ID, Command: job.Command, Started: job.Started})
			}
		}
		hub.mu.Unlock()
	}
	return processes
}
