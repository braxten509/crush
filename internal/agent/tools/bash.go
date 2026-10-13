package tools

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/shell"
)

type BashParams struct {
	Description         string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string `json:"command" description:"The command to execute"`
	WorkingDir          string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool   `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. Use job_output to read the output later."`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty" description:"Seconds to wait before automatically moving the command to a background job (default: 60)"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
}

const (
	BashToolName = "bash"

	DefaultAutoBackgroundAfter = 60 // Commands taking longer automatically become background jobs
	MaxOutputLength            = 30000
	BashNoOutput               = "no output"
)

//go:embed bash.md.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	BannedCommands  string
	MaxOutputLength int
	Attribution     config.Attribution
	ModelID         string
	RgAvailable     bool
	GhAvailable     bool
}

var bannedCommands = []string{
	// Network/Download tools
	"alias",
	"aria2c",
	"axel",
	"chrome",
	"curl",
	"curlie",
	"firefox",
	"http-prompt",
	"httpie",
	"links",
	"lynx",
	"nc",
	"safari",
	"scp",
	"ssh",
	"telnet",
	"w3m",
	"wget",
	"xh",

	// System administration
	"doas",
	"su",
	"sudo",

	// Package managers
	"apk",
	"apt",
	"apt-cache",
	"apt-get",
	"dnf",
	"dpkg",
	"emerge",
	"home-manager",
	"makepkg",
	"opkg",
	"pacman",
	"paru",
	"pkg",
	"pkg_add",
	"pkg_delete",
	"portage",
	"rpm",
	"yay",
	"yum",
	"zypper",

	// System modification
	"at",
	"batch",
	"chkconfig",
	"crontab",
	"fdisk",
	"mkfs",
	"mount",
	"parted",
	"service",
	"systemctl",
	"umount",

	// Network configuration
	"firewall-cmd",
	"ifconfig",
	"ip",
	"iptables",
	"netstat",
	"pfctl",
	"route",
	"ufw",
}

func bashDescription(attribution *config.Attribution, modelID string) string {
	bannedCommandsStr := strings.Join(bannedCommands, ", ")
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  bannedCommandsStr,
		MaxOutputLength: MaxOutputLength,
		Attribution:     *attribution,
		ModelID:         modelID,
		RgAvailable:     GetRg() != "",
		GhAvailable:     GHAvailable,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

func blockFuncs() []shell.BlockFunc {
	return []shell.BlockFunc{
		shell.CommandsBlocker(bannedCommands),

		// System package managers
		shell.ArgumentsBlocker("apk", []string{"add"}, nil),
		shell.ArgumentsBlocker("apt", []string{"install"}, nil),
		shell.ArgumentsBlocker("apt-get", []string{"install"}, nil),
		shell.ArgumentsBlocker("dnf", []string{"install"}, nil),
		shell.ArgumentsBlocker("pacman", nil, []string{"-S"}),
		shell.ArgumentsBlocker("pkg", []string{"install"}, nil),
		shell.ArgumentsBlocker("yum", []string{"install"}, nil),
		shell.ArgumentsBlocker("zypper", []string{"install"}, nil),

		// Language-specific package managers
		shell.ArgumentsBlocker("brew", []string{"install"}, nil),
		shell.ArgumentsBlocker("cargo", []string{"install"}, nil),
		shell.ArgumentsBlocker("gem", []string{"install"}, nil),
		shell.ArgumentsBlocker("go", []string{"install"}, nil),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"--global"}),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"-g"}),
		shell.ArgumentsBlocker("pip", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pip3", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"--global"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"-g"}),
		shell.ArgumentsBlocker("yarn", []string{"global", "add"}, nil),

		// `go test -exec` can run arbitrary commands
		shell.ArgumentsBlocker("go", []string{"test"}, []string{"-exec"}),
	}
}

// NewBashTool returns the bash tool. jobStarted, when set, is told about
// the jobs its calls leave running in the background.
func NewBashTool(permissions permission.Service, workingDir, spillDir string, attribution *config.Attribution, modelID string, jobStarted JobStarted) fantasy.AgentTool {
	t := &bashTool{
		permissions: permissions,
		workingDir:  workingDir,
		spillDir:    spillDir,
		jobStarted:  jobStarted,
	}
	return fantasy.NewAgentTool(
		BashToolName,
		string(bashDescription(attribution, modelID)),
		t.run,
	)
}

// bashTool holds what the bash tool needs to run a command.
type bashTool struct {
	permissions permission.Service
	workingDir  string
	spillDir    string
	jobStarted  JobStarted
}

// bashRun is one command the bash tool is running.
type bashRun struct {
	params    BashParams
	sessionID string
	marker    string
	shellEnv  []string
	workDir   string
	startTime time.Time
}

