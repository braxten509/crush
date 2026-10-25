package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/webtool"
	"github.com/charmbracelet/crush/internal/permission"
)

//go:embed templates/agentic_fetch.md
var agenticFetchToolDescription string

// agenticFetchValidationResult holds the validated parameters from the tool call context.
type agenticFetchValidationResult struct {
	SessionID      string
	AgentMessageID string
}

// validateAgenticFetchParams validates the tool call parameters and extracts required context values.
func validateAgenticFetchParams(ctx context.Context, params tools.AgenticFetchParams) (agenticFetchValidationResult, error) {
	if params.Prompt == "" {
		return agenticFetchValidationResult{}, errors.New("prompt is required")
	}

	sessionID := tools.GetSessionFromContext(ctx)
	if sessionID == "" {
		return agenticFetchValidationResult{}, errors.New("session id missing from context")
	}

	agentMessageID := tools.GetMessageFromContext(ctx)
	if agentMessageID == "" {
		return agenticFetchValidationResult{}, errors.New("agent message id missing from context")
	}

	return agenticFetchValidationResult{
		SessionID:      sessionID,
		AgentMessageID: agentMessageID,
	}, nil
}

//go:embed templates/agentic_fetch_prompt.md.tpl
var agenticFetchPromptTmpl []byte

func (c *coordinator) agenticFetchTool(_ context.Context, client *http.Client) (fantasy.AgentTool, error) {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	return fantasy.NewParallelAgentTool(
		tools.AgenticFetchToolName,
		agenticFetchToolDescription,
		func(ctx context.Context, params tools.AgenticFetchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return c.runAgenticFetch(ctx, client, params, call)
		},
	), nil
}

// runAgenticFetch handles one agentic_fetch call: it asks for permission,
// prepares the page or search prompt and runs a fetch sub-agent on it.
func (c *coordinator) runAgenticFetch(ctx context.Context, client *http.Client, params tools.AgenticFetchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	validationResult, err := validateAgenticFetchParams(ctx, params)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	// Determine description based on mode.
	var description string
	if params.URL != "" {
		description = fmt.Sprintf("Fetch and analyze content from URL: %s", params.URL)
	} else {
		description = "Search the web and analyze results"
	}

	p, err := c.permissions.Request(
		ctx,
		permission.CreatePermissionRequest{
			SessionID:   validationResult.SessionID,
			Path:        c.cfg.WorkingDir(),
			ToolCallID:  call.ID,
			ToolName:    tools.AgenticFetchToolName,
			Action:      "fetch",
			Description: description,
			Params:      tools.AgenticFetchPermissionsParams(params),
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		return tools.NewPermissionDeniedResponse(), nil
	}

	tmpDir, err := os.MkdirTemp(c.cfg.Config().Options.DataDirectory, "crush-fetch-*")
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to create temporary directory: %s", err)), nil
	}
	defer os.RemoveAll(tmpDir)

	fullPrompt, errMsg := agenticFetchPrompt(ctx, client, params, tmpDir)
	if errMsg != "" {
		return fantasy.NewTextErrorResponse(errMsg), nil
	}

	agent, err := c.newAgenticFetchAgent(ctx, client, tmpDir)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}

	return c.runSubAgent(ctx, subAgentParams{
		Agent:          agent,
		SessionID:      validationResult.SessionID,
		AgentMessageID: validationResult.AgentMessageID,
		ToolCallID:     call.ID,
		Prompt:         fullPrompt,
		SessionTitle:   "Fetch Analysis",
		SessionSetup: func(sessionID string) {
			c.permissions.AutoApproveSession(sessionID)
		},
	})
}

// agenticFetchPrompt builds the fetch sub-agent's prompt. In URL mode it
// fetches the page first, saving a large one to tmpDir for the sub-agent to
// search. A non-empty error message means the page could not be prepared.
func agenticFetchPrompt(ctx context.Context, client *http.Client, params tools.AgenticFetchParams, tmpDir string) (string, string) {
	if params.URL == "" {
		// Search mode: let the sub-agent search and fetch as needed.
		return fmt.Sprintf("%s\n\nUse the web_search tool to find relevant information. Break down the question into smaller, focused searches if needed. After searching, use web_fetch to get detailed content from the most relevant results.", params.Prompt), ""
	}

	// URL mode: fetch the URL content first.
	content, err := webtool.FetchURLAndConvert(ctx, client, params.URL)
	if err != nil {
		return "", fmt.Sprintf("Failed to fetch URL: %s", err)
	}

	hasLargeContent := len(content) > tools.LargeContentThreshold
	if !hasLargeContent {
		return fmt.Sprintf("%s\n\nWeb page URL: %s\n\n<webpage_content>\n%s\n</webpage_content>", params.Prompt, params.URL, content), ""
	}

	tempFile, err := os.CreateTemp(tmpDir, "page-*.md")
	if err != nil {
		return "", fmt.Sprintf("Failed to create temporary file: %s", err)
	}
	tempFilePath := tempFile.Name()

	if _, err := tempFile.WriteString(content); err != nil {
		tempFile.Close()
		return "", fmt.Sprintf("Failed to write content to file: %s", err)
	}
	tempFile.Close()

	return fmt.Sprintf("%s\n\nThe web page from %s has been saved to: %s\n\nUse the view and grep tools to analyze this file and extract the requested information.", params.Prompt, params.URL, tempFilePath), ""
}

// newAgenticFetchAgent builds the sub-agent that analyzes fetched content
// in tmpDir, running on the helper model.
func (c *coordinator) newAgenticFetchAgent(ctx context.Context, client *http.Client, tmpDir string) (SessionAgent, error) {
	promptOpts := []prompt.Option{
		prompt.WithWorkingDir(tmpDir),
	}

	promptTemplate, err := prompt.NewPrompt("agentic_fetch", string(agenticFetchPromptTmpl), promptOpts...)
	if err != nil {
		return nil, fmt.Errorf("error creating prompt: %s", err)
	}

	// Web fetches run on the helper model.
	helper, err := c.buildHelperModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("error building the helper model: %s", err)
	}

	systemPrompt, err := promptTemplate.Build(ctx, helper.Model.Provider(), helper.Model.Model(), c.cfg)
	if err != nil {
		return nil, fmt.Errorf("error building system prompt: %s", err)
	}

	helperProviderCfg, ok := c.cfg.Config().Providers.Get(helper.ModelCfg.Provider)
	if !ok {
		return nil, errors.New("helper model provider not configured")
	}

	webFetchTool := webtool.NewWebFetchTool(tmpDir, client)
	webSearchTool := webtool.NewWebSearchTool(client)
	fetchTools := []fantasy.AgentTool{
		webFetchTool,
		webSearchTool,
		tools.NewGlobTool(tmpDir, c.cfg.Config().Tools.Glob),
		tools.NewGrepTool(tmpDir, c.cfg.Config().Tools.Grep),
		webtool.NewSourcegraphTool(client),
		tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, nil, tmpDir),
	}

	// Sub-agent tools run without hook interception. The top-level
	// `agentic_fetch` call itself is already wrapped from the coder's
	// side; firing hooks again for every inner tool call would run
	// the user's hooks N times per delegated turn.

	return NewSessionAgent(SessionAgentOptions{
		Model:                helper,
		HelperModel:          helper,
		SystemPromptPrefix:   helperProviderCfg.SystemPromptPrefix,
		SystemPrompt:         systemPrompt,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Tools:                fetchTools,
	}), nil
}
