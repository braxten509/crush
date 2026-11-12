package shelltool

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
	"slices"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"mvdan.cc/sh/v3/syntax"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/sandbox"
	"github.com/charmbracelet/crush/internal/shell"
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
	RgAvailable     bool
	GhAvailable     bool
}

// defaultBlockedCommands is the built-in dangerous-command list. It is a
// default, not a hardcoded policy: the `permissions` config block can add
// to it and take away from it (see resolveBlockedCommands).
var defaultBlockedCommands = []string{
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

	// Indirect execution: these run a command name the static check can't
	// see, so whatever they wrap would never be flagged on its own.
	"env",
	"eval",
	"nohup",
	"xargs",

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

// resolveBlockedCommands returns the effective dangerous-command list:
// the built-in defaults plus perms.BlockedCommands, minus
// perms.AllowedCommands.
//
// The built-in order is preserved and user additions are appended. The list is
// embedded verbatim in the tool description, so it has to be deterministic, but
// sorting it would scramble the category grouping the defaults are written in
// and churn the prompt for every existing user.
func resolveBlockedCommands(perms *config.Permissions) []string {
	if perms == nil {
		return slices.Clone(defaultBlockedCommands)
	}
	// Both sides are normalized the way the block list itself matches, so a
	// config entry spelled "/usr/bin/CURL" or " curl " lines up with the
	// "curl" already on the list. Without this, allowing a command by a
	// slightly different spelling did nothing at all, which is the worst way
	// for a permission setting to be wrong: it looks applied.
	allowed := make(map[string]struct{}, len(perms.AllowedCommands))
	for _, cmd := range perms.AllowedCommands {
		allowed[shell.NormalizeCommandName(cmd)] = struct{}{}
	}

	blocked := make([]string, 0, len(defaultBlockedCommands)+len(perms.BlockedCommands))
	blocked = append(blocked, defaultBlockedCommands...)
	for _, cmd := range perms.BlockedCommands {
		if name := shell.NormalizeCommandName(cmd); name != "" {
			blocked = append(blocked, name)
		}
	}
	blocked = slices.DeleteFunc(blocked, func(c string) bool {
		_, ok := allowed[shell.NormalizeCommandName(c)]
		return ok
	})

	seen := make(map[string]struct{}, len(blocked))
	deduped := blocked[:0]
	for _, cmd := range blocked {
		if _, dup := seen[cmd]; dup {
			continue
		}
		seen[cmd] = struct{}{}
		deduped = append(deduped, cmd)
	}
	return deduped
}

// bashDescription renders the tool description. It names no model: the
// commit record says "Assisted-by: Crush" only.
func bashDescription(attribution *config.Attribution, blocked []string) string {
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  strings.Join(blocked, ", "),
		MaxOutputLength: tools.MaxOutputLength,
		Attribution:     *attribution,
		RgAvailable:     tools.GetRg() != "",
		GhAvailable:     tools.GHAvailable,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

// blockFuncs builds the block list from the resolved dangerous commands.
// The flat command list is configurable; the argument blockers below are
// structural rules about specific flags rather than a list of binaries, so
// they stay built in.
func blockFuncs(blocked []string) []shell.BlockFunc {
	return []shell.BlockFunc{
		shell.CommandsBlocker(blocked),

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

// NewBashTool returns the bash tool. maxBackgroundJobs caps how many jobs
// may run at once (see config.Options.GetMaxBackgroundJobs). perms adjusts
// the dangerous-command list (blocked_commands, allowed_commands) and adds
// configured safe commands (safe_commands); nil keeps the built-in lists.
// jobStarted, when set, is told about the jobs its calls leave running in
// the background.
func NewBashTool(permissions permission.Service, workingDir, spillDir string, attribution *config.Attribution, maxBackgroundJobs int, perms *config.Permissions, jobStarted tools.JobStarted, opts ...BashOption) fantasy.AgentTool {
	t := newBashTool(permissions, workingDir, spillDir, maxBackgroundJobs, perms, jobStarted, opts...)
	return fantasy.NewAgentTool(
		tools.BashToolName,
		bashDescription(attribution, t.blocked),
		t.run,
	)
}

// NewReadOnlyBashTool returns a bash tool whose commands run inside the
// read-only sandbox: they can read anything but can't change the project.
// Its other arguments are those of [NewBashTool].
func NewReadOnlyBashTool(permissions permission.Service, workingDir, spillDir string, attribution *config.Attribution, maxBackgroundJobs int, perms *config.Permissions, jobStarted tools.JobStarted, opts ...BashOption) fantasy.AgentTool {
	t := newBashTool(permissions, workingDir, spillDir, maxBackgroundJobs, perms, jobStarted, opts...)
	t.readOnly = true
	return fantasy.NewAgentTool(
		tools.BashToolName,
		bashDescription(attribution, t.blocked),
		t.run,
	)
}

// BashOption adjusts a bash tool as it is built.
type BashOption func(*bashTool)

// WithAutoBackground gives the tool the user's time before a foreground
// command is moved to the background (config.Options.GetAutoBackground).
// It is read at every command, so a saved change applies at once.
func WithAutoBackground(after func() time.Duration) BashOption {
	return func(t *bashTool) { t.autoBackground = after }
}

func newBashTool(permissions permission.Service, workingDir, spillDir string, maxBackgroundJobs int, perms *config.Permissions, jobStarted tools.JobStarted, opts ...BashOption) *bashTool {
	t := &bashTool{
		permissions:       permissions,
		workingDir:        workingDir,
		spillDir:          spillDir,
		jobStarted:        jobStarted,
		maxBackgroundJobs: maxBackgroundJobs,
		blocked:           resolveBlockedCommands(perms),
		userSafe:          userSafeCommands(perms),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// bashTool holds what the bash tool needs to run a command.
type bashTool struct {
	permissions permission.Service
	workingDir  string
	spillDir    string
	jobStarted  tools.JobStarted
	readOnly    bool
	// maxBackgroundJobs caps how many jobs may run at once.
	maxBackgroundJobs int
	// autoBackground reads the user's time before a foreground command is
	// moved to the background; nil means the default.
	autoBackground func() time.Duration
	// blocked is the resolved dangerous-command list.
	blocked []string
	// userSafe are the configured commands that skip the prompt.
	userSafe []safeCommand
}

// bashRun is one command the bash tool is running.
type bashRun struct {
	params    tools.BashParams
	sessionID string
	marker    string
	shellEnv  []string
	workDir   string
	startTime time.Time
	// shellDir and shellCommand are where and what the shell runs: the
	// working folder and command, or the sandbox wrapping them.
	shellDir     string
	shellCommand string
	// blockFuncs is the run-time block list for this command; nil once the
	// user approved a command the check recognised, or in sysadmin mode.
	blockFuncs []shell.BlockFunc
}

func (t *bashTool) run(ctx context.Context, params tools.BashParams, call fantasy.ToolCall) (toolResponse fantasy.ToolResponse, resultErr error) {
	if params.Command == "" {
		return fantasy.NewTextErrorResponse("missing command"), nil
	}

	// Determine working directory
	execWorkingDir := cmp.Or(params.WorkingDir, t.workingDir)
	safeReadOnly := isSafeReadOnly(params.Command, t.userSafe)

	sessionID := tools.GetSessionFromContext(ctx)
	shellEnv := tools.GetContextValue(ctx, tools.ShellEnvContextKey, []string(nil))
	marker := uuid.NewString()
	shellEnv = append(append([]string(nil), shellEnv...), tools.NativeShellEnv+"="+marker)
	if sessionID == "" {
		return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for executing shell command")
	}
	// Check whether the command is dangerous so we can surface a
	// warning in the permission dialog.
	var check shell.CommandCheck
	if !safeReadOnly {
		check = shell.CheckCommand(params.Command, blockFuncs(t.blocked))

		p, err := t.permissions.Request(
			ctx,
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        execWorkingDir,
				ToolCallID:  call.ID,
				ToolName:    tools.BashToolName,
				Action:      "execute",
				Description: fmt.Sprintf("Execute command: %s", params.Command),
				Params:      tools.BashPermissionsParams(params),
				Danger:      check.Reason,
				// Path is only the working directory here, so a
				// session grant keyed on it alone would turn one
				// approval of `ls` into standing approval for every
				// later command run in that directory. Key it to the
				// command the user was actually shown.
				GrantKey: params.Command,
			},
		)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !p {
			return tools.NewPermissionDeniedResponse(), nil
		}
	}

	// Re-blocking a command that was already approved would override
	// the answer that was given, whether the user gave it at a prompt
	// or yolo mode gave it on their behalf. That only applies when the
	// check actually recognised a command by name, though: a warning
	// that merely says "this could not be analysed" names nothing the
	// user could have judged, so approving it is not approval of
	// anything in particular and the block list stays on. Otherwise a
	// stray $(date) anywhere on the line would waive the run-time
	// checks for every command beside it.
	//
	// What the block list can see is narrow: commands this interpreter
	// dispatches itself, including names that only resolve after a
	// glob or variable expands. A command that runs inside a child
	// process is invisible to it.
	//
	// Sysadmin mode is the deliberate exception: it asks for nothing
	// and blocks nothing.
	var blocksToUse []shell.BlockFunc
	if !check.Matched && t.permissions.PermissionMode() != permission.PermissionModeSysadmin {
		blocksToUse = blockFuncs(t.blocked)
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

		shellDir:     execWorkingDir,
		shellCommand: params.Command,
		blockFuncs:   blocksToUse,
	}
	if t.readOnly {
		dir, command, err := lockedCommand(params.Command, execWorkingDir)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		r.shellDir, r.shellCommand = dir, command
	}
	// If explicitly requested as background, start immediately with detached context
	if params.RunInBackground {
		toolResponse, backgrounded, resultErr = t.runInBackground(ctx, r, movedSleep)
		return toolResponse, resultErr
	}
	toolResponse, backgrounded, resultErr = t.runInForeground(ctx, r)
	return toolResponse, resultErr
}

// lockedCommand wraps a command so bash runs it read-only in dir. It
// returns the folder to start the shell in and the shell command.
func lockedCommand(command, dir string) (string, string, error) {
	shellDir, name, args, err := sandbox.ReadOnly(dir, nil, "bash", []string{"-c", command})
	if err != nil {
		return "", "", err
	}
	quoted := make([]string, 0, len(args)+1)
	for _, arg := range append([]string{name}, args...) {
		q, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			return "", "", err
		}
		quoted = append(quoted, q)
	}
	return shellDir, strings.Join(quoted, " "), nil
}

// FastFailureWait is how long a new background command is watched for a
// quick failure before it is left running as a job; a variable so tests can
// shorten it.
var FastFailureWait = time.Second

// runInBackground starts a command the model asked to run in the
// background. It reports whether the command was left running as a job.
func (t *bashTool) runInBackground(ctx context.Context, r *bashRun, movedSleep bool) (fantasy.ToolResponse, bool, error) {
	r.startTime = time.Now()
	bgManager := shell.GetBackgroundShellManager()
	// Use background context so it continues after tool returns. Start drops
	// stale finished jobs itself and only counts running ones.
	bgShell, err := bgManager.Start(context.WithoutCancel(ctx), r.shellDir, r.blockFuncs, r.shellCommand, r.params.Description, t.maxBackgroundJobs, r.shellEnv...)
	if err != nil {
		// A refused start is something the agent can act on (stop or wait
		// for a job), so it is a tool error, not one that ends the turn.
		return fantasy.NewTextErrorResponse("Failed to start background shell: " + err.Error()), false, nil
	}

	// Wait a short time to detect fast failures (blocked commands, syntax errors, etc.)
	time.Sleep(FastFailureWait)
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
	bgShell, err := bgManager.Start(context.WithoutCancel(ctx), r.shellDir, r.blockFuncs, r.shellCommand, r.params.Description, t.maxBackgroundJobs, r.shellEnv...)
	if err != nil {
		return fantasy.NewTextErrorResponse("Failed to start shell: " + err.Error()), false, nil
	}

	// Wait for either completion, auto-background threshold, the user moving
	// it to the background, or context cancellation
	backgroundSignal := make(chan struct{}, 1)
	unregisterBackground := tools.RegisterShellBackground(r.sessionID, r.marker, backgroundSignal)
	defer unregisterBackground()
	manuallyBackgrounded := false
	timer := time.NewTimer(autoBackgroundWait(r.params.AutoBackgroundAfter, t.autoBackgroundLimit()))
	defer timer.Stop()

	select {
	case <-bgShell.Done():
	case <-timer.C:
	case <-backgroundSignal:
		manuallyBackgrounded = true
	case <-ctx.Done():
		// Incoming context was cancelled before we moved to background
		// Kill the shell and return error
		bgManager.Kill(bgShell.ID)
		return fantasy.ToolResponse{}, false, ctx.Err()
	}
	stdout, stderr, done, execErr := bgShell.GetOutput()

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
		// A command the shell could not run (blocked, unparsable) is the
		// agent's to fix, so it comes back as a tool error rather than
		// ending the turn.
		return fantasy.NewTextErrorResponse(fmt.Sprintf("[Job %s] Error executing command: %s", bgShell.ID, execErr)), nil
	}

	stdout = formatOutput(stdout, stderr, execErr, t.spillDir)

	metadata := tools.BashResponseMetadata{
		StartTime:        r.startTime.UnixMilli(),
		EndTime:          time.Now().UnixMilli(),
		Output:           stdout,
		Description:      r.params.Description,
		Background:       r.params.RunInBackground,
		WorkingDirectory: bgShell.WorkingDir,
	}
	if stdout == "" {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(tools.BashNoOutput), metadata), nil
	}
	stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
}

// jobMetadata describes a command left running as a job.
func (r *bashRun) jobMetadata(bgShell *shell.BackgroundShell) tools.BashResponseMetadata {
	return tools.BashResponseMetadata{
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
	if ansi.StringWidth(content) <= tools.MaxOutputLength {
		return content
	}

	halfLength := tools.MaxOutputLength / 2
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

// autoBackgroundLimit is the user's time before a foreground command is
// moved to the background.
func (t *bashTool) autoBackgroundLimit() time.Duration {
	if t.autoBackground != nil {
		if after := t.autoBackground(); after > 0 {
			return after
		}
	}
	return time.Duration(tools.MaxAutoBackgroundAfter) * time.Second
}

// autoBackgroundWait is how long a command runs in the foreground before it
// becomes a background job: the user's time (limit), or the shorter time
// the command asked for, never under the protected foreground wait (owner,
// 2026-10-21: "if a command has been running for 2 minutes or more it is
// forced to background"; 2026-11-12: the time is a setting).
func autoBackgroundWait(requested int, limit time.Duration) time.Duration {
	wait := limit
	if requested > 0 {
		wait = min(limit, time.Duration(requested)*time.Second)
	}
	return max(ForegroundWaitLimit+time.Second, wait)
}
