package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/herdr"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
	"github.com/google/uuid"
	"github.com/pkg/browser"
)

// ClientWorkspace implements the Workspace interface by delegating all
// operations to a remote server via the client SDK. It caches the
// proto.Workspace returned at creation time and refreshes it after
// config-mutating operations.
type ClientWorkspace struct {
	client *client.Client

	mu     sync.RWMutex
	ws     proto.Workspace
	skills *skills.Manager
	// lastSession is the most recent session ID reported via
	// SetCurrentSession. The subscription loop re-asserts it after a
	// reconnect, because the server's per-client presence entry (or the
	// whole workspace) may have been re-created in the meantime.
	lastSession string

	// subCtx bounds the lifetime of the event subscription (and its
	// reconnect loop). Shutdown cancels it so Subscribe stops
	// reconnecting instead of racing the teardown.
	subCtx    context.Context
	subCancel context.CancelFunc
	// subStarted reports whether the subscription loop ever ran, and
	// subDone is closed when it returns. Shutdown uses them to let an
	// in-flight workspace recovery finish before it says goodbye to the
	// server, so the workspace it releases is the one recovery just
	// minted.
	subStarted atomic.Bool
	subDone    chan struct{}

	// herdrClient reports agent state to herdr when running inside
	// a herdr-managed pane. Nil when not in a herdr environment.
	herdrClient *herdr.Client

	// ensureEngine starts the project's engine again after it was lost
	// (engine-design.md 5.14). Set only for engine connections; unused
	// until piece A.
	ensureEngine func(context.Context) error

	// shellOutputs feeds live `!` output to the commands waiting for it
	// (engine-design.md 5.9 a).
	shellOutputs shellOutputs
}

// SSE reconnect backoff bounds for the workspace event stream. Declared
// as vars (not consts) so tests can shrink the delays.
var (
	sseReconnectInitialBackoff = 250 * time.Millisecond
	sseReconnectMaxBackoff     = 10 * time.Second
)

// NewClientWorkspace creates a new ClientWorkspace that proxies all
// operations through the given client SDK. The ws parameter is the
// proto.Workspace snapshot returned by the server at creation time. The
// snapshot's Skills field seeds a process-local skills.Manager so the
// TUI sees discovery state before the first SSE event arrives. The
// manager is constructed with WithGlobalMirror because the client
// process represents exactly one workspace and the TUI reads
// skills.GetLatestStates directly at construction time.
func NewClientWorkspace(c *client.Client, ws proto.Workspace) *ClientWorkspace {
	if ws.Config != nil {
		ws.Config.SetupAgents()
		ws.Config.NormalizeOptions()
	}
	states := proto.SkillStatesToDomain(ws.Skills)
	mgr := skills.NewManager(nil, nil, states, skills.WithGlobalMirror())
	subCtx, subCancel := context.WithCancel(context.Background())
	return &ClientWorkspace{
		client:      c,
		ws:          ws,
		skills:      mgr,
		subCtx:      subCtx,
		subCancel:   subCancel,
		subDone:     make(chan struct{}),
		herdrClient: herdr.Init(),
	}
}

// refreshWorkspace re-fetches the workspace from the server, updating
// the cached snapshot. Called after config-mutating operations.
func (w *ClientWorkspace) refreshWorkspace() {
	updated, err := w.client.GetWorkspace(context.Background(), w.workspaceID())
	if err != nil {
		slog.Error("Failed to refresh workspace", "error", err)
		return
	}
	if updated.Config != nil {
		updated.Config.SetupAgents()
		updated.Config.NormalizeOptions()
	}
	w.mu.Lock()
	w.ws = *updated
	w.mu.Unlock()
}

// cached returns a snapshot of the cached workspace.
func (w *ClientWorkspace) cached() proto.Workspace {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ws
}

// workspaceID returns the cached workspace ID.
func (w *ClientWorkspace) workspaceID() string {
	return w.cached().ID
}

// -- Sessions --

func (w *ClientWorkspace) CreateSession(ctx context.Context, title string) (session.Session, error) {
	sess, err := w.client.CreateSession(ctx, w.workspaceID(), title)
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*sess), nil
}

func (w *ClientWorkspace) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	sess, err := w.client.GetSession(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*sess), nil
}

func (w *ClientWorkspace) ListSessions(ctx context.Context) ([]session.Session, error) {
	protoSessions, err := w.client.ListSessions(ctx, w.workspaceID())
	if err != nil {
		return nil, err
	}
	return proto.SessionsToDomain(protoSessions), nil
}

