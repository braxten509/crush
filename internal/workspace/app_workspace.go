package workspace

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	mcptools "github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// AppWorkspace implements the Workspace interface by delegating
// directly to an in-process [app.App] instance. This is the default
// mode when the client/server architecture is not enabled.
type AppWorkspace struct {
	app   *app.App
	store *config.ConfigStore
	// ops holds the operation bodies shared with the server backend.
	ops backend.Ops
}

// NewAppWorkspace creates a new AppWorkspace wrapping the given app
// and config store.
func NewAppWorkspace(a *app.App, store *config.ConfigStore) *AppWorkspace {
	return &AppWorkspace{
		app:   a,
		store: store,
		ops:   backend.Ops{App: a, Cfg: store},
	}
}

// -- Sessions --

func (w *AppWorkspace) CreateSession(ctx context.Context, title string) (session.Session, error) {
	return w.ops.CreateSession(ctx, title)
}

func (w *AppWorkspace) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	return w.ops.GetSession(ctx, sessionID)
}

func (w *AppWorkspace) ListSessions(ctx context.Context) ([]session.Session, error) {
	return w.ops.ListSessions(ctx)
}

func (w *AppWorkspace) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	return w.ops.SaveSession(ctx, sess)
}

func (w *AppWorkspace) DeleteSession(ctx context.Context, sessionID string) error {
	return w.ops.DeleteSession(ctx, sessionID)
}

func (w *AppWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return w.app.Sessions.CreateAgentToolSessionID(messageID, toolCallID)
}

func (w *AppWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	return w.app.Sessions.ParseAgentToolSessionID(sessionID)
}

// SetCurrentSession reports the active session to herdr so the pane
// can persist a resumable reference. Multi-client presence tracking
// is irrelevant in single-client local mode, but herdr still needs
// to know which session is live to support agent resume.
func (w *AppWorkspace) SetCurrentSession(ctx context.Context, sessionID string) error {
	w.app.ReportCurrentSession(sessionID)
	return nil
}

// RoutesChannelEvents reports false: in-process mode has no server to
// route channel events, so the frontend injects them itself.
func (w *AppWorkspace) RoutesChannelEvents() bool { return false }

// -- Messages --

func (w *AppWorkspace) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.ops.ListMessages(ctx, sessionID)
}

func (w *AppWorkspace) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.ops.ListUserMessages(ctx, sessionID)
}

func (w *AppWorkspace) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return w.ops.ListAllUserMessages(ctx)
}

// -- Agent --

func (w *AppWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error {
	if w.app.AgentCoordinator == nil {
		return &message.DefinitiveSubmissionError{Err: ErrAgentNotInitialized}
	}
	_, err := w.app.AgentCoordinator.Run(ctx, sessionID, prompt, attachments...)
	if err != nil {
		return &message.DefinitiveSubmissionError{Err: err}
	}
	return nil
}

func (w *AppWorkspace) AgentRunChannel(ctx context.Context, channel, sessionID, prompt string, attachments ...message.Attachment) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	_, err := w.app.AgentCoordinator.Run(agent.WithChannel(ctx, channel), sessionID, prompt, attachments...)
	return err
}

func (w *AppWorkspace) AgentRunShellCommand(ctx context.Context, sessionID, command string, termWidth int, onProgress func(string), isFirstMessage bool) (proto.ShellCommandResponse, error) {
	return w.ops.RunShellCommand(ctx, backend.ShellCommand{
		SessionID:      sessionID,
		Command:        command,
		Cwd:            w.store.WorkingDir(),
		TermWidth:      termWidth,
		OnProgress:     onProgress,
		IsFirstMessage: isFirstMessage,
	})
}

func (w *AppWorkspace) AgentCancel(sessionID string) {
	w.ops.CancelSession(sessionID)
}

func (w *AppWorkspace) AgentBackground(sessionID string) bool {
	return cliagent.Background(sessionID) || tools.BackgroundShell(sessionID)
}

