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

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/herdr"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
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
// viewing to the server. Empty sessionID clears the entry. Errors
// are propagated to the caller; the TUI logs and ignores them since
// the presence record is a hint, not correctness-critical state.
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

// AgentRunShellCommand runs a user shell command on the server. Output is
// not streamed in client mode, so onProgress is unused.
func (w *ClientWorkspace) AgentRunShellCommand(ctx context.Context, sessionID, command string, termWidth int, _ func(string), isFirstMessage bool) (proto.ShellCommandResponse, error) {
	return w.client.RunShellCommand(ctx, w.workspaceID(), sessionID, command, termWidth, isFirstMessage)
}

func (w *ClientWorkspace) AgentCancel(sessionID string) {
	_ = w.client.CancelAgentSession(context.Background(), w.workspaceID(), sessionID)
}

// ponytail: in-process only, like sub-agents; add a server route if client
// mode needs it.
func (w *ClientWorkspace) AgentBackground(string) bool { return false }

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

func (w *ClientWorkspace) Subscribe(program *tea.Program) {
	defer log.RecoverPanic("ClientWorkspace.Subscribe", func() {
		slog.Info("TUI subscription panic: attempting graceful shutdown")
		program.Quit()
	})

	w.runSubscription(program.Send)
}

// maxRecoveryEscalate is the number of consecutive failed workspace
// recovery attempts after which the loop tells the UI the connection
// looks unrecoverable. It keeps retrying regardless: a hard stop would
// strand a user whose server comes back a minute later, and Shutdown can
// always cancel it.
const maxRecoveryEscalate = 20

// recoveryCreateTimeout bounds a single re-registration attempt. It is
// generous because workspace startup is slow (config, database, LSP, MCP);
// it exists only so an unresponsive server cannot pin the subscription
// goroutine indefinitely, and the loop simply retries when it trips.
// A var, not a const, so tests can shrink it.
var recoveryCreateTimeout = 30 * time.Second

// runSubscription subscribes to the workspace event stream and forwards
// translated events to send, reconnecting with capped exponential
// backoff whenever the stream drops. It returns only when the
// subscription context is cancelled (via Shutdown). Split out from
// Subscribe so it can be tested without a real *tea.Program.
//
// Two failures need more than a retry. A 404 means the server no longer
// knows this workspace, so resubscribing with the same ID can never
// succeed and the loop re-registers instead. And any stream that closes
// loses whatever was published while the client was away, so every
// re-established stream — even one that reconnects on the first try —
// re-asserts the client's session and asks the UI to resync.
func (w *ClientWorkspace) runSubscription(send func(tea.Msg)) {
	w.subStarted.Store(true)
	defer close(w.subDone)

	backoff := sseReconnectInitialBackoff
	degraded := false
	recoveryFailures := 0
	markDegraded := func(err error, stuck bool) {
		if degraded && !stuck {
			return
		}
		degraded = true
		send(ConnectionEvent{State: ConnectionDegraded, Err: err, Stuck: stuck})
	}

	for {
		if w.subCtx.Err() != nil {
			return
		}

		evc, err := w.client.SubscribeEvents(w.subCtx, w.workspaceID())
		if err != nil {
			if w.subCtx.Err() != nil {
				return
			}
			markDegraded(err, false)
			if !errors.Is(err, client.ErrNotFound) {
				slog.Error("Failed to subscribe to workspace events; retrying",
					"error", err, "retry_in", backoff)
			} else if w.recoverWorkspace() == nil {
				// Re-registered: resubscribe immediately under the fresh
				// workspace ID.
				backoff = sseReconnectInitialBackoff
				continue
			} else if w.subCtx.Err() == nil {
				recoveryFailures++
				if recoveryFailures == maxRecoveryEscalate {
					markDegraded(ErrWorkspaceGone, true)
				}
			}
			if !w.sleepOrDone(backoff) {
				return
			}
			backoff = min(backoff*2, sseReconnectMaxBackoff)
			continue
		}

		if degraded {
			degraded = false
			recoveryFailures = 0
			w.afterReconnect(send)
		}
		backoff = sseReconnectInitialBackoff
		w.consumeEvents(evc, send)

		// The event channel closed: the server restarted, the stream was
		// interrupted, or the workspace briefly went away. Reconnect
		// after a short delay instead of leaving the TUI permanently
		// orphaned, which is what surfaced as a stuck "coder agent is
		// offline".
		if w.subCtx.Err() != nil {
			return
		}
		markDegraded(ErrStreamClosed, false)
		slog.Warn("Workspace event stream closed; reconnecting", "retry_in", backoff)
		if !w.sleepOrDone(backoff) {
			return
		}
		backoff = min(backoff*2, sseReconnectMaxBackoff)
	}
}