func (w *ClientWorkspace) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	saved, err := w.client.SaveSession(ctx, w.workspaceID(), proto.SessionFromDomain(sess))
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*saved), nil
}

func (w *ClientWorkspace) DeleteSession(ctx context.Context, sessionID string) error {
	return w.client.DeleteSession(ctx, w.workspaceID(), sessionID)
}

func (w *ClientWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return fmt.Sprintf("%s$$%s", messageID, toolCallID)
}

func (w *ClientWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	parts := strings.Split(sessionID, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// SetCurrentSession reports the session this client is currently
// viewing to the server. Empty sessionID clears the entry. The report
// scopes the client's event stream: permission and question prompts
// are only delivered to clients viewing the session that raised them.
// Errors are propagated to the caller; the TUI logs and ignores them
// since the presence record is best-effort, not correctness-critical
// state.
func (w *ClientWorkspace) SetCurrentSession(ctx context.Context, sessionID string) error {
	w.herdrClient.SetSessionID(sessionID)
	w.mu.Lock()
	w.lastSession = sessionID
	w.mu.Unlock()
	return w.client.SetCurrentSession(ctx, w.workspaceID(), sessionID)
}

// RoutesChannelEvents reports true: the server backend injects each
// channel event exactly once (see backend.startChannelRouter), so this
// client must not inject on EventChannelMessage — with several clients
// attached, per-client injection would run the same event multiple
// times, and it would never run with zero clients attached.
func (w *ClientWorkspace) RoutesChannelEvents() bool { return true }

// -- Messages --

func (w *ClientWorkspace) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	msgs, err := w.client.ListMessages(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	return proto.MessagesToDomain(msgs), nil
}

func (w *ClientWorkspace) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	msgs, err := w.client.ListUserMessages(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	return proto.MessagesToDomain(msgs), nil
}

func (w *ClientWorkspace) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	msgs, err := w.client.ListAllUserMessages(ctx, w.workspaceID())
	if err != nil {
		return nil, err
	}
	return proto.MessagesToDomain(msgs), nil
}

// -- Agent --

func (w *ClientWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error {
	// SubmissionID tracks optimistic UI feedback. Keep RunID empty so
	// interactive follow-ups can still be steered into the running turn.
	return w.client.SendMessage(ctx, w.workspaceID(), sessionID, "", "", prompt, attachments...)
}

func (w *ClientWorkspace) AgentRunChannel(ctx context.Context, channel, sessionID, prompt string, attachments ...message.Attachment) error {
	return w.client.SendMessage(ctx, w.workspaceID(), sessionID, "", channel, prompt, attachments...)
}

// AgentRunShellCommand runs a user shell command on the server. With
// onProgress, the command gets a new request ID and the engine's
// shell_output events for it feed onProgress until the command returns;
// the returned output stays the authoritative one.
func (w *ClientWorkspace) AgentRunShellCommand(ctx context.Context, sessionID, command string, termWidth int, onProgress func(string), isFirstMessage bool) (proto.ShellCommandResponse, error) {
	if onProgress == nil {
		return w.client.RunShellCommand(ctx, w.workspaceID(), sessionID, command, termWidth, isFirstMessage)
	}
	requestID := uuid.NewString()
	done := w.shellOutputs.wait(requestID, onProgress)
	defer done()
	return w.client.RunShellCommandLive(ctx, w.workspaceID(), sessionID, command, termWidth, isFirstMessage, requestID)
}

func (w *ClientWorkspace) AgentCancel(sessionID string) {
	_ = w.client.CancelAgentSession(context.Background(), w.workspaceID(), sessionID)
}

// AgentBackground moves the session's running command to the background
// (Ctrl+B) through the engine. A failed call is logged and reports false.
func (w *ClientWorkspace) AgentBackground(sessionID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), agentBackgroundTimeout)
	defer cancel()
	moved, err := w.client.MoveToBackground(ctx, w.workspaceID(), sessionID)
	if err != nil {
		slog.Warn("Could not move the running command to the background", "error", err)
		return false
	}
	return moved
}

// agentBackgroundTimeout bounds the Ctrl+B call, which the UI waits on.
const agentBackgroundTimeout = 2 * time.Second

func (w *ClientWorkspace) AgentInterrupt(sessionID string) {
	_ = w.client.InterruptAgentSession(context.Background(), w.workspaceID(), sessionID)
}