func (w *AppWorkspace) AgentInterrupt(sessionID string) {
	w.ops.Interrupt(sessionID)
}

func (w *AppWorkspace) AgentRecallQueuedPrompt(_ context.Context, sessionID string) (*message.QueuedPrompt, error) {
	if w.app.AgentCoordinator == nil {
		return nil, ErrAgentNotInitialized
	}
	return w.ops.RecallQueuedPrompt(sessionID)
}

func (w *AppWorkspace) AgentIsBusy() bool {
	if w.app.AgentCoordinator == nil {
		return false
	}
	return w.app.AgentCoordinator.IsBusy()
}

func (w *AppWorkspace) AgentIsSessionBusy(sessionID string) bool {
	return w.ops.IsSessionBusy(sessionID)
}

func (w *AppWorkspace) AgentModel() AgentModel {
	if w.app.AgentCoordinator == nil {
		return AgentModel{}
	}
	m := w.app.AgentCoordinator.Model()
	return AgentModel{
		CatwalkCfg: m.CatwalkCfg,
		ModelCfg:   m.ModelCfg,
	}
}

func (w *AppWorkspace) AgentIsReady() bool {
	return w.app.AgentCoordinator != nil
}

func (w *AppWorkspace) AgentReadyErr() error {
	if w.app.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}
	return nil
}

func (w *AppWorkspace) AgentQueuedPrompts(sessionID string) int {
	return w.ops.QueuedPrompts(sessionID)
}

func (w *AppWorkspace) AgentQueuedPromptsList(sessionID string) []string {
	return w.ops.QueuedPromptsList(sessionID)
}

func (w *AppWorkspace) AgentClearQueue(sessionID string) {
	w.ops.ClearQueue(sessionID)
}

// AgentSetMain switches the active agent. Like the server, it refuses
// while the agent is running.
func (w *AppWorkspace) AgentSetMain(agentID string) error {
	return w.ops.SetMainAgent(agentID)
}

func (w *AppWorkspace) AgentSummarize(ctx context.Context, sessionID string) error {
	return w.ops.Summarize(ctx, sessionID)
}

func (w *AppWorkspace) UpdateAgentModel(ctx context.Context) error {
	return w.app.UpdateAgentModel(ctx)
}

func (w *AppWorkspace) InitCoderAgent(ctx context.Context) error {
	return w.app.InitCoderAgent(ctx)
}

func (w *AppWorkspace) InitCoderAgentNonInteractive(ctx context.Context) error {
	return w.app.InitCoderAgentNonInteractive(ctx)
}

func (w *AppWorkspace) GetDefaultSmallModel(providerID string) config.SelectedModel {
	return w.ops.GetDefaultSmallModel(providerID)
}

// -- Permissions --

func (w *AppWorkspace) PermissionGrant(perm permission.PermissionRequest) bool {
	resolved, _ := w.ops.ResolvePermission(perm, proto.PermissionAllow)
	return resolved
}

func (w *AppWorkspace) PermissionGrantPersistent(perm permission.PermissionRequest) bool {
	resolved, _ := w.ops.ResolvePermission(perm, proto.PermissionAllowForSession)
	return resolved
}

func (w *AppWorkspace) PermissionDeny(perm permission.PermissionRequest) bool {
	resolved, _ := w.ops.ResolvePermission(perm, proto.PermissionDeny)
	return resolved
}

func (w *AppWorkspace) PermissionSkipRequests() bool {
	return w.app.Permissions.SkipRequests()
}

func (w *AppWorkspace) PermissionSetSkipRequests(skip bool) {
	w.app.Permissions.SetSkipRequests(skip)
}

// -- Questions --

func (w *AppWorkspace) QuestionAnswer(responses []question.Answer) bool {
	return w.app.Questions.Answer(responses)
}

func (w *AppWorkspace) QuestionCancel() bool {
	return w.app.Questions.Cancel()
}

