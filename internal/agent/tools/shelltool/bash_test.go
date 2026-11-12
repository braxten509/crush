package shelltool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/shell"
)

func TestBashCtrlBLeavesCommandRunning(t *testing.T) {
	tool := newBashToolForTest(t.TempDir())
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), tools.SessionIDContextKey, "background-test"))
	defer cancel()
	// The command keeps running until the test opens its gate.
	gate := filepath.Join(t.TempDir(), "gate")
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0o600) })
	finished := make(chan fantasy.ToolResponse, 1)
	go func() {
		finished <- runBashTool(t, tool, ctx, tools.BashParams{Command: "while [ ! -e " + gate + " ]; do sleep 0.05; done; echo survived", Description: "background test"})
	}()
	require.Eventually(t, func() bool { return tools.BackgroundShell("background-test") }, time.Second, 10*time.Millisecond)
	var response fantasy.ToolResponse
	select {
	case response = <-finished:
	case <-time.After(time.Second):
		t.Fatal("Ctrl+B did not release the foreground call")
	}
	var metadata tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.True(t, metadata.Background)
	require.NotEmpty(t, metadata.ShellID)
	cancel()
	job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
	require.True(t, ok)
	require.NoError(t, os.WriteFile(gate, nil, 0o600))
	job.Wait()
	stdout, _, _, err := job.GetOutput()
	require.NoError(t, err)
	require.Contains(t, stdout, "survived")
	require.NoError(t, shell.GetBackgroundShellManager().Remove(metadata.ShellID))
	require.False(t, tools.BackgroundShell("background-test"))
}

type mockBashPermissionService struct {
	*pubsub.Broker[permission.PermissionRequest]
}

func (m *mockBashPermissionService) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	return true, nil
}

func (m *mockBashPermissionService) Grant(req permission.PermissionRequest) bool { return true }

func (m *mockBashPermissionService) Deny(req permission.PermissionRequest) bool { return true }

func (m *mockBashPermissionService) GrantPersistent(req permission.PermissionRequest) bool {
	return true
}

func (m *mockBashPermissionService) AutoApproveSession(sessionID string) {}

func (m *mockBashPermissionService) ActiveRequest() (permission.PermissionRequest, bool) {
	return permission.PermissionRequest{}, false
}

func (m *mockBashPermissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}

func (m *mockBashPermissionService) SetPermissionMode(mode permission.PermissionMode) {}

func (m *mockBashPermissionService) PermissionMode() permission.PermissionMode {
	return permission.PermissionModeNormal
}

func (m *mockBashPermissionService) SubscribeModeChanges(ctx context.Context) <-chan pubsub.Event[permission.ModeChangedEvent] {
	return make(<-chan pubsub.Event[permission.ModeChangedEvent])
}

func TestBashTool_DefaultAutoBackgroundThreshold(t *testing.T) {
	workingDir := t.TempDir()
	tool := newBashToolForTest(workingDir)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "default threshold",
		Command:     "echo done",
	})

	require.False(t, resp.IsError)
	var meta tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.False(t, meta.Background)
	require.Empty(t, meta.ShellID)
	require.Contains(t, meta.Output, "done")
}

type recordingPermissionService struct {
	*pubsub.Broker[permission.PermissionRequest]
	requestCount int
	allow        bool
	mode         permission.PermissionMode
	lastDanger   string
}

func (m *recordingPermissionService) Request(ctx context.Context, req permission.CreatePermissionRequest) (bool, error) {
	m.requestCount++
	m.lastDanger = req.Danger
	return m.allow, nil
}

func (m *recordingPermissionService) Grant(req permission.PermissionRequest) bool { return true }

func (m *recordingPermissionService) Deny(req permission.PermissionRequest) bool { return true }

func (m *recordingPermissionService) GrantPersistent(req permission.PermissionRequest) bool {
	return true
}

func (m *recordingPermissionService) AutoApproveSession(sessionID string) {}

func (m *recordingPermissionService) ActiveRequest() (permission.PermissionRequest, bool) {
	return permission.PermissionRequest{}, false
}