func (w *ClientWorkspace) AgentRecallQueuedPrompt(ctx context.Context, sessionID string) (*message.QueuedPrompt, error) {
	prompt, err := w.client.RecallAgentSessionQueuedPrompt(ctx, w.workspaceID(), sessionID)
	if err != nil || prompt == nil {
		return nil, err
	}
	return &message.QueuedPrompt{Prompt: prompt.Prompt, Attachments: proto.AttachmentsToMessage(prompt.Attachments), SubmissionID: prompt.SubmissionID}, nil
}

func (w *ClientWorkspace) AgentIsBusy() bool {
	info, err := w.client.GetAgentInfo(context.Background(), w.workspaceID())
	if err != nil {
		return false
	}
	return info.IsBusy
}

func (w *ClientWorkspace) AgentIsSessionBusy(sessionID string) bool {
	info, err := w.client.GetAgentSessionInfo(context.Background(), w.workspaceID(), sessionID)
	if err != nil {
		return false
	}
	return info.IsBusy
}

func (w *ClientWorkspace) AgentModel() AgentModel {
	info, err := w.client.GetAgentInfo(context.Background(), w.workspaceID())
	if err != nil {
		return AgentModel{}
	}
	return AgentModel{
		CatwalkCfg: info.Model,
		ModelCfg:   info.ModelCfg,
	}
}

func (w *ClientWorkspace) AgentIsReady() bool {
	return w.AgentReadyErr() == nil
}

func (w *ClientWorkspace) AgentReadyErr() error {
	info, err := w.client.GetAgentInfo(context.Background(), w.workspaceID())
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			// The server answered, it just does not know this workspace
			// any more. The subscription loop is already re-registering;
			// saying "lost connection" here would be plainly wrong.
			return ErrWorkspaceGone
		}
		// The workspace/server could not be reached. This is distinct
		// from an initialized-but-not-ready agent: the server may have
		// torn the workspace down or restarted underneath us.
		return fmt.Errorf("%w: %v", ErrServerUnreachable, err)
	}
	if !info.IsReady {
		return ErrAgentNotInitialized
	}
	return nil
}

func (w *ClientWorkspace) AgentQueuedPrompts(sessionID string) int {
	count, err := w.client.GetAgentSessionQueuedPrompts(context.Background(), w.workspaceID(), sessionID)
	if err != nil {
		return 0
	}
	return count
}

func (w *ClientWorkspace) AgentQueuedPromptsList(sessionID string) []string {
	prompts, err := w.client.GetAgentSessionQueuedPromptsList(context.Background(), w.workspaceID(), sessionID)
	if err != nil {
		return nil
	}
	return prompts
}

func (w *ClientWorkspace) AgentClearQueue(sessionID string) {
	_ = w.client.ClearAgentSessionQueuedPrompts(context.Background(), w.workspaceID(), sessionID)
}

func (w *ClientWorkspace) AgentSetMain(agentID string) error {
	return w.client.SetMainAgent(context.Background(), w.workspaceID(), agentID)
}

func (w *ClientWorkspace) AgentSummarize(ctx context.Context, sessionID string) error {
	return w.client.AgentSummarizeSession(ctx, w.workspaceID(), sessionID)
}

func (w *ClientWorkspace) UpdateAgentModel(ctx context.Context) error {
	return w.client.UpdateAgent(ctx, w.workspaceID())
}

func (w *ClientWorkspace) InitCoderAgent(ctx context.Context) error {
	return w.client.InitiateAgentProcessing(ctx, w.workspaceID(), true)
}

func (w *ClientWorkspace) InitCoderAgentNonInteractive(ctx context.Context) error {
	return w.client.InitiateAgentProcessing(ctx, w.workspaceID(), false)
}

func (w *ClientWorkspace) GetDefaultSmallModel(providerID string) config.SelectedModel {
	model, err := w.client.GetDefaultSmallModel(context.Background(), w.workspaceID(), providerID)
	if err != nil {
		return config.SelectedModel{}
	}
	return *model
}

// -- Permissions --

func (w *ClientWorkspace) PermissionGrant(perm permission.PermissionRequest) bool {
	resolved, _ := w.client.GrantPermission(context.Background(), w.workspaceID(), proto.PermissionGrant{
		Permission: proto.PermissionRequestFromDomain(perm),
		Action:     proto.PermissionAllow,
	})
	return resolved
}