// recoverWorkspace re-registers the workspace after the server reported it
// gone: it re-creates it from the cached snapshot (the server's own view of
// path, data dir, flags and env), adopts the new ID, and re-initializes the
// coder agent when the config is ready, mirroring the startup handshake. The
// server's path dedupe means this either rejoins a live sibling workspace or
// mints a fresh one. It must only be called from the subscription goroutine,
// the only writer of the cached ID.
//
// The create deliberately runs detached from the subscription context: the
// server does not abandon a create when the requesting connection goes away,
// so cancelling would hide the outcome while the workspace got registered
// anyway. Riding it out means the ID is known by the time Shutdown looks,
// and retirement covers a lost response regardless. Its own timeout keeps a
// wedged server from pinning the subscription goroutine forever; the client
// SDK sets no request timeout of its own.
func (w *ClientWorkspace) recoverWorkspace() error {
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(w.subCtx), recoveryCreateTimeout,
	)
	defer cancel()
	created, err := w.client.CreateWorkspace(ctx, w.recreateArgs())
	if err != nil {
		slog.Error("Failed to re-register workspace; retrying", "error", err)
		return err
	}
	if created.Config != nil {
		created.Config.SetupAgents()
		created.Config.NormalizeOptions()
	}
	w.mu.Lock()
	oldID := w.ws.ID
	w.ws = *created
	w.mu.Unlock()
	slog.Info("Re-registered workspace after server-side loss",
		"old_id", oldID, "new_id", created.ID)

	if created.Config != nil && created.Config.IsConfigured() {
		if err := w.InitCoderAgent(w.subCtx); err != nil {
			// Matches the startup handshake: agent init failure is
			// logged, not fatal, since the user can still pick a model.
			slog.Error("Failed to initialize coder agent after workspace recovery", "error", err)
		}
	}
	return nil
}

// recreateArgs derives the CreateWorkspace request used for recovery from
// the cached snapshot. The ID is dropped so the server can dedupe by
// path or mint a fresh workspace, and Version carries this client's
// version, matching the startup handshake.
func (w *ClientWorkspace) recreateArgs() proto.Workspace {
	ws := w.cached()
	return proto.Workspace{
		Path:     ws.Path,
		DataDir:  ws.DataDir,
		Debug:    ws.Debug,
		YOLO:     ws.YOLO,
		Channels: ws.Channels,
		Env:      ws.Env,
		Version:  version.Version,
	}
}

// afterReconnect runs once a degraded subscription is re-established. It
// re-asserts the client's current-session selection, since the server's
// presence entry (or the whole workspace) may have been re-created while we
// were away, and tells the UI to resync state published while detached. The
// SSE handler attaches the client before writing its 200, so the presence
// call cannot be rejected as not-attached here.
func (w *ClientWorkspace) afterReconnect(send func(tea.Msg)) {
	w.mu.RLock()
	sid := w.lastSession
	w.mu.RUnlock()
	if sid != "" {
		if err := w.SetCurrentSession(w.subCtx, sid); err != nil {
			slog.Warn("Failed to re-assert current session after reconnect", "error", err)
		}
	}
	send(ConnectionEvent{State: ConnectionRecovered})
}

// sleepOrDone waits for d or until the subscription context is
// cancelled. It reports false when the context was cancelled, signalling
// the caller to stop reconnecting.
func (w *ClientWorkspace) sleepOrDone(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-w.subCtx.Done():
		return false
	}
}

// consumeEvents drives the workspace event loop. It is split out from
// Subscribe so tests can drive it without a real *tea.Program.
// ConfigChanged events trigger a workspace refresh; all other events
// are translated into domain types and forwarded to send.
func (w *ClientWorkspace) consumeEvents(evc <-chan any, send func(tea.Msg)) {
	for ev := range evc {
		// Forward events to herdr if running inside a herdr pane.
		if hev := herdr.Translate(ev); hev != nil {
			w.herdrClient.HandleEvent(hev)
		}

		if _, ok := ev.(pubsub.Event[proto.ConfigChanged]); ok {
			w.refreshWorkspace()
			continue
		}
		translated := w.translateEvent(ev)
		if translated != nil && send != nil {
			send(translated)
		}
	}
}

// shutdownDrainTimeout bounds how long Shutdown waits for the subscription
// loop to stop. Exceeding it is not a correctness problem — retiring the
// client releases whatever a late recovery registers — it only makes the
// goodbye less tidy.
const shutdownDrainTimeout = 5 * time.Second

func (w *ClientWorkspace) Shutdown() {
	// Stop the reconnect/recovery loop first, then wait for it: cancelling
	// alone does not unwind a workspace recovery that is already in
	// flight, and we want to release the workspace that recovery ended up
	// with rather than one it is about to replace.
	if w.subCancel != nil {
		w.subCancel()
	}
	w.awaitSubscription()
	w.herdrClient.Close()

	// Retiring the client releases every claim it holds, on every workspace,
	// and blocks any further create from this client ID. That is what makes
	// teardown exact even when a recovery create's response was lost: the
	// create either landed before this call, and its claim is released here,
	// or it arrives afterwards and registers nothing.
	err := w.client.RetireClient(context.Background())
	if err == nil {
		return
	}
	if !errors.Is(err, client.ErrUnsupported) {
		slog.Warn("Failed to retire client on the server", "error", err)
		return
	}
	// The server predates client retirement, so fall back to releasing
	// the workspace we know about. Nothing better is possible against an
	// older server.
	_ = w.client.DeleteWorkspace(context.Background(), w.workspaceID())
}