func (m *recordingPermissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[permission.PermissionNotification] {
	return make(<-chan pubsub.Event[permission.PermissionNotification])
}

func (m *recordingPermissionService) SetPermissionMode(mode permission.PermissionMode) { m.mode = mode }

func (m *recordingPermissionService) PermissionMode() permission.PermissionMode { return m.mode }

func (m *recordingPermissionService) SubscribeModeChanges(ctx context.Context) <-chan pubsub.Event[permission.ModeChangedEvent] {
	return make(<-chan pubsub.Event[permission.ModeChangedEvent])
}

func newBashToolForTest(workingDir string) fantasy.AgentTool {
	permissions := &mockBashPermissionService{Broker: pubsub.NewBroker[permission.PermissionRequest]()}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	return NewBashTool(permissions, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
}

func newBashToolWithRecordingPerms(workingDir string, allow bool) (fantasy.AgentTool, *recordingPermissionService) {
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  allow,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	return NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil), perms
}

func TestBashTool_ChainedCommandsRequirePermission(t *testing.T) {
	workingDir := t.TempDir()
	tool, perms := newBashToolWithRecordingPerms(workingDir, true)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	// A chain is judged by its parts, so joining two read-only commands is
	// no more of a prompt than either one alone would be.
	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "chained ls",
		Command:     "ls && echo done",
	})

	require.False(t, resp.IsError)
	require.Equal(t, 0, perms.requestCount, "a chain of read-only commands should not prompt")

	// One part that writes is enough to require approval for the whole chain.
	perms.requestCount = 0
	resp = runBashTool(t, tool, ctx, tools.BashParams{
		Description: "chained touch",
		Command:     "ls && touch newfile",
	})

	require.False(t, resp.IsError)
	require.Equal(t, 1, perms.requestCount, "a chain containing a write must prompt")

	// Plain ls should NOT trigger permission check.
	perms.requestCount = 0
	resp = runBashTool(t, tool, ctx, tools.BashParams{
		Description: "plain ls",
		Command:     "ls -la",
	})

	require.False(t, resp.IsError)
	require.Equal(t, 0, perms.requestCount, "plain ls should not trigger permission request")
}

func TestBashTool_ChainedCommandsDenied(t *testing.T) {
	workingDir := t.TempDir()
	tool, perms := newBashToolWithRecordingPerms(workingDir, false)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "chained ls denied",
		Command:     "ls && rm -rf /",
	})

	require.Equal(t, 1, perms.requestCount)
	require.True(t, resp.IsError)
}

func TestIsDangerousCommand(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		dangerous bool
	}{
		{
			name:      "simple banned command - curl",
			command:   "curl https://example.com",
			dangerous: true,
		},
		{
			name:      "simple banned command - sudo",
			command:   "sudo apt-get update",
			dangerous: true,
		},
		{
			name:      "npm global install with --global",
			command:   "npm install --global typescript",
			dangerous: true,
		},
		{
			name:      "npm global install with -g",
			command:   "npm install -g typescript",
			dangerous: true,
		},
		{
			name:      "npm local install",
			command:   "npm install typescript",
			dangerous: false,
		},
		{
			name:      "go test with -exec",
			command:   "go test -exec ./malicious ./...",
			dangerous: true,
		},
		{
			name:      "go test without -exec",
			command:   "go test ./...",
			dangerous: false,
		},
		{
			name:      "safe command - ls",
			command:   "ls -la",
			dangerous: false,
		},
		{
			name:      "safe command - echo",
			command:   "echo hello",
			dangerous: false,
		},
		{
			name:      "safe command - git",
			command:   "git status",
			dangerous: false,
		},
		{
			name:      "pip install with --user",
			command:   "pip install --user requests",
			dangerous: true,
		},
		{
			name:      "pip install without --user",
			command:   "pip install requests",
			dangerous: false,
		},
		{
			name:      "brew install",
			command:   "brew install wget",
			dangerous: true,
		},
		{
			// Quoting the command name must not evade detection.
			name:      "quoted dangerous command name",
			command:   `"brew" install wget`,
			dangerous: true,
		},
		{
			// Quoted arguments must still be seen by argument blockers.
			name:      "quoted dangerous argument",
			command:   `go test "-exec" ./malicious ./...`,
			dangerous: true,
		},
		{
			// Command substitution can't be resolved without executing, so
			// it is treated conservatively as dangerous.
			name:      "command substitution is conservative",
			command:   "$(echo brew) install wget",
			dangerous: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shell.IsCommandBlocked(tt.command, blockFuncs(defaultBlockedCommands))
			require.Equal(t, tt.dangerous, result, "command: %s", tt.command)
		})
	}
}

func runBashTool(t *testing.T, tool fantasy.AgentTool, ctx context.Context, params tools.BashParams) fantasy.ToolResponse {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  tools.BashToolName,
		Input: string(input),
	}

	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}

func TestTruncateOutputValidUTF8(t *testing.T) {
	t.Parallel()
	// CJK characters are 2 cells wide; this string is far wider than
	// MaxOutputLength so TruncateOutput must truncate it.
	content := strings.Repeat("你好世界", tools.MaxOutputLength)

	out := TruncateOutput(content, t.TempDir())
	require.True(t, utf8.ValidString(out), "truncated output must stay valid UTF-8")
	require.Less(t, len(out), len(content))
}

func TestTruncateOutputShortContent(t *testing.T) {
	t.Parallel()
	content := "short output"
	require.Equal(t, content, TruncateOutput(content, t.TempDir()))
}