func (t *bashTool) run(ctx context.Context, params BashParams, call fantasy.ToolCall) (toolResponse fantasy.ToolResponse, resultErr error) {
	if params.Command == "" {
		return fantasy.NewTextErrorResponse("missing command"), nil
	}

	// Determine working directory
	execWorkingDir := cmp.Or(params.WorkingDir, t.workingDir)
	isSafeReadOnly := isSafeReadOnlyCommand(params.Command)

	sessionID := GetSessionFromContext(ctx)
	shellEnv := GetContextValue(ctx, ShellEnvContextKey, []string(nil))
	marker := uuid.NewString()
	shellEnv = append(append([]string(nil), shellEnv...), NativeShellEnv+"="+marker)
	if sessionID == "" {
		return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for executing shell command")
	}
	if !isSafeReadOnly {
		p, err := t.permissions.Request(
			ctx,
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        execWorkingDir,
				ToolCallID:  call.ID,
				ToolName:    BashToolName,
				Action:      "execute",
				Description: fmt.Sprintf("Execute command: %s", params.Command),
				Params:      BashPermissionsParams(params),
			},
		)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !p {
			return NewPermissionDeniedResponse(), nil
		}
	}

	ctx, report := filechange.WithCommandReview(ctx, execWorkingDir, config.GlobalCacheDir(), filepath.Dir(config.GlobalConfigData()))
	backgrounded := false
	defer func() {
		if !backgrounded {
			toolResponse.Metadata = filechange.WithReview(toolResponse.Metadata, report.Finish())
		}
	}()

	// A long leading sleep only stalls the agent, so it runs in the
	// background instead, as Claude Code does.
	movedSleep := !params.RunInBackground && LeadingSleep(params.Command)
	params.RunInBackground = params.RunInBackground || movedSleep

	r := &bashRun{
		params:    params,
		sessionID: sessionID,
		marker:    marker,
		shellEnv:  shellEnv,
		workDir:   execWorkingDir,
	}
	// If explicitly requested as background, start immediately with detached context
	if params.RunInBackground {
		toolResponse, backgrounded, resultErr = t.runInBackground(ctx, r, movedSleep)
		return toolResponse, resultErr
	}
	toolResponse, backgrounded, resultErr = t.runInForeground(ctx, r)
	return toolResponse, resultErr
}

// isSafeReadOnlyCommand reports whether a command is a single read-only
// command that runs without asking for permission.
func isSafeReadOnlyCommand(command string) bool {
	if containsCommandChaining(command) {
		return false
	}
	cmdLower := strings.ToLower(command)
	for _, safe := range safeCommands {
		if strings.HasPrefix(cmdLower, safe) {
			if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
				return true
			}
		}
	}
	return false
}

