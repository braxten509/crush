package rpc

import (
	"net/http"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

const (
	get  = http.MethodGet
	post = http.MethodPost
	put  = http.MethodPut
	del  = http.MethodDelete
)

// System endpoints.
var (
	Health  = Op[Empty, Empty]{Method: get, Path: "/v1/health"}
	Version = Op[Empty, proto.VersionInfo]{Method: get, Path: "/v1/version"}
	Control = Op[proto.ServerControl, Empty]{Method: post, Path: "/v1/control"}
	// ServerConfig returns the server-level config store, which has no
	// exported fields and so encodes as an empty object.
	ServerConfig = Op[Empty, *config.ConfigStore]{Method: get, Path: "/v1/config"}
	RetireClient = Op[Empty, Empty]{Method: del, Path: "/v1/clients/{client_id}"}
)

// Workspace endpoints.
var (
	ListWorkspaces     = Op[Empty, []proto.Workspace]{Method: get, Path: "/v1/workspaces"}
	CreateWorkspace    = Op[proto.Workspace, proto.Workspace]{Method: post, Path: "/v1/workspaces"}
	GetWorkspace       = Op[Empty, proto.Workspace]{Method: get, Path: "/v1/workspaces/{id}"}
	DeleteWorkspace    = Op[Empty, Empty]{Method: del, Path: "/v1/workspaces/{id}"}
	SetCurrentSession  = Op[proto.CurrentSession, Empty]{Method: post, Path: "/v1/workspaces/{id}/current-session"}
	WorkspaceConfig    = Op[Empty, *config.Config]{Method: get, Path: "/v1/workspaces/{id}/config"}
	WorkspaceProviders = Op[Empty, []catwalk.Provider]{Method: get, Path: "/v1/workspaces/{id}/providers"}
	// WorkspaceEvents streams Server-Sent Events rather than a JSON
	// body; only its route is shared.
	WorkspaceEvents   = Op[Empty, Empty]{Method: get, Path: "/v1/workspaces/{id}/events"}
	AllUserMessages   = Op[Empty, []proto.Message]{Method: get, Path: "/v1/workspaces/{id}/messages/user"}
	GitBranch         = Op[Empty, proto.GitBranchResponse]{Method: get, Path: "/v1/workspaces/{id}/git/branch"}
	ProviderUsable    = Op[Empty, proto.ProviderAvailabilityResponse]{Method: get, Path: "/v1/workspaces/{id}/providers/availability"}
	ProjectNeedsInit  = Op[Empty, proto.ProjectNeedsInitResponse]{Method: get, Path: "/v1/workspaces/{id}/project/needs-init"}
	ProjectInit       = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/project/init"}
	ProjectInitPrompt = Op[Empty, proto.ProjectInitPromptResponse]{Method: get, Path: "/v1/workspaces/{id}/project/init-prompt"}
)

// Session endpoints.
var (
	ListSavedSessions    = Op[Empty, []session.Session]{Method: get, Path: "/v1/workspaces/{id}/saved-sessions"}
	ChangeSavedSession   = Op[session.CatalogChange, struct{}]{Method: post, Path: "/v1/workspaces/{id}/saved-sessions"}
	GetSavedSession      = Op[Empty, proto.Session]{Method: get, Path: "/v1/workspaces/{id}/saved-sessions/{sid}"}
	SavedSessionMessages = Op[Empty, []proto.Message]{Method: get, Path: "/v1/workspaces/{id}/saved-sessions/{sid}/messages"}
	ListSessions         = Op[Empty, []proto.Session]{Method: get, Path: "/v1/workspaces/{id}/sessions"}
	CreateSession        = Op[proto.Session, proto.Session]{Method: post, Path: "/v1/workspaces/{id}/sessions"}
	GetSession           = Op[Empty, proto.Session]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}"}
	SaveSession          = Op[proto.Session, proto.Session]{Method: put, Path: "/v1/workspaces/{id}/sessions/{sid}"}
	DeleteSession        = Op[Empty, Empty]{Method: del, Path: "/v1/workspaces/{id}/sessions/{sid}"}
	SessionHistory       = Op[Empty, []proto.File]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/history"}
	SessionMessages      = Op[Empty, []proto.Message]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/messages"}
	MessageReview        = Op[Empty, proto.Message]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/messages/{mid}/review"}
	SessionUserMessages  = Op[Empty, []proto.Message]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/messages/user"}
	SetKeptSkill         = Op[proto.SetKeptSkillRequest, proto.Session]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/kept-skills"}
	SetSessionModel      = Op[proto.SetSessionModelRequest, proto.Session]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/model"}
	// The last pick is the model a new chat starts on; a pick made with no
	// chat open sets it. No chat runs on it.
	LastPick    = Op[Empty, config.SelectedModel]{Method: get, Path: "/v1/workspaces/{id}/last-pick"}
	SetLastPick = Op[config.SelectedModel, Empty]{Method: post, Path: "/v1/workspaces/{id}/last-pick"}
)