// -- FileTracker --

func (w *AppWorkspace) FileTrackerRecordRead(ctx context.Context, sessionID, path string) {
	w.ops.FileTrackerRecordRead(ctx, sessionID, path)
}

func (w *AppWorkspace) FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time {
	return w.ops.FileTrackerLastReadTime(ctx, sessionID, path)
}

func (w *AppWorkspace) FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return w.ops.FileTrackerListReadFiles(ctx, sessionID)
}

// -- History --

func (w *AppWorkspace) ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error) {
	return w.ops.ListSessionHistory(ctx, sessionID)
}

// -- LSP --

func (w *AppWorkspace) LSPStart(ctx context.Context, path string) {
	w.ops.LSPStart(ctx, path)
}

func (w *AppWorkspace) LSPStopAll(ctx context.Context) {
	w.ops.LSPStopAll(ctx)
}

func (w *AppWorkspace) LSPGetStates() map[string]LSPClientInfo {
	states := app.GetLSPStates()
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

func (w *AppWorkspace) LSPGetDiagnosticCounts(name string) lsp.DiagnosticCounts {
	state, ok := app.GetLSPState(name)
	if !ok || state.Client == nil {
		return lsp.DiagnosticCounts{}
	}
	return state.Client.GetDiagnosticCounts()
}

// -- Config (read-only) --

func (w *AppWorkspace) Config() *config.Config {
	return w.store.Config()
}

func (w *AppWorkspace) WorkingDir() string {
	return w.store.WorkingDir()
}

func (w *AppWorkspace) GitBranch(context.Context) (string, error) {
	return w.ops.GitBranch(), nil
}

func (w *AppWorkspace) Resolver() config.VariableResolver {
	return w.store.Resolver()
}

// -- Config mutations --

func (w *AppWorkspace) UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	return w.ops.UpdatePreferredModel(scope, modelType, model)
}

func (w *AppWorkspace) SetCompactMode(scope config.Scope, enabled bool) error {
	return w.ops.SetCompactMode(scope, enabled)
}

func (w *AppWorkspace) SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error {
	return w.ops.SetProviderAPIKey(scope, providerID, apiKey)
}

func (w *AppWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	return w.ops.SetConfigField(scope, key, value)
}

func (w *AppWorkspace) SetConfigFields(scope config.Scope, fields map[string]any) error {
	return w.ops.SetConfigFields(scope, fields)
}

func (w *AppWorkspace) RemoveConfigField(scope config.Scope, key string) error {
	return w.ops.RemoveConfigField(scope, key)
}

func (w *AppWorkspace) ImportCopilot() (*oauth.Token, bool) {
	return w.ops.ImportCopilot()
}

func (w *AppWorkspace) RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error {
	return w.ops.RefreshOAuthToken(ctx, scope, providerID)
}

// -- Project lifecycle --

func (w *AppWorkspace) ProjectNeedsInitialization() (bool, error) {
	return w.ops.ProjectNeedsInitialization()
}

func (w *AppWorkspace) MarkProjectInitialized() error {
	return w.ops.MarkProjectInitialized()
}

func (w *AppWorkspace) InitializePrompt() (string, error) {
	return w.ops.InitializePrompt()
}

func (w *AppWorkspace) ListSkills(_ context.Context) ([]skills.CatalogEntry, error) {
	return w.ops.ListSkills(), nil
}

func (w *AppWorkspace) ReadSkill(_ context.Context, skillID string) ([]byte, skills.SkillReadResult, error) {
	return w.ops.ReadSkill(skillID)
}

// -- MCP operations --

func (w *AppWorkspace) MCPGetStates() map[string]mcptools.ClientInfo {
	return mcptools.GetStates()
}

func (w *AppWorkspace) MCPRefreshPrompts(ctx context.Context, name string) {
	mcptools.RefreshPrompts(ctx, name)
}