// runInBackground starts a command the model asked to run in the
// background. It reports whether the command was left running as a job.
func (t *bashTool) runInBackground(ctx context.Context, r *bashRun, movedSleep bool) (fantasy.ToolResponse, bool, error) {
	r.startTime = time.Now()
	bgManager := shell.GetBackgroundShellManager()
	bgManager.Cleanup()
	// Use background context so it continues after tool returns
	bgShell, err := bgManager.Start(context.WithoutCancel(ctx), r.workDir, blockFuncs(), r.params.Command, r.params.Description, r.shellEnv...)
	if err != nil {
		return fantasy.ToolResponse{}, false, fmt.Errorf("error starting background shell: %w", err)
	}

	// Wait a short time to detect fast failures (blocked commands, syntax errors, etc.)
	time.Sleep(1 * time.Second)
	stdout, stderr, done, execErr := bgShell.GetOutput()

	if done {
		// Command failed or completed very quickly
		bgManager.Remove(bgShell.ID)
		resp, err := t.completed(r, bgShell, stdout, stderr, execErr)
		return resp, false, err
	}

	// Still running after fast-failure check - return as background job
	metadata := r.jobMetadata(bgShell)
	response := fmt.Sprintf("Background shell started with ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
	if t.jobStarted.Tell(ctx, r.sessionID, r.marker, bgShell) {
		response += jobReportNote
	}
	if movedSleep {
		response = "A sleep longer than 10 seconds doesn't run in the foreground, so this runs in the background. To wait for something, loop on a check (until <check>; do sleep 2; done).\n\n" + response
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), true, nil
}

// runInForeground runs a command until it finishes, the user moves it to
// the background, or it runs past its auto-background threshold. It reports
// whether the command was left running as a job.
func (t *bashTool) runInForeground(ctx context.Context, r *bashRun) (fantasy.ToolResponse, bool, error) {
	// Start synchronous execution with auto-background support
	r.startTime = time.Now()

	// Start with detached context so it can survive if moved to background
	bgManager := shell.GetBackgroundShellManager()
	bgManager.Cleanup()
	bgShell, err := bgManager.Start(context.WithoutCancel(ctx), r.workDir, blockFuncs(), r.params.Command, r.params.Description, r.shellEnv...)
	if err != nil {
		return fantasy.ToolResponse{}, false, fmt.Errorf("error starting shell: %w", err)
	}

	// Wait for either completion, auto-background threshold, or context cancellation
	backgroundSignal := make(chan struct{}, 1)
	unregisterBackground := RegisterShellBackground(r.sessionID, r.marker, backgroundSignal)
	defer unregisterBackground()
	manuallyBackgrounded := false
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	autoBackgroundAfter := max(int(ForegroundWaitLimit/time.Second)+1, cmp.Or(r.params.AutoBackgroundAfter, DefaultAutoBackgroundAfter))
	autoBackgroundThreshold := time.Duration(autoBackgroundAfter) * time.Second
	timeout := time.After(autoBackgroundThreshold)

	var stdout, stderr string
	var done bool
	var execErr error

waitLoop:
	for {
		select {
		case <-ticker.C:
			stdout, stderr, done, execErr = bgShell.GetOutput()
			if done {
				break waitLoop
			}
		case <-timeout:
			stdout, stderr, done, execErr = bgShell.GetOutput()
			break waitLoop
		case <-backgroundSignal:
			manuallyBackgrounded = true
			stdout, stderr, done, execErr = bgShell.GetOutput()
			break waitLoop
		case <-ctx.Done():
			// Incoming context was cancelled before we moved to background
			// Kill the shell and return error
			bgManager.Kill(bgShell.ID)
			return fantasy.ToolResponse{}, false, ctx.Err()
		}
	}

	if done {
		// Command completed within threshold - return synchronously
		// Remove from background manager since we're returning directly
		// Don't call Kill() as it cancels the context and corrupts the exit code
		bgManager.Remove(bgShell.ID)
		resp, err := t.completed(r, bgShell, stdout, stderr, execErr)
		return resp, false, err
	}

	// Still running - keep as background job
	metadata := r.jobMetadata(bgShell)
	response := fmt.Sprintf("Command is taking longer than expected and has been moved to background.\n\nBackground shell ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
	if manuallyBackgrounded {
		response = fmt.Sprintf("Command moved to the background by the user.\n\nBackground shell ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
	}
	if t.jobStarted.Tell(ctx, r.sessionID, r.marker, bgShell) {
		response += jobReportNote
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), true, nil
}

// completed builds the response for a command that finished before it was
// left running as a job.
func (t *bashTool) completed(r *bashRun, bgShell *shell.BackgroundShell, stdout, stderr string, execErr error) (fantasy.ToolResponse, error) {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)
	if exitCode == 0 && !interrupted && execErr != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("[Job %s] error executing command: %w", bgShell.ID, execErr)
	}

	stdout = formatOutput(stdout, stderr, execErr, t.spillDir)

	metadata := BashResponseMetadata{
		StartTime:        r.startTime.UnixMilli(),
		EndTime:          time.Now().UnixMilli(),
		Output:           stdout,
		Description:      r.params.Description,
		Background:       r.params.RunInBackground,
		WorkingDirectory: bgShell.WorkingDir,
	}
	if stdout == "" {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
	}
	stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
}

// jobMetadata describes a command left running as a job.
func (r *bashRun) jobMetadata(bgShell *shell.BackgroundShell) BashResponseMetadata {
	return BashResponseMetadata{
		StartTime:        r.startTime.UnixMilli(),
		EndTime:          time.Now().UnixMilli(),
		Description:      r.params.Description,
		WorkingDirectory: bgShell.WorkingDir,
		Background:       true,
		ShellID:          bgShell.ID,
	}
}

// jobReportNote tells the agent a background job reports back by itself.
const jobReportNote = " Crush will report when it ends; keep working without waiting."

// formatOutput formats the output of a completed command with error handling
func formatOutput(stdout, stderr string, execErr error, spillDir string) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	stdout = truncateOutput(stdout, spillDir)
	stderr = truncateOutput(stderr, spillDir)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	return stdout
}

// TruncateOutput keeps the head and tail of oversized tool output. The
// dropped middle is written in full to a file under spillDir so the agent
// can read it back rather than losing it for good.
func TruncateOutput(content, spillDir string) string {
	if ansi.StringWidth(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := ansi.Truncate(content, halfLength, "")
	end := ansi.TruncateLeft(content, ansi.StringWidth(content)-halfLength, "")

	truncatedLinesCount := max(strings.Count(content, "\n")-strings.Count(start, "\n")-strings.Count(end, "\n"), 0)
	marker := fmt.Sprintf("... [%d lines truncated] ...", truncatedLinesCount)
	if path, err := shell.SpillOutput(ansi.Strip(content), spillDir); err == nil {
		marker = fmt.Sprintf("... [%d lines truncated, full output: %s] ...", truncatedLinesCount, path)
	} else {
		slog.Debug("Could not spill tool output", "dir", spillDir, "error", err)
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s", start, marker, end)
}

func truncateOutput(content, spillDir string) string {
	return TruncateOutput(content, spillDir)
}

func normalizeWorkingDir(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, fsext.WindowsWorkingDirDrive(), "")
	}
	return filepath.ToSlash(path)
}