// File tracker endpoints.
var (
	FileTrackerFiles    = Op[Empty, []string]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/filetracker/files"}
	FileTrackerRead     = Op[proto.FileTrackerReadRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/filetracker/read"}
	FileTrackerLastRead = Op[Empty, time.Time]{Method: get, Path: "/v1/workspaces/{id}/filetracker/lastread"}
)

// LSP endpoints.
var (
	ListLSPs       = Op[Empty, map[string]proto.LSPClientInfo]{Method: get, Path: "/v1/workspaces/{id}/lsps"}
	LSPDiagnostics = Op[Empty, map[protocol.DocumentURI][]protocol.Diagnostic]{Method: get, Path: "/v1/workspaces/{id}/lsps/{lsp}/diagnostics"}
	LSPStart       = Op[proto.LSPStartRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/lsps/start"}
	LSPStopAll     = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/lsps/stop"}
)

// Permission and question endpoints.
var (
	GetPermissionsSkip = Op[Empty, proto.PermissionSkipRequest]{Method: get, Path: "/v1/workspaces/{id}/permissions/skip"}
	SetPermissionsSkip = Op[proto.PermissionSkipRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/permissions/skip"}
	GrantPermission    = Op[proto.PermissionGrant, proto.PermissionGrantResponse]{Method: post, Path: "/v1/workspaces/{id}/permissions/grant"}
	AnswerQuestion     = Op[proto.QuestionAnswer, proto.QuestionAnswerResponse]{Method: post, Path: "/v1/workspaces/{id}/questions/answer"}
	CancelQuestion     = Op[Empty, proto.QuestionAnswerResponse]{Method: post, Path: "/v1/workspaces/{id}/questions/cancel"}
)

// Agent endpoints.
var (
	AgentInfo    = Op[Empty, proto.AgentInfo]{Method: get, Path: "/v1/workspaces/{id}/agent"}
	SendMessage  = Op[proto.AgentMessage, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent", Status: http.StatusAccepted}
	InitAgent    = Op[proto.AgentInitRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/init"}
	UpdateAgent  = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/update"}
	SetMainAgent = Op[proto.AgentSetMainRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/main"}
	AgentSession = Op[Empty, proto.AgentSession]{Method: get, Path: "/v1/workspaces/{id}/agent/sessions/{sid}"}
	CancelAgent  = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/cancel"}
	QueuedCount  = Op[Empty, int]{Method: get, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/prompts/queued"}
	QueuedList   = Op[Empty, []string]{Method: get, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/prompts/list"}
	// QueuedSummaries shares QueuedList's route; the details=true query
	// parameter selects this richer response.
	QueuedSummaries   = Op[Empty, []message.QueuedPromptSummary]{Method: get, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/prompts/list"}
	ClearQueue        = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/prompts/clear"}
	RecallQueued      = Op[Empty, *proto.AgentMessage]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/prompts/recall"}
	InterruptAgent    = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/interrupt"}
	SummarizeSession  = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/summarize"}
	RunShellCommand   = Op[proto.ShellCommandRequest, proto.ShellCommandResponse]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/shell"}
)

// Config endpoints.
var (
	SetConfigField       = Op[proto.ConfigSetRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/set"}
	RemoveConfigField    = Op[proto.ConfigRemoveRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/remove"}
	SetHelperModel = Op[proto.ConfigModelRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/model"}
	SetCompactMode       = Op[proto.ConfigCompactRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/compact"}
	SetProviderAPIKey    = Op[proto.ConfigProviderKeyRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/provider-key"}
	ImportCopilot        = Op[Empty, proto.ImportCopilotResponse]{Method: post, Path: "/v1/workspaces/{id}/config/import-copilot"}
	RefreshOAuthToken    = Op[proto.ConfigRefreshOAuthRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/config/refresh-oauth"}
)

// Skill endpoints.
var (
	ListSkills      = Op[Empty, []proto.SkillInfo]{Method: get, Path: "/v1/workspaces/{id}/skills"}
	ReadSkill       = Op[proto.ReadSkillRequest, proto.ReadSkillResponse]{Method: post, Path: "/v1/workspaces/{id}/skills/read"}
	InstalledSkills = Op[Empty, []skills.InstalledSkill]{Method: get, Path: "/v1/workspaces/{id}/skills/installed"}
	ManageSkill     = Op[proto.ManageSkillRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/skills/manage"}
)

// MCP endpoints.
var (
	RefreshMCPTools            = Op[proto.MCPNameRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/refresh-tools"}
	ReadMCPResource            = Op[proto.MCPReadResourceRequest, []proto.MCPResourceContents]{Method: post, Path: "/v1/workspaces/{id}/mcp/read-resource"}
	ListMCPPrompts             = Op[Empty, []proto.MCPPrompt]{Method: get, Path: "/v1/workspaces/{id}/mcp/prompts"}
	GetMCPPrompt               = Op[proto.MCPGetPromptRequest, proto.MCPGetPromptResponse]{Method: post, Path: "/v1/workspaces/{id}/mcp/get-prompt"}
	MCPStates                  = Op[Empty, map[string]proto.MCPClientInfo]{Method: get, Path: "/v1/workspaces/{id}/mcp/states"}
	MCPPendingAuth             = Op[Empty, []proto.MCPPendingAuthServer]{Method: get, Path: "/v1/workspaces/{id}/mcp/pending-auth"}
	MCPAuthURL                 = Op[Empty, proto.MCPAuthResponse]{Method: get, Path: "/v1/workspaces/{id}/mcp/auth-url"}
	MCPAuthenticate            = Op[proto.MCPNameRequest, proto.MCPAuthResponse]{Method: post, Path: "/v1/workspaces/{id}/mcp/auth"}
	MCPRefreshPrompts          = Op[proto.MCPNameRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/refresh-prompts"}
	MCPRefreshResources        = Op[proto.MCPNameRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/refresh-resources"}
	EnableDockerMCP            = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/docker/enable"}
	DisableDockerMCP           = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/docker/disable"}
	MCPServersDisabled         = Op[Empty, []string]{Method: get, Path: "/v1/workspaces/{id}/mcp/disabled"}
	SetMCPServerDisabled       = Op[proto.MCPSetServerDisabledRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/disabled"}
	MCPServersEnabled          = Op[Empty, []string]{Method: get, Path: "/v1/workspaces/{id}/mcp/enabled"}
	SetMCPServerConfigDisabled = Op[proto.MCPSetServerDisabledRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/config-disabled"}
	StartMCPServer             = Op[proto.MCPNameRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/mcp/start"}
)