func (w *AppWorkspace) MCPRefreshResources(ctx context.Context, name string) {
	mcptools.RefreshResources(ctx, name)
}

func (w *AppWorkspace) RefreshMCPTools(ctx context.Context, name string) {
	w.ops.RefreshMCPTools(ctx, name)
}

func (w *AppWorkspace) ReadMCPResource(ctx context.Context, name, uri string) ([]MCPResourceContents, error) {
	return w.ops.ReadMCPResource(ctx, name, uri)
}

func (w *AppWorkspace) ListMCPPrompts(context.Context) ([]commands.MCPPrompt, error) {
	return w.ops.ListMCPPrompts()
}

func (w *AppWorkspace) GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error) {
	return w.ops.GetMCPPrompt(clientID, promptID, args)
}

func (w *AppWorkspace) EnableDockerMCP(ctx context.Context) error {
	return w.ops.EnableDockerMCP(ctx)
}

func (w *AppWorkspace) DisableDockerMCP() error {
	return w.ops.DisableDockerMCP()
}

// MCPServersDisabled returns the MCP servers disabled for this
// repository. The override set is shared by every session, including
// sub-agent sessions.
func (w *AppWorkspace) MCPServersDisabled(ctx context.Context) ([]string, error) {
	return w.ops.MCPServersDisabled(ctx)
}

// MCPServersEnabled returns the MCP servers with a repository-scoped
// enabled override: config-disabled servers the user enabled here. Startup
// force-starts them so the toggle survives restarts.
func (w *AppWorkspace) MCPServersEnabled(ctx context.Context) ([]string, error) {
	return w.ops.MCPServersEnabled(ctx)
}

// MCPSetServerDisabled adds or removes a repository-scoped MCP override
// and applies it to the running client. Config files are never touched.
func (w *AppWorkspace) MCPSetServerDisabled(ctx context.Context, name string, disabled bool) error {
	return w.ops.SetMCPServerDisabled(ctx, name, disabled)
}

// MCPSetServerConfigDisabled toggles an MCP server's disabled flag in the
// global config and applies the change to the running client unless a
// repository-scoped override takes precedence.
func (w *AppWorkspace) MCPSetServerConfigDisabled(ctx context.Context, name string, disabled bool) error {
	return w.ops.SetMCPServerConfigDisabled(ctx, name, disabled)
}

// MCPStartServer starts the named MCP server even when its config entry is
// disabled. The repository-scoped enabled override recorded by
// MCPSetServerDisabled makes the start survive restarts. Config files are
// never touched.
func (w *AppWorkspace) MCPStartServer(ctx context.Context, name string) error {
	return w.ops.StartMCPServer(ctx, name)
}

// MCPAuthenticate runs the OAuth flow for a named MCP server, opening the
// authorization page in the local browser. (The server suppresses the
// browser and lets the remote client open it instead.)
func (w *AppWorkspace) MCPAuthenticate(ctx context.Context, name string) error {
	return mcptools.AuthenticateMCP(ctx, w.store, name)
}

func (w *AppWorkspace) MCPPendingAuth() []mcptools.PendingAuthServer {
	return w.ops.MCPPendingAuth()
}

func (w *AppWorkspace) MCPAuthURL(name string) string {
	return mcptools.MCPAuthURL(name)
}

// -- Lifecycle --

func (w *AppWorkspace) Subscribe(program *tea.Program) {
	w.app.Subscribe(program)
}

func (w *AppWorkspace) Shutdown() {
	w.app.Shutdown()
}

// App returns the underlying app.App instance.
func (w *AppWorkspace) App() *app.App {
	return w.app
}

// Store returns the underlying config store.
func (w *AppWorkspace) Store() *config.ConfigStore {
	return w.store
}

func (w *AppWorkspace) SetKeptSkill(ctx context.Context, sessionID string, skill session.KeptSkill, on bool) (session.Session, error) {
	return w.ops.SetKeptSkill(ctx, sessionID, skill, on)
}