func TestTruncateOutputEmoji(t *testing.T) {
	t.Parallel()
	// Emoji with ZWJ sequences should not be split.
	content := strings.Repeat("👨‍👩‍👧‍👦", tools.MaxOutputLength)

	out := TruncateOutput(content, t.TempDir())
	require.True(t, utf8.ValidString(out), "truncated output must stay valid UTF-8")
	require.Less(t, len(out), len(content))
}

// The commit record names Crush only: the tool is built once for every
// chat, and each chat owns its model, so no one model's name fits it.
func TestBashCommitRecordNamesNoModel(t *testing.T) {
	desc := bashDescription(&config.Attribution{TrailerStyle: "assisted-by"}, defaultBlockedCommands)
	require.Contains(t, desc, "Assisted-by: Crush\n")
	require.NotContains(t, desc, "Assisted-by: Crush:")
}

func TestResolveBlockedCommands(t *testing.T) {
	t.Parallel()

	t.Run("nil config uses the defaults", func(t *testing.T) {
		t.Parallel()
		require.ElementsMatch(t, defaultBlockedCommands, resolveBlockedCommands(nil))
	})

	t.Run("blocked_commands adds to the defaults", func(t *testing.T) {
		t.Parallel()
		got := resolveBlockedCommands(&config.Permissions{
			BlockedCommands: []string{"kubectl", "terraform"},
		})
		require.Contains(t, got, "kubectl")
		require.Contains(t, got, "terraform")
		require.Contains(t, got, "curl", "defaults must still apply")
	})

	t.Run("allowed_commands removes defaults", func(t *testing.T) {
		t.Parallel()
		got := resolveBlockedCommands(&config.Permissions{
			AllowedCommands: []string{"curl", "wget"},
		})
		require.NotContains(t, got, "curl")
		require.NotContains(t, got, "wget")
		require.Contains(t, got, "sudo", "unrelated defaults must stay")
	})

	t.Run("allowed_commands wins over blocked_commands", func(t *testing.T) {
		t.Parallel()
		got := resolveBlockedCommands(&config.Permissions{
			BlockedCommands: []string{"kubectl"},
			AllowedCommands: []string{"kubectl"},
		})
		require.NotContains(t, got, "kubectl")
	})

	t.Run("built-in order is preserved and duplicates collapsed", func(t *testing.T) {
		t.Parallel()
		got := resolveBlockedCommands(&config.Permissions{
			BlockedCommands: []string{"curl", "kubectl", "kubectl"},
		})
		// The list is embedded verbatim in the tool description, so the
		// defaults must keep their hand-grouped order and additions go last.
		require.Equal(t, defaultBlockedCommands, got[:len(defaultBlockedCommands)])
		require.Equal(t, []string{"kubectl"}, got[len(defaultBlockedCommands):],
			"additions append once; a duplicate of a default is dropped")
	})

	t.Run("unblocking a command stops it reading as dangerous", func(t *testing.T) {
		t.Parallel()
		blocked := resolveBlockedCommands(&config.Permissions{AllowedCommands: []string{"curl"}})
		require.False(t, shell.IsCommandBlocked("curl https://example.com", blockFuncs(blocked)))
		require.True(t, shell.IsCommandBlocked("sudo rm -rf /", blockFuncs(blocked)))
	})
}

// TestBashTool_ApprovedDangerousCommandRuns pins the rule that a command the
// user was warned about and approved anyway reaches the shell unguarded:
// re-blocking it would override the answer they just gave.
func TestBashTool_ApprovedDangerousCommandRuns(t *testing.T) {
	workingDir := t.TempDir()
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeNormal,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	// `ifconfig` is on the default dangerous list, so it is prompted for.
	// Approving it must not then be overridden by the exec-time block list.
	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "approved dangerous command",
		Command:     "ifconfig --help",
	})

	require.Equal(t, 1, perms.requestCount, "a dangerous command must be prompted for")
	require.Equal(t, "ifconfig", perms.lastDanger,
		"the prompt must name what tripped the check")
	require.False(t, resp.IsError, "an approved command must not be re-blocked at exec time: %s", resp.Content)
}

// TestBashTool_UnflaggedCommandKeepsBlockList is the other side of that rule.
// The static check expands commands with globbing off, so a glob hides the
// dangerous name from it and yolo mode auto-approves the call unseen. The block
// list stays on at exec time, where the glob has resolved and the real command
// name is finally visible.
func TestBashTool_UnflaggedCommandKeepsBlockList(t *testing.T) {
	workingDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "ifconfig"), []byte("#!/bin/sh\n"), 0o755))
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeYolo,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "indirect dangerous command",
		Command:     "ifconf?g --help",
	})

	require.Empty(t, perms.lastDanger, "the static check cannot see through a glob")
	require.True(t, resp.IsError, "the block list stops it at exec time: %s", resp.Content)
}