// awaitSubscription waits for the subscription loop to return. It returns
// immediately when the loop never started, which is the case for
// workspaces shut down before Subscribe runs.
func (w *ClientWorkspace) awaitSubscription() {
	if !w.subStarted.Load() || w.subDone == nil {
		return
	}
	t := time.NewTimer(shutdownDrainTimeout)
	defer t.Stop()
	select {
	case <-w.subDone:
	case <-t.C:
		slog.Warn("Timed out waiting for the workspace subscription to stop")
	}
}

// translateEvent converts proto-typed SSE events into the domain types
// that the TUI's Update() method expects. Skills events also update the
// process-local skills.Manager so callers reading
// skills.GetLatestStates see fresh data.
func (w *ClientWorkspace) translateEvent(ev any) tea.Msg {
	switch e := ev.(type) {
	case pubsub.Event[proto.LSPEvent]:
		return pubsub.Event[LSPEvent]{
			Type: e.Type,
			Payload: LSPEvent{
				Type:            LSPEventType(e.Payload.Type),
				Name:            e.Payload.Name,
				State:           e.Payload.State,
				Error:           e.Payload.Error,
				DiagnosticCount: e.Payload.DiagnosticCount,
			},
		}
	case pubsub.Event[proto.MCPEvent]:
		return pubsub.Event[mcp.Event]{Type: e.Type, Payload: proto.MCPEventToDomain(e.Payload)}
	case pubsub.Event[proto.PermissionRequest]:
		return pubsub.Event[permission.PermissionRequest]{
			Type:    e.Type,
			Payload: proto.PermissionRequestToDomain(e.Payload),
		}
	case pubsub.Event[proto.PermissionNotification]:
		return pubsub.Event[permission.PermissionNotification]{
			Type:    e.Type,
			Payload: proto.PermissionNotificationToDomain(e.Payload),
		}
	case pubsub.Event[proto.QuestionRequest]:
		return pubsub.Event[question.Request]{
			Type:    e.Type,
			Payload: proto.QuestionRequestToDomain(e.Payload),
		}
	case pubsub.Event[proto.QuestionNotification]:
		return pubsub.Event[question.Notification]{
			Type:    e.Type,
			Payload: question.Notification{BatchID: e.Payload.BatchID},
		}
	case pubsub.Event[proto.Message]:
		return pubsub.Event[message.Message]{
			Type:    e.Type,
			Payload: proto.MessageToDomain(e.Payload),
		}
	case pubsub.Event[proto.Session]:
		return pubsub.Event[session.Session]{
			Type:    e.Type,
			Payload: proto.SessionToDomain(e.Payload),
		}
	case pubsub.Event[proto.File]:
		return pubsub.Event[history.File]{
			Type:    e.Type,
			Payload: proto.FileToDomain(e.Payload),
		}
	case pubsub.Event[proto.AgentEvent]:
		return pubsub.Event[notify.Notification]{
			Type:    e.Type,
			Payload: proto.AgentEventToDomain(e.Payload),
		}
	case pubsub.Event[proto.RunComplete]:
		// Translate the wire-level proto.RunComplete back into the
		// agent's domain notify.RunComplete. Without this case the
		// default branch below warns on every run completion in the
		// server-mode TUI, even though the TUI itself doesn't act
		// on RunComplete — converting silently keeps the workspace
		// event bridge symmetric with the server-side wrapEvent.
		return pubsub.Event[notify.RunComplete]{
			Type:    e.Type,
			Payload: proto.RunCompleteToDomain(e.Payload),
		}
	case pubsub.Event[proto.SkillsEvent]:
		states := proto.SkillStatesToDomain(e.Payload.States)
		if w.skills != nil {
			w.skills.SetLatestStates(states)
		}
		return pubsub.Event[skills.Event]{
			Type:    e.Type,
			Payload: skills.Event{States: states},
		}
	case pubsub.Event[proto.UpdateAvailable]:
		return app.UpdateAvailableMsg{
			CurrentVersion: e.Payload.CurrentVersion,
			LatestVersion:  e.Payload.LatestVersion,
			IsDevelopment:  e.Payload.IsDevelopment,
		}
	default:
		slog.Warn("Unknown event type in translateEvent", "type", fmt.Sprintf("%T", ev))
		return nil
	}
}

func (w *ClientWorkspace) SetKeptSkill(ctx context.Context, sessionID string, skill session.KeptSkill, on bool) (session.Session, error) {
	sess, err := w.client.SetKeptSkill(ctx, w.workspaceID(), sessionID, proto.KeptSkill{ID: skill.ID, Name: skill.Name}, on)
	if err != nil {
		return session.Session{}, err
	}
	return proto.SessionToDomain(*sess), nil
}