func (w *ClientWorkspace) PermissionGrantPersistent(perm permission.PermissionRequest) bool {
	resolved, _ := w.client.GrantPermission(context.Background(), w.workspaceID(), proto.PermissionGrant{
		Permission: proto.PermissionRequestFromDomain(perm),
		Action:     proto.PermissionAllowForSession,
	})
	return resolved
}

func (w *ClientWorkspace) PermissionDeny(perm permission.PermissionRequest) bool {
	resolved, _ := w.client.GrantPermission(context.Background(), w.workspaceID(), proto.PermissionGrant{
		Permission: proto.PermissionRequestFromDomain(perm),
		Action:     proto.PermissionDeny,
	})
	return resolved
}

func (w *ClientWorkspace) PermissionSkipRequests() bool {
	skip, err := w.client.GetPermissionsSkipRequests(context.Background(), w.workspaceID())
	if err != nil {
		return false
	}
	return skip
}

func (w *ClientWorkspace) PermissionSetSkipRequests(skip bool) {
	_ = w.client.SetPermissionsSkipRequests(context.Background(), w.workspaceID(), skip)
}

// -- Questions --

// QuestionAnswer submits answers for a question via the client SDK.
func (w *ClientWorkspace) QuestionAnswer(responses []question.Answer) bool {
	resolved, err := w.client.AnswerQuestionBatch(context.Background(), w.workspaceID(), proto.QuestionAnswerFromDomain(responses))
	if err != nil {
		slog.Error("Failed to answer question", "error", err)
		return false
	}
	return resolved
}

// QuestionCancel cancels the pending question via the client SDK.
func (w *ClientWorkspace) QuestionCancel() bool {
	cancelled, err := w.client.CancelQuestionBatch(context.Background(), w.workspaceID())
	if err != nil {
		slog.Error("Failed to cancel question", "error", err)
		return false
	}
	return cancelled
}

// -- FileTracker --

func (w *ClientWorkspace) FileTrackerRecordRead(ctx context.Context, sessionID, path string) {
	_ = w.client.FileTrackerRecordRead(ctx, w.workspaceID(), sessionID, path)
}

func (w *ClientWorkspace) FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time {
	t, err := w.client.FileTrackerLastReadTime(ctx, w.workspaceID(), sessionID, path)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (w *ClientWorkspace) FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return w.client.FileTrackerListReadFiles(ctx, w.workspaceID(), sessionID)
}

// -- History --

func (w *ClientWorkspace) ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error) {
	files, err := w.client.ListSessionHistoryFiles(ctx, w.workspaceID(), sessionID)
	if err != nil {
		return nil, err
	}
	return proto.FilesToDomain(files), nil
}

// -- LSP --

func (w *ClientWorkspace) LSPStart(ctx context.Context, path string) {
	_ = w.client.LSPStart(ctx, w.workspaceID(), path)
}

func (w *ClientWorkspace) LSPStopAll(ctx context.Context) {
	_ = w.client.LSPStopAll(ctx, w.workspaceID())
}

func (w *ClientWorkspace) LSPGetStates() map[string]LSPClientInfo {
	states, err := w.client.GetLSPs(context.Background(), w.workspaceID())
	if err != nil {
		return nil
	}
	result := make(map[string]LSPClientInfo, len(states))
	for k, v := range states {
		result[k] = LSPClientInfo{
			Name:            v.Name,
			State:           v.State,
			Error:           v.Error,
			DiagnosticCount: v.DiagnosticCount,
			ConnectedAt:     v.ConnectedAt,
		}
	}
	return result
}

func (w *ClientWorkspace) LSPGetDiagnosticCounts(name string) lsp.DiagnosticCounts {
	diags, err := w.client.GetLSPDiagnostics(context.Background(), w.workspaceID(), name)
	if err != nil {
		return lsp.DiagnosticCounts{}
	}
	var counts lsp.DiagnosticCounts
	for _, fileDiags := range diags {
		for _, d := range fileDiags {
			switch d.Severity {
			case protocol.SeverityError:
				counts.Error++
			case protocol.SeverityWarning:
				counts.Warning++
			case protocol.SeverityInformation:
				counts.Information++
			case protocol.SeverityHint:
				counts.Hint++
			}
		}
	}
	return counts
}

// -- Config (read-only) --

func (w *ClientWorkspace) Config() *config.Config {
	return w.cached().Config
}

func (w *ClientWorkspace) WorkingDir() string {
	return w.cached().Path
}

// GitBranch asks the server for the branch checked out in the workspace's
// working directory. Callers keep this off the render path; the TUI polls it
// on a ticker and renders from its own state.
func (w *ClientWorkspace) GitBranch(ctx context.Context) (string, error) {
	return w.client.GitBranch(ctx, w.workspaceID())
}