// TestBashTool_SysadminModeSkipsBlockList pins sysadmin mode as the deliberate
// exception: it asks for nothing and blocks nothing.
func TestBashTool_SysadminModeSkipsBlockList(t *testing.T) {
	workingDir := t.TempDir()
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeSysadmin,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "ifconfig"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "indirect dangerous command",
		Command:     "ifconf?g --help",
	})

	require.False(t, resp.IsError, "sysadmin mode blocks nothing: %s", resp.Content)
}

// TestBashTool_DeniedDangerousCommandDoesNotRun is the other half: denial at
// the permission layer stops the command, no block list required.
func TestBashTool_DeniedDangerousCommandDoesNotRun(t *testing.T) {
	workingDir := t.TempDir()
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  false,
		mode:   permission.PermissionModeNormal,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "denied dangerous command",
		Command:     "ifconfig --help",
	})

	require.Equal(t, 1, perms.requestCount)
	require.True(t, resp.IsError)
}

// TestBashTool_SafeReadOnlyPathStillEnforcesBlockList pins the one branch that
// never asks: a safe-prefix command skips the permission request entirely, so
// the block list stays on there as a backstop.
func TestBashTool_SafeReadOnlyPathStillEnforcesBlockList(t *testing.T) {
	workingDir := t.TempDir()
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeNormal,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "safe command",
		Command:     "pwd",
	})

	require.Zero(t, perms.requestCount, "safe read-only commands must not prompt")
	require.False(t, resp.IsError)
}

// TestBashTool_YoloModeDoesNotPromptForDangerousCommands pins the rule that
// yolo mode means stop asking. It drives the real permission service rather
// than a mock, because the whole behaviour under test lives there.
func TestBashTool_YoloModeDoesNotPromptForDangerousCommands(t *testing.T) {
	workingDir := t.TempDir()
	perms := permission.NewPermissionService(workingDir, nil, nil)
	perms.SetPermissionMode(permission.PermissionModeYolo)

	// Any prompt would land here. Nothing should.
	prompts := perms.Subscribe(t.Context())

	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "test-session")

	// `ifconfig` is on the default dangerous list. In yolo it must run
	// without asking, and without being re-blocked at exec time either.
	// Run it off the test goroutine so a regression that reinstates the
	// prompt fails here in a few seconds rather than hanging until the
	// whole package times out.
	responses := make(chan fantasy.ToolResponse, 1)
	go func() {
		responses <- runBashTool(t, tool, ctx, tools.BashParams{
			Description: "dangerous command in yolo mode",
			Command:     "ifconfig --help",
		})
	}()

	var got fantasy.ToolResponse
	select {
	case got = <-responses:
	case <-time.After(30 * time.Second):
		t.Fatal("yolo mode blocked waiting for a permission prompt that should never have been raised")
	}

	require.False(t, got.IsError, "a dangerous command must not be blocked in yolo mode: %s", got.Content)

	select {
	case p := <-prompts:
		t.Fatalf("yolo mode must not prompt, but got a request for %q", p.Payload.Description)
	default:
	}
}

// TestBashTool_NormalModeStillPromptsForDangerousCommands is the other half of
// the rule: manual mode keeps asking.
func TestBashTool_NormalModeStillPromptsForDangerousCommands(t *testing.T) {
	workingDir := t.TempDir()
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeNormal,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "test-session")

	runBashTool(t, tool, ctx, tools.BashParams{
		Description: "dangerous command in normal mode",
		Command:     "ifconfig --help",
	})

	require.Equal(t, 1, perms.requestCount, "normal mode must still ask")
	require.Equal(t, "ifconfig", perms.lastDanger,
		"the prompt must name what tripped the check")
}

// TestBlockedCommandErrorNamesTheCommand checks that a command stopped at
// exec time names the command that was recognised. The static check and the
// run-time check have to agree about that, or the same command is described
// two different ways depending on which one caught it.
func TestBlockedCommandErrorNamesTheCommand(t *testing.T) {
	workingDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workingDir, "ifconfig"), []byte("#!/bin/sh\n"), 0o755))
	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
		mode:   permission.PermissionModeYolo,
	}
	attribution := &config.Attribution{TrailerStyle: config.TrailerStyleNone}
	tool := NewBashTool(perms, workingDir, workingDir, attribution, config.DefaultMaxBackgroundJobs, nil, nil)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "test-session")

	resp := runBashTool(t, tool, ctx, tools.BashParams{
		Description: "indirect dangerous command",
		Command:     "ifconf?g --help",
	})

	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "ifconfig", "the error must name the command")
	require.NotContains(t, resp.Content, "ifconf?g", "not the spelling that reached it")
}
