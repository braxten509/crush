package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// setConfigField sets a configuration field. Over TCP, a write that would
// change the CPU setting answers 403 local_only and changes nothing.
func (c *controllerV1) setConfigField(r *http.Request, req proto.ConfigSetRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SetConfigFieldAs(r.PathValue("id"), secureLocalOnly(r) == nil, req.Scope, req.Key, req.Value)
}

// removeConfigField removes a configuration field, with the CPU rule of
// setConfigField.
func (c *controllerV1) removeConfigField(r *http.Request, req proto.ConfigRemoveRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.RemoveConfigFieldAs(r.PathValue("id"), secureLocalOnly(r) == nil, req.Scope, req.Key)
}

// setHelperModel sets the helper model. Each chat owns its model, so any
// other model type is refused.
func (c *controllerV1) setHelperModel(r *http.Request, req proto.ConfigModelRequest) (rpc.Empty, error) {
	if req.ModelType != config.SelectedModelTypeSmall {
		return rpc.Empty{}, badRequest(backend.ErrChatOwnsModel.Error())
	}
	return rpc.Empty{}, c.backend.SetHelperModel(r.PathValue("id"), req.Scope, req.Model)
}

// setCompactMode sets compact mode.
func (c *controllerV1) setCompactMode(r *http.Request, req proto.ConfigCompactRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SetCompactMode(r.PathValue("id"), req.Scope, req.Enabled)
}

// setProviderAPIKey sets a provider's API key or OAuth token.
func (c *controllerV1) setProviderAPIKey(r *http.Request, req proto.ConfigProviderKeyRequest) (rpc.Empty, error) {
	apiKey, err := req.DecodeAPIKey()
	if err != nil {
		return rpc.Empty{}, badRequest(err.Error())
	}
	return rpc.Empty{}, c.backend.SetProviderAPIKey(r.PathValue("id"), req.Scope, req.ProviderID, apiKey)
}

// importCopilot imports GitHub Copilot credentials from disk.
func (c *controllerV1) importCopilot(r *http.Request, _ rpc.Empty) (proto.ImportCopilotResponse, error) {
	token, ok, err := c.backend.ImportCopilot(r.PathValue("id"))
	if err != nil {
		return proto.ImportCopilotResponse{}, err
	}
	return proto.ImportCopilotResponse{Token: token, Success: ok}, nil
}

// refreshOAuthToken refreshes a provider's OAuth token.
func (c *controllerV1) refreshOAuthToken(r *http.Request, req proto.ConfigRefreshOAuthRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.RefreshOAuthToken(r.Context(), r.PathValue("id"), req.Scope, req.ProviderID)
}

// getProjectNeedsInit reports whether the project needs initialization.
func (c *controllerV1) getProjectNeedsInit(r *http.Request, _ rpc.Empty) (proto.ProjectNeedsInitResponse, error) {
	needs, err := c.backend.ProjectNeedsInitialization(r.PathValue("id"))
	return proto.ProjectNeedsInitResponse{NeedsInit: needs}, err
}

// markProjectInitialized marks the project as initialized.
func (c *controllerV1) markProjectInitialized(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.MarkProjectInitialized(r.PathValue("id"))
}

// getProjectInitPrompt returns the project initialization prompt.
func (c *controllerV1) getProjectInitPrompt(r *http.Request, _ rpc.Empty) (proto.ProjectInitPromptResponse, error) {
	prompt, err := c.backend.InitializePrompt(r.PathValue("id"))
	return proto.ProjectInitPromptResponse{Prompt: prompt}, err
}

// getGitBranch returns the Git branch of the workspace's working
// directory.
func (c *controllerV1) getGitBranch(r *http.Request, _ rpc.Empty) (proto.GitBranchResponse, error) {
	branch, err := c.backend.GitBranch(r.PathValue("id"))
	return proto.GitBranchResponse{Branch: branch}, err
}

// getProviderAvailability returns, by provider id, why a provider cannot
// run in the workspace's folder.
func (c *controllerV1) getProviderAvailability(r *http.Request, _ rpc.Empty) (proto.ProviderAvailabilityResponse, error) {
	providers, err := c.backend.ProviderAvailability(r.PathValue("id"))
	return proto.ProviderAvailabilityResponse{Providers: providers}, err
}

// listSkills returns the effective visible skills.
func (c *controllerV1) listSkills(r *http.Request, _ rpc.Empty) ([]proto.SkillInfo, error) {
	return c.backend.ListSkills(r.PathValue("id"))
}

// readSkill reads a skill's content by ID.
func (c *controllerV1) readSkill(r *http.Request, req proto.ReadSkillRequest) (proto.ReadSkillResponse, error) {
	content, result, err := c.backend.ReadSkill(r.Context(), r.PathValue("id"), req.SkillID)
	if err != nil {
		return proto.ReadSkillResponse{}, err
	}
	return proto.ReadSkillResponse{Content: content, Result: result}, nil
}

// listInstalledSkills lists installed skills, including disabled ones.
func (c *controllerV1) listInstalledSkills(r *http.Request, _ rpc.Empty) ([]skills.InstalledSkill, error) {
	return c.backend.InstalledSkills(r.PathValue("id"))
}

// manageSkill installs a skill or toggles an installed one.
func (c *controllerV1) manageSkill(r *http.Request, req proto.ManageSkillRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.ManageSkill(r.Context(), r.PathValue("id"), req)
}

// listLSPs returns the state of every LSP client.
func (c *controllerV1) listLSPs(r *http.Request, _ rpc.Empty) (map[string]proto.LSPClientInfo, error) {
	states, err := c.backend.GetLSPStates(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	result := make(map[string]proto.LSPClientInfo, len(states))
	for k, v := range states {
		result[k] = proto.LSPClientInfo{
			Name:            v.Name,
			State:           v.State,
			Error:           v.Error,
			DiagnosticCount: v.DiagnosticCount,
			ConnectedAt:     v.ConnectedAt,
		}
	}
	return result, nil
}

// getLSPDiagnostics returns the diagnostics of an LSP client.
func (c *controllerV1) getLSPDiagnostics(r *http.Request, _ rpc.Empty) (map[protocol.DocumentURI][]protocol.Diagnostic, error) {
	return c.backend.GetLSPDiagnostics(r.PathValue("id"), r.PathValue("lsp"))
}

// startLSP starts the language servers for a path.
func (c *controllerV1) startLSP(r *http.Request, req proto.LSPStartRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.LSPStart(r.Context(), r.PathValue("id"), req.Path)
}

// stopAllLSPs stops every language server.
func (c *controllerV1) stopAllLSPs(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.LSPStopAll(r.Context(), r.PathValue("id"))
}