// ProviderAvailability asks the server why a provider cannot run in the
// workspace's folder. The model menu uses it to grey those rows out.
func (w *ClientWorkspace) ProviderAvailability(ctx context.Context) (map[string]string, error) {
	return w.client.ProviderAvailability(ctx, w.workspaceID())
}

func (w *ClientWorkspace) Resolver() config.VariableResolver {
	return config.IdentityResolver()
}

// -- Config mutations --

func (w *ClientWorkspace) UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	err := w.client.UpdatePreferredModel(context.Background(), w.workspaceID(), scope, modelType, model)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

func (w *ClientWorkspace) SetCompactMode(scope config.Scope, enabled bool) error {
	err := w.client.SetCompactMode(context.Background(), w.workspaceID(), scope, enabled)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

func (w *ClientWorkspace) SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error {
	err := w.client.SetProviderAPIKey(context.Background(), w.workspaceID(), scope, providerID, apiKey)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

func (w *ClientWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	err := w.client.SetConfigField(context.Background(), w.workspaceID(), scope, key, value)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

func (w *ClientWorkspace) SetConfigFields(scope config.Scope, fields map[string]any) error {
	for key, value := range fields {
		if err := w.SetConfigField(scope, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (w *ClientWorkspace) RemoveConfigField(scope config.Scope, key string) error {
	err := w.client.RemoveConfigField(context.Background(), w.workspaceID(), scope, key)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

func (w *ClientWorkspace) ImportCopilot() (*oauth.Token, bool) {
	token, ok, err := w.client.ImportCopilot(context.Background(), w.workspaceID())
	if err != nil {
		return nil, false
	}
	if ok {
		w.refreshWorkspace()
	}
	return token, ok
}

func (w *ClientWorkspace) RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error {
	err := w.client.RefreshOAuthToken(ctx, w.workspaceID(), scope, providerID)
	if err == nil {
		w.refreshWorkspace()
	}
	return err
}

// -- Project lifecycle --

func (w *ClientWorkspace) ProjectNeedsInitialization() (bool, error) {
	return w.client.ProjectNeedsInitialization(context.Background(), w.workspaceID())
}

func (w *ClientWorkspace) MarkProjectInitialized() error {
	return w.client.MarkProjectInitialized(context.Background(), w.workspaceID())
}

func (w *ClientWorkspace) InitializePrompt() (string, error) {
	return w.client.GetInitializePrompt(context.Background(), w.workspaceID())
}

func (w *ClientWorkspace) ListSkills(ctx context.Context) ([]skills.CatalogEntry, error) {
	entries, err := w.client.ListSkills(ctx, w.workspaceID())
	if err != nil {
		return nil, err
	}
	return proto.SkillInfosToDomain(entries), nil
}

func (w *ClientWorkspace) ReadSkill(ctx context.Context, skillID string) ([]byte, skills.SkillReadResult, error) {
	resp, err := w.client.ReadSkill(ctx, w.workspaceID(), skillID)
	if err != nil {
		return nil, skills.SkillReadResult{}, err
	}
	return resp.Content, proto.SkillReadResultToDomain(resp.Result), nil
}

// -- MCP operations --

// mcpStatesTimeout bounds a single MCP state probe. The client SDK sets no
// request timeout of its own, and the UI serializes refreshes behind an
// in-flight flag: without a deadline a hung request would wedge that flag
// forever, queueing every later refresh behind it and silently freezing the
// MCP sidebar. A var, not a const, so tests can shrink it.
var mcpStatesTimeout = 10 * time.Second

func (w *ClientWorkspace) MCPGetStates() map[string]mcp.ClientInfo {
	ctx, cancel := context.WithTimeout(context.Background(), mcpStatesTimeout)
	defer cancel()
	states, err := w.client.MCPGetStates(ctx, w.workspaceID())
	if err != nil {
		slog.Warn("Failed to fetch MCP states", "error", err)
		return nil
	}
	return proto.MCPClientInfosToDomain(states)
}

func (w *ClientWorkspace) MCPRefreshPrompts(ctx context.Context, name string) {
	_ = w.client.MCPRefreshPrompts(ctx, w.workspaceID(), name)
}

func (w *ClientWorkspace) MCPRefreshResources(ctx context.Context, name string) {
	_ = w.client.MCPRefreshResources(ctx, w.workspaceID(), name)
}

func (w *ClientWorkspace) RefreshMCPTools(ctx context.Context, name string) {
	_ = w.client.RefreshMCPTools(ctx, w.workspaceID(), name)
}

func (w *ClientWorkspace) ReadMCPResource(ctx context.Context, name, uri string) ([]MCPResourceContents, error) {
	return w.client.ReadMCPResource(ctx, w.workspaceID(), name, uri)
}

func (w *ClientWorkspace) ListMCPPrompts(ctx context.Context) ([]commands.MCPPrompt, error) {
	prompts, err := w.client.ListMCPPrompts(ctx, w.workspaceID())
	if err != nil {
		return nil, err
	}
	return proto.MCPPromptsToDomain(prompts), nil
}

func (w *ClientWorkspace) GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error) {
	return w.client.GetMCPPrompt(context.Background(), w.workspaceID(), clientID, promptID, args)
}

func (w *ClientWorkspace) EnableDockerMCP(ctx context.Context) error {
	return w.client.EnableDockerMCP(ctx, w.workspaceID())
}

func (w *ClientWorkspace) DisableDockerMCP() error {
	return w.client.DisableDockerMCP(context.Background(), w.workspaceID())
}

func (w *ClientWorkspace) MCPServersDisabled(ctx context.Context) ([]string, error) {
	return w.client.MCPServersDisabled(ctx, w.workspaceID())
}

func (w *ClientWorkspace) MCPServersEnabled(ctx context.Context) ([]string, error) {
	return w.client.MCPServersEnabled(ctx, w.workspaceID())
}

func (w *ClientWorkspace) MCPSetServerDisabled(ctx context.Context, name string, disabled bool) error {
	return w.client.SetMCPServerDisabled(ctx, w.workspaceID(), name, disabled)
}

func (w *ClientWorkspace) MCPSetServerConfigDisabled(ctx context.Context, name string, disabled bool) error {
	return w.client.SetMCPServerConfigDisabled(ctx, w.workspaceID(), name, disabled)
}

func (w *ClientWorkspace) MCPStartServer(ctx context.Context, name string) error {
	return w.client.StartMCPServer(ctx, w.workspaceID(), name)
}

func (w *ClientWorkspace) MCPAuthenticate(ctx context.Context, name string) error {
	// The server suppresses its own browser open for this flow; the client
	// polls the auth URL and opens it locally so the user authorizes on
	// their own machine. The OAuth callback listener runs on the server
	// (localhost ports shared when server and client are co-located).
	authErr := make(chan error, 1)
	go func() {
		authErr <- w.client.MCPAuthenticate(ctx, w.workspaceID(), name)
	}()

	// Poll for the authorization URL so we can open it in the local
	// browser as soon as the flow generates one.
	var opened bool
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-authErr:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if opened {
				continue
			}
			if u := w.MCPAuthURL(name); u != "" {
				if err := browser.OpenURL(u); err != nil {
					slog.Warn("Failed to open MCP OAuth URL in browser", "error", err)
				}
				opened = true
			}
		}
	}
}

func (w *ClientWorkspace) MCPPendingAuth() []mcp.PendingAuthServer {
	pending, err := w.client.MCPPendingAuth(context.Background(), w.workspaceID())
	if err != nil {
		slog.Warn("Failed to fetch MCP pending auth", "error", err)
		return nil
	}
	return proto.MCPPendingAuthToDomain(pending)
}

func (w *ClientWorkspace) MCPAuthURL(name string) string {
	// The server's in-progress authorization URL is exposed through the
	// pending-auth list while the flow runs; a server in StateNeedsAuth
	// paired with an active flow reports its URL here. Poll the server
	// for the in-flight URL.
	u, err := w.client.MCPAuthURL(context.Background(), w.workspaceID(), name)
	if err != nil {
		return ""
	}
	return u
}

// -- Lifecycle --

func (w *ClientWorkspace) SetSessionModel(ctx context.Context, sessionID string, model session.ModelChoice) (session.Session, error) {
	sess, err := w.client.SetSessionModel(ctx, w.workspaceID(), sessionID, model)
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*sess), nil
}

func (w *ClientWorkspace) SetKeptSkill(ctx context.Context, sessionID string, skill session.KeptSkill, on bool) (session.Session, error) {
	sess, err := w.client.SetKeptSkill(ctx, w.workspaceID(), sessionID, proto.KeptSkill{ID: skill.ID, Name: skill.Name, ByAgent: skill.ByAgent}, on)
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*sess), nil
}
