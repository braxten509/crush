package agent

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// Coordinator errors.
var (
	errCoderAgentNotConfigured         = errors.New("coder agent not configured")
	errPlanAgentNotConfigured          = errors.New("plan agent not configured")
	errMainAgentNotFound               = errors.New("main agent not found")
	errModelProviderNotConfigured      = errors.New("model provider not configured")
	errLargeModelNotSelected           = errors.New("large model not selected")
	errSmallModelNotSelected           = errors.New("small model not selected")
	errLargeModelProviderNotConfigured = errors.New("large model provider not configured")
	errSmallModelProviderNotConfigured = errors.New("small model provider not configured")
	errLargeModelNotFound              = errors.New("large model not found in provider config")
	errSmallModelNotFound              = errors.New("small model not found in provider config")
)

type Coordinator interface {
	SetMainAgent(agentName string) error
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	// RunAccepted runs a call that was already accepted via
	// BeginAccepted on the fire-and-forget dispatch path. The handle is
	// the only carrier of accept-state across the backend.runAgent /
	// Coordinator / sessionAgent.Run layers: it reaches
	// sessionAgent.Run as SessionAgentCall.Accepted, where it is
	// consumed under dispatchMu once the accepted -> (cancel-on-entry |
	// queued | active) transition is chosen.
	RunAccepted(ctx context.Context, accept *AcceptedRun, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	BeginAccepted(sessionID string) *AcceptedRun
	Cancel(sessionID string)
	Interrupt(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	RecallQueuedPrompt(sessionID string) *message.QueuedPrompt
	Summarize(context.Context, string) error
	Model() Model
	UpdateModels(ctx context.Context) error
	GenerateTitle(ctx context.Context, sessionID, prompt string)
}

type coordinator struct {
	cfg         *config.ConfigStore
	sessions    session.Service
	messages    message.Service
	permissions permission.Service
	questions   question.Service
	history     history.Service
	filetracker filetracker.Service
	lspManager  *lsp.Manager
	notify      pubsub.Publisher[notify.Notification]
	runComplete pubsub.Publisher[notify.RunComplete]
	interactive bool
	tasks       *taskHub

	// agentMu guards mainAgent and mainAgentName: SetMainAgent runs on
	// HTTP handler goroutines while runs, cancels, and probes read the
	// current agent from their own goroutines.
	agentMu       sync.RWMutex
	mainAgent     SessionAgent
	mainAgentName string
	agents        map[string]SessionAgent

	// Skills discovery results (session-start snapshot).
	allSkills    []*skills.Skill // Pre-filter: all discovered after dedup.
	activeSkills []*skills.Skill // Post-filter: active skills only.
	skillTracker *skills.Tracker

	readyWg readyGroup
}

// CoordinatorOptions holds the dependencies for NewCoordinator. Using a
// struct keeps the constructor self-documenting and avoids a long
// positional parameter list.
type CoordinatorOptions struct {
	Config      *config.ConfigStore
	Sessions    session.Service
	Messages    message.Service
	Permissions permission.Service
	Questions   question.Service
	History     history.Service
	FileTracker filetracker.Service
	LSPManager  *lsp.Manager
	Notify      pubsub.Publisher[notify.Notification]
	RunComplete pubsub.Publisher[notify.RunComplete]
	// Tasks receives background sub-agent updates. Nil turns them off.
	Tasks       pubsub.Publisher[Task]
	Skills      *skills.Manager
	Interactive bool
}

func NewCoordinator(ctx context.Context, opts CoordinatorOptions) (Coordinator, error) {
	// Skills are pre-discovered by the caller (see app.New /
	// backend.CreateWorkspace) and passed in via the manager. If no
	// manager was provided (legacy callers), fall back to an in-line
	// discovery so the coordinator still works.
	var allSkills, activeSkills []*skills.Skill
	if opts.Skills != nil {
		allSkills = opts.Skills.AllSkills()
		activeSkills = opts.Skills.ActiveSkills()
	} else {
		allSkills, activeSkills = discoverSkills(opts.Config)
	}
	skillTracker := skills.NewTracker(activeSkills)

	c := &coordinator{
		cfg:          opts.Config,
		sessions:     opts.Sessions,
		messages:     opts.Messages,
		permissions:  opts.Permissions,
		questions:    opts.Questions,
		history:      opts.History,
		filetracker:  opts.FileTracker,
		lspManager:   opts.LSPManager,
		notify:       opts.Notify,
		runComplete:  opts.RunComplete,
		agents:       make(map[string]SessionAgent),
		allSkills:    allSkills,
		activeSkills: activeSkills,
		skillTracker: skillTracker,
		interactive:  opts.Interactive,
	}
	if opts.Tasks != nil {
		c.tasks = newTaskHub(c, opts.Tasks)
	}

	agentCfg, ok := opts.Config.Config().Agents[config.AgentCoder]
	if !ok {
		return nil, errCoderAgentNotConfigured
	}

	coderPrompt, err := coderPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, coderPrompt, agentCfg, false)
	if err != nil {
		return nil, err
	}
	c.agents[config.AgentCoder] = agent

	planCfg, ok := c.cfg.Config().Agents[config.AgentPlan]
	if !ok {
		return nil, errPlanAgentNotConfigured
	}

	planSystemPrompt, err := planPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	planAgent, err := c.buildAgent(ctx, planSystemPrompt, planCfg, false)
	if err != nil {
		return nil, err
	}
	c.agents[config.AgentPlan] = planAgent

	c.mainAgent = agent
	c.mainAgentName = config.AgentCoder
	cliagent.OnUnprompted(func(sessionID string) bool {
		if c.sessions == nil {
			return false
		}
		if _, err := c.sessions.Get(context.Background(), sessionID); err != nil {
			return false // another workspace's session
		}
		ctx := message.WithHiddenUserMessage(withCLIContinue(context.Background()))
		if _, err := c.Run(ctx, sessionID, "A background task finished."); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Showing an agent CLI's reply between turns", "session_id", sessionID, "error", err)
		}
		return true
	})
	return c, nil
}

// activeAgent returns the coordinator's current main agent and its config
// name as one snapshot. Callers use the snapshot for the whole operation,
// so a SetMainAgent racing mid-flight never splits a run, model refresh, or
// summarize across two agents.
func (c *coordinator) activeAgent() (SessionAgent, string) {
	c.agentMu.RLock()
	defer c.agentMu.RUnlock()
	return c.mainAgent, c.mainAgentName
}

// currentAgent returns the current main agent.
func (c *coordinator) currentAgent() SessionAgent {
	agent, _ := c.activeAgent()
	return agent
}

func (c *coordinator) SetMainAgent(agentName string) error {
	c.agentMu.Lock()
	defer c.agentMu.Unlock()
	agent, ok := c.agents[agentName]
	if !ok {
		return fmt.Errorf("%w: %s", errMainAgentNotFound, agentName)
	}
	c.mainAgent = agent
	c.mainAgentName = agentName
	return nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	return c.run(ctx, nil, sessionID, prompt, attachments...)
}

// RunAccepted implements Coordinator.
func (c *coordinator) RunAccepted(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	return c.run(ctx, accept, sessionID, prompt, attachments...)
}

// run is the shared implementation behind Run and RunAccepted. When
// accept is non-nil it is threaded onto the SessionAgentCall as
// Accepted so sessionAgent.Run can consume the accept reservation under
// dispatchMu; when nil (the in-process/local path) no accept tracking
// applies.
func (c *coordinator) run(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.interactive || c.tasks == nil {
		return c.runTurn(ctx, accept, sessionID, prompt, attachments...)
	}
	run, owner := c.tasks.beginSessionRun(ctx, sessionID)
	if !owner {
		var turnCtx context.Context = sessionFollowUpContext{Context: run.ctx, values: ctx}
		if sessionID != run.sessionID {
			// Descendant sessions share cancellation and lifetime, but
			// cannot complete the parent's externally correlated request.
			turnCtx = WithRunID(turnCtx, "")
		} else if run.observe(RunIDFromContext(ctx)) {
			turnCtx = WithRunID(turnCtx, RunIDFromContext(ctx))
			MarkRunCompletePublished(ctx)
		}
		ownTurn := sessionID == run.sessionID
		if ownTurn {
			run.turnStarted()
		}
		result, err := c.runTurn(turnCtx, accept, sessionID, prompt, attachments...)
		if ownTurn {
			run.turnEnded()
		}
		run.release(err)
		return result, err
	}
	defer c.tasks.endSessionRun(run)
	run.turnStarted()
	result, err := c.runTurn(run.ctx, accept, sessionID, prompt, attachments...)
	run.turnEnded()
	run.release(err)
	complete, hasComplete, err := run.wait()
	if err != nil {
		complete.SessionID = sessionID
		hasComplete = true
	}
	if run.ctx.Err() != nil {
		c.currentAgent().Cancel(sessionID)
		err = run.ctx.Err()
		complete.SessionID = sessionID
		complete.Cancelled = true
		hasComplete = true
	}
	if hasComplete {
		complete.RunID = RunIDFromContext(ctx)
		if err != nil {
			complete.Error = err.Error()
		}
		if c.runComplete != nil {
			publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			c.runComplete.PublishMustDeliver(publishCtx, pubsub.UpdatedEvent, complete)
			for _, runID := range run.observerIDs() {
				observed := complete
				observed.RunID = runID
				c.runComplete.PublishMustDeliver(publishCtx, pubsub.UpdatedEvent, observed)
			}
			MarkRunCompletePublished(ctx)
		}
		if err == nil && run.followUps {
			if result == nil {
				result = &fantasy.AgentResult{}
			}
			result.Response.Content = fantasy.ResponseContent{fantasy.TextContent{Text: complete.Text}}
		}
	}
	return result, err
}

func (c *coordinator) runTurn(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (result *fantasy.AgentResult, err error) {
	defer c.recoverTurn(ctx, sessionID, &err)
	if err := c.readyWg.Wait(); err != nil {
		return nil, err
	}

	// MCP servers connect asynchronously (see mcp.Initialize).
	//
	// Interactive runs never wait for that to finish: the tool list below
	// is built from whatever is registered right now, servers still
	// connecting are simply absent from this run's palette, and they are
	// picked up by later runs once they register and publish
	// EventToolsListChanged. Blocking here froze the TUI for the duration
	// of the slowest server's connect timeout whenever a prompt was sent
	// before initialization finished — most visibly on the first message.
	//
	// Non-interactive runs get a single shot at the tool palette, so they
	// do wait for initialization to settle — but bounded by InitWaitBudget
	// rather than each server's connect timeout, so a server wedged
	// mid-handshake cannot stall a headless run for minutes. Past the
	// budget the turn proceeds without the stragglers; their tools simply
	// stay absent from this run.
	if !c.interactive {
		if err := mcp.WaitForInitBudget(ctx, mcp.InitWaitBudget); err != nil {
			return nil, fmt.Errorf("failed to wait for MCP initialization: %w", err)
		}
	}

	// refresh models before each run. Snapshot the agent first: the run,
	// its model settings, and the model refresh below must all target the
	// same agent even if SetMainAgent swaps the main agent mid-flight.
	agent, agentName := c.activeAgent()
	if err := c.updateAgentModels(ctx, agent, agentName); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return nil, errModelProviderNotConfigured
	}

	mergedOptions, temp, topP, topK, freqPenalty, presPenalty := mergeCallOptions(model, providerCfg)

	if err := c.refreshTokenIfExpired(ctx, providerCfg); err != nil {
		// NOTE(@andreynering): We don't return here because the event handling to ask the user to reauthenticate
		// depends on the flow below. If refresh fails, proceed with the token we have.
		slog.Error("Failed to refresh OAuth2 token. Proceeding with existing token.", "error", err)
	}

	// Coalesce per-attempt RunComplete payloads so only the final
	// outcome reaches subscribers. Without this, the first attempt's
	// failed RunComplete (unauthorized) would race ahead of the
	// retry's success, and `crush run` would exit on the stale error
	// before ever seeing the retry result. Each attempt's
	// SessionAgentCall.OnComplete hook overwrites latest; we publish
	// exactly once after retries resolve, via PublishMustDeliver, so
	// a momentarily-full subscriber buffer can't silently drop the
	// terminal event.
	var (
		latest    notify.RunComplete
		hasLatest bool
	)
	onComplete := func(rc notify.RunComplete) {
		latest = rc
		hasLatest = true
	}
	// Unlike the per-turn retry closure, the session owner outlives queued
	// dispatch. Its callback can safely stay on calls after enqueueing.
	sessionRun := sessionRunFromContext(ctx, sessionID)
	if sessionRun != nil {
		onComplete = sessionRun.record
	}
	// Propagate the caller-supplied RunID (set via agent.WithRunID
	// at the HTTP boundary in backend.SendMessage) onto the
	// SessionAgentCall so the terminal RunComplete event echoes it
	// back. Both attempts in the retry chain reuse the same RunID;
	// the coalesce closure publishes the final outcome under that
	// same correlator.
	runID := RunIDFromContext(ctx)
	channel := ChannelFromContext(ctx)
	c.syncSessionChannel(ctx, sessionID, channel)
	run := func() (*fantasy.AgentResult, error) {
		return agent.Run(ctx, SessionAgentCall{
			SessionID:         sessionID,
			SubmissionID:      message.SubmissionID(ctx),
			RunID:             runID,
			Channel:           channel,
			Prompt:            prompt,
			HiddenUserMessage: message.HiddenUserMessage(ctx),
			CLIContinue:       ctx.Value(cliContinueKey{}) != nil,
			Attachments:       attachments,
			MaxOutputTokens:   maxTokens,
			ProviderOptions:   mergedOptions,
			Temperature:       temp,
			TopP:              topP,
			TopK:              providers.CallTopK(providerCfg, topK),
			FrequencyPenalty:  freqPenalty,
			PresencePenalty:   presPenalty,
			OnComplete:        onComplete,
			Accepted:          accept,
			NonInteractive:    !c.interactive,
			sessionRun:        sessionRun,
			OnAuthRefresh:     c.makeAuthRefreshCallback(providerCfg),
		})
	}
	beforeLoaded := c.skillTracker.LoadedNames()
	result, originalErr := run()
	logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)

	// Notify only if still unauthorized after retry — a successful
	// retry means the user doesn't need to re-authenticate. AWS SSO is
	// handled transparently inside OnAuthRefresh, so it needs no post-run
	// notification here.
	if originalErr != nil && isUnauthorized(originalErr) && c.notify != nil && model.ModelCfg.Provider == hyper.Name {
		c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			Type:       notify.TypeReAuthenticate,
			ProviderID: model.ModelCfg.Provider,
		})
	}

	if hasLatest && c.runComplete != nil && sessionRunFromContext(ctx, sessionID) == nil {
		c.runComplete.PublishMustDeliver(ctx, pubsub.UpdatedEvent, latest)
		// Signal to the dispatcher (backend.runAgent) that the
		// authoritative terminal RunComplete for this run was already
		// emitted, so it does not publish a duplicate fallback for the
		// error it is about to receive.
		MarkRunCompletePublished(ctx)
	}
	return result, originalErr
}

// syncSessionChannel reconciles the session's persisted channel binding with
// the origin of the turn about to run. A channel-originated turn (re)binds
// the session to that channel — the newest push wins — and a local turn
// clears a stale binding, since the session is no longer channel-driven once
// the user takes it over directly. This is the binding's whole lifecycle:
// it is only ever a reflection of the most recent turn's origin, and the
// column is dropped with the session row when the session is deleted, so
// there is no separate state to reap.
//
// Failures are logged and the turn proceeds: the binding is provenance for
// reply routing, not a precondition for running.
func (c *coordinator) syncSessionChannel(ctx context.Context, sessionID, channel string) {
	sess, err := c.sessions.Get(ctx, sessionID)
	if err != nil {
		// A missing session is expected (it may be created later in the
		// run), but a real database failure would otherwise be silent.
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("Failed to load session for channel binding sync",
				"session", sessionID, "channel", channel, "error", err)
		}
		return
	}
	if sess.Channel == channel {
		return
	}
	if _, err := c.sessions.SetChannel(ctx, sessionID, channel); err != nil {
		slog.Warn("Failed to sync session channel binding",
			"session", sessionID, "channel", channel, "error", err)
	}
}

// getProviderOptions returns the provider options for a call to model.
func getProviderOptions(model Model, providerCfg config.ProviderConfig) fantasy.ProviderOptions {
	return providers.Options(model.CatwalkCfg, model.ModelCfg, providerCfg)
}

// providerBuilder builds providers and models from the live config.
func (c *coordinator) providerBuilder() providers.Builder {
	return providers.Builder{Config: c.cfg, Permissions: c.permissions, History: c.history}
}

func mergeCallOptions(model Model, cfg config.ProviderConfig) (fantasy.ProviderOptions, *float64, *float64, *int64, *float64, *float64) {
	modelOptions := getProviderOptions(model, cfg)
	temp := cmp.Or(model.ModelCfg.Temperature, model.CatwalkCfg.Options.Temperature)
	topP := cmp.Or(model.ModelCfg.TopP, model.CatwalkCfg.Options.TopP)
	topK := cmp.Or(model.ModelCfg.TopK, model.CatwalkCfg.Options.TopK)
	freqPenalty := cmp.Or(model.ModelCfg.FrequencyPenalty, model.CatwalkCfg.Options.FrequencyPenalty)
	presPenalty := cmp.Or(model.ModelCfg.PresencePenalty, model.CatwalkCfg.Options.PresencePenalty)
	return modelOptions, temp, topP, topK, freqPenalty, presPenalty
}

func (c *coordinator) buildAgent(ctx context.Context, prompt *prompt.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx, isSubAgent)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, _ := c.cfg.Config().Providers.Get(large.ModelCfg.Provider)
	var tasks *taskHub
	if agent.ID == config.AgentCoder {
		tasks = c.tasks
	}
	result := NewSessionAgent(SessionAgentOptions{
		LargeModel:           large,
		SmallModel:           small,
		SystemPromptPrefix:   largeProviderCfg.SystemPromptPrefix,
		SystemPrompt:         "",
		IsSubAgent:           isSubAgent,
		Tasks:                tasks,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Cfg:                  c.cfg,
		Tools:                nil,
		Notify:               c.notify,
		RunComplete:          c.runComplete,
	})

	// The readiness goroutines below perform one-time setup — building the
	// system prompt and the initial tool list — whose results the
	// coordinator needs for its whole lifetime, so they must survive the
	// caller's context being canceled. Several entry points build an agent
	// from a short-lived HTTP request context: the server's
	// InitAgent/UpdateAgent handlers, and UpdateModels -> buildTools ->
	// agentTool -> buildAgent for the sub-agent. The tool-list build reads
	// the MCP registry as it stands; servers still connecting are picked up
	// by later runs. WithoutCancel drops cancellation while keeping context
	// values; the work is local and always completes.
	initCtx := context.WithoutCancel(ctx)

	c.readyWg.Go(func() error {
		systemPrompt, err := prompt.Build(initCtx, large.Model.Provider(), large.Model.Model(), c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	c.readyWg.Go(func() error {
		tools, err := c.buildTools(initCtx, agent, isSubAgent)
		if err != nil {
			return err
		}
		result.SetTools(tools)
		return nil
	})

	return result, nil
}

func (c *coordinator) buildTools(ctx context.Context, agent config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
	var allTools []fantasy.AgentTool
	if slices.Contains(agent.AllowedTools, AgentToolName) {
		agentTool, err := c.agentTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}

	if slices.Contains(agent.AllowedTools, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}

	// Get the model name for the agent
	modelID := ""
	if modelCfg, ok := c.cfg.Config().Models[agent.Model]; ok {
		if model := c.cfg.Config().GetModel(modelCfg.Provider, modelCfg.Model); model != nil {
			modelID = model.ID
		}
	}

	logFile := filepath.Join(c.cfg.Config().Options.DataDirectory, "logs", "crush.log")

	// Build hook runner if PreToolUse hooks are configured.
	var hookRunner *hooks.Runner
	if preToolHooks := c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(preToolHooks) > 0 {
		hookRunner = hooks.NewRunner(preToolHooks, c.cfg.WorkingDir(), c.cfg.WorkingDir())
	}

	allTools = append(
		allTools,
		tools.NewBashTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Options.DataDirectory, c.cfg.Config().Options.Attribution, modelID),
		tools.NewCrushInfoTool(c.cfg, c.lspManager, c.allSkills, c.activeSkills, c.skillTracker),
		tools.NewCrushLogsTool(logFile),
		tools.NewJobOutputTool(c.cfg.Config().Options.DataDirectory),
		tools.NewJobKillTool(),
		tools.NewDownloadTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewMultiEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewFetchTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewGlobTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Glob),
		tools.NewGrepTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Grep),
		tools.NewLsTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Tools.Ls),
		tools.NewSourcegraphTool(nil),
		tools.NewTodosTool(c.sessions),
		tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, c.skillTracker, c.cfg.WorkingDir(), c.cfg.Config().Options.SkillsPaths...),
		tools.NewWriteTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
	)

	// Question tool is interactive-only and not available to sub-agents.
	if !isSubAgent && c.interactive {
		allTools = append(allTools, tools.NewQuestionTool(c.questions))
	}

	// Add LSP tools if user has configured LSPs or auto_lsp is enabled (nil or true).
	if len(c.cfg.Config().LSP) > 0 || c.cfg.Config().Options.AutoLSP == nil || *c.cfg.Config().Options.AutoLSP {
		allTools = append(
			allTools,
			tools.NewDiagnosticsTool(c.lspManager),
			tools.NewReferencesTool(c.lspManager),
			tools.NewLSPRestartTool(c.lspManager),
			tools.NewSymbolsTool(c.lspManager),
			tools.NewDefinitionTool(c.lspManager),
			tools.NewCallHierarchyTool(c.lspManager),
			tools.NewRenameTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
			tools.NewReplaceSymbolTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		)
	}

	if len(c.cfg.Config().MCP) > 0 {
		allTools = append(
			allTools,
			tools.NewListMCPResourcesTool(c.cfg, c.permissions),
			tools.NewReadMCPResourceTool(c.cfg, c.permissions),
		)
	}

	var filteredTools []fantasy.AgentTool
	for _, tool := range allTools {
		if slices.Contains(agent.AllowedTools, tool.Info().Name) {
			filteredTools = append(filteredTools, tool)
		}
	}

	for _, tool := range tools.GetMCPTools(c.permissions, c.cfg, c.cfg.WorkingDir()) {
		if agent.AllowedMCP == nil {
			// No MCP restrictions
			filteredTools = append(filteredTools, tool)
			continue
		}
		if len(agent.AllowedMCP) == 0 {
			// No MCPs allowed
			slog.Debug("No MCPs allowed", "tool", tool.Name(), "agent", agent.Name)
			break
		}

		for mcp, tools := range agent.AllowedMCP {
			if mcp != tool.MCP() {
				continue
			}
			if len(tools) == 0 || slices.Contains(tools, tool.MCPToolName()) {
				filteredTools = append(filteredTools, tool)
				break
			}
			slog.Debug("MCP not allowed", "tool", tool.Name(), "agent", agent.Name)
		}
	}
	slices.SortFunc(filteredTools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})

	// Wrap tools with hook interception for the top-level agent only.
	// Sub-agents (the `agent` task tool, `agentic_fetch`, etc.) run
	// without hook interception to avoid firing the user's hook N times
	// per delegated turn. The top-level invocation of the sub-agent tool
	// itself is still wrapped from the coder's side.
	// The secrets guard sits under the hooks and covers sub-agents too.
	filteredTools = wrapToolsWithHooks(guardTools(filteredTools), hookRunner, isSubAgent)

	return filteredTools, nil
}

// TODO: when we support multiple agents we need to change this so that we pass in the agent specific model config
func (c *coordinator) buildAgentModels(ctx context.Context, isSubAgent bool) (Model, Model, error) {
	largeModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	if !ok {
		return Model{}, Model{}, errLargeModelNotSelected
	}
	smallModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, Model{}, errSmallModelNotSelected
	}

	largeProviderCfg, ok := c.cfg.Config().Providers.Get(largeModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errLargeModelProviderNotConfigured
	}

	largeProvider, err := c.providerBuilder().Provider(largeProviderCfg, largeModelCfg, isSubAgent)
	if err != nil {
		return Model{}, Model{}, err
	}

	smallProviderCfg, ok := c.cfg.Config().Providers.Get(smallModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errSmallModelProviderNotConfigured
	}

	smallProvider, err := c.providerBuilder().Provider(smallProviderCfg, smallModelCfg, true)
	if err != nil {
		return Model{}, Model{}, err
	}

	// Resolve the selected model from the provider's whole catalog: an
	// API-key model, a ChatGPT subscription model, or a Grok one. The
	// subscription catalogs live outside Models, so looking only there
	// strands a Grok/ChatGPT model that catwalk doesn't also publish.
	largeCatwalkModel := c.cfg.Config().GetModel(largeModelCfg.Provider, largeModelCfg.Model)
	smallCatwalkModel := c.cfg.Config().GetModel(smallModelCfg.Provider, smallModelCfg.Model)

	if largeCatwalkModel == nil {
		return Model{}, Model{}, errLargeModelNotFound
	}

	if smallCatwalkModel == nil {
		return Model{}, Model{}, errSmallModelNotFound
	}

	largeModel, err := c.providerBuilder().LanguageModel(ctx, largeProvider, largeProviderCfg, largeModelCfg)
	if err != nil {
		return Model{}, Model{}, err
	}
	smallModel, err := c.providerBuilder().LanguageModel(ctx, smallProvider, smallProviderCfg, smallModelCfg)
	if err != nil {
		return Model{}, Model{}, err
	}

	large := Model{
		Model:      largeModel,
		CatwalkCfg: *largeCatwalkModel,
		ModelCfg:   largeModelCfg,
		FlatRate:   largeProviderCfg.FlatRate,
	}
	small := Model{
		Model:      smallModel,
		CatwalkCfg: *smallCatwalkModel,
		ModelCfg:   smallModelCfg,
		FlatRate:   smallProviderCfg.FlatRate,
	}

	return large, small, nil
}

// BeginAccepted reserves an accept slot for sessionID on the active
// agent and returns the ownership handle. It is the fire-and-forget
// dispatch path's only way to mark a run as accepted-but-not-yet-active
// so a cancel arriving before the run registers in activeRequests is not
// lost.
func (c *coordinator) BeginAccepted(sessionID string) *AcceptedRun {
	return c.currentAgent().BeginAccepted(sessionID)
}

func (c *coordinator) Cancel(sessionID string) {
	if c.tasks != nil {
		c.tasks.mu.Lock()
		if run := c.tasks.sessionRuns[sessionID]; run != nil {
			run.cancel()
		}
		c.tasks.mu.Unlock()
	}
	c.currentAgent().Cancel(sessionID)
}

func (c *coordinator) Interrupt(sessionID string) {
	c.currentAgent().Interrupt(sessionID)
}

func (c *coordinator) CancelAll() {
	if c.tasks != nil {
		c.tasks.stopAll()
	}
	c.currentAgent().CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent().ClearQueue(sessionID)
}

func (c *coordinator) RecallQueuedPrompt(sessionID string) *message.QueuedPrompt {
	return c.currentAgent().RecallQueuedPrompt(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent().IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent().IsSessionBusy(sessionID)
}

func (c *coordinator) Model() Model {
	return c.currentAgent().Model()
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// A ChatGPT or Grok login without its model catalog — the fetch at
	// login failed, or the credentials predate it — would leave the
	// models dialog's subscription section empty. Fill it in lazily; the
	// guards make this a no-op once a catalog exists.
	c.cfg.RefetchOpenAIChatGPTModels(ctx)
	c.cfg.RefetchGrokModels(ctx)

	agent, name := c.activeAgent()
	return c.updateAgentModels(ctx, agent, name)
}

// updateAgentModels rebuilds the model and tool configuration for the
// given agent from the current config.
func (c *coordinator) updateAgentModels(ctx context.Context, agent SessionAgent, name string) error {
	// build the models again so we make sure we get the latest config
	large, small, err := c.buildAgentModels(ctx, false)
	if err != nil {
		return err
	}
	agent.SetModels(large, small)

	agentCfg, ok := c.cfg.Config().Agents[name]
	if !ok {
		return fmt.Errorf("%w: %s", errMainAgentNotFound, name)
	}

	tools, err := c.buildTools(ctx, agentCfg, false)
	if err != nil {
		return err
	}
	agent.SetTools(tools)
	return nil
}

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent().QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent().QueuedPromptsList(sessionID)
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	agent := c.currentAgent()
	providerCfg, ok := c.cfg.Config().Providers.Get(agent.Model().ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}

	if err := c.refreshTokenIfExpired(ctx, providerCfg); err != nil {
		slog.Error("Failed to refresh OAuth2 token before summarize. Proceeding with existing token.", "error", err)
	}

	// Auth failures during summarize flow through fantasy's OnAuthRefresh,
	// the same path used by regular turns.
	return agent.Summarize(ctx, sessionID, getProviderOptions(agent.Model(), providerCfg), c.makeAuthRefreshCallback(providerCfg))
}

// GenerateTitle generates a session title using the current agent.
func (c *coordinator) GenerateTitle(ctx context.Context, sessionID, prompt string) {
	agent := c.currentAgent()
	if agent == nil {
		return
	}
	agent.GenerateTitle(ctx, sessionID, prompt)
}

// refreshTokenIfExpired proactively refreshes the OAuth token if it has expired.
func (c *coordinator) refreshTokenIfExpired(ctx context.Context, providerCfg config.ProviderConfig) error {
	if providerCfg.OAuthToken == nil || !providerCfg.OAuthToken.IsExpired() {
		return nil
	}
	slog.Debug("Token needs to be refreshed", "provider", providerCfg.ID)
	return c.refreshOAuth2Token(ctx, providerCfg)
}

// retryAfterUnauthorized attempts to refresh credentials after an auth error
// and returns nil if the request should be retried. For OAuth providers whose
// refresh token is revoked, and for Bedrock providers whose AWS SSO session
// has expired, it triggers interactive re-authentication and blocks until the
// user completes it (or the context is cancelled).
func (c *coordinator) retryAfterUnauthorized(ctx context.Context, providerCfg config.ProviderConfig) error {
	switch {
	case providerCfg.OAuthToken != nil:
		slog.Debug("Received 401. Refreshing token and retrying", "provider", providerCfg.ID)
		if err := c.refreshOAuth2Token(ctx, providerCfg); err != nil {
			// If the refresh token was revoked, trigger interactive
			// re-auth and wait for the user to complete it.
			var exchangeErr *oauth.TokenExchangeError
			if c.notify != nil && errors.As(err, &exchangeErr) && exchangeErr.IsRefreshTokenRevoked() {
				slog.Info("Refresh token revoked, waiting for re-authentication", "provider", providerCfg.ID)
				c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
					Type:       notify.TypeReAuthenticate,
					ProviderID: providerCfg.ID,
				})
				return c.waitForInteractiveReauth(ctx, providerCfg.ID)
			}
			return err
		}
		return nil
	case providerCfg.AWSAuthRefresh != "":
		return c.refreshAWSCredentials(ctx, providerCfg)
	case strings.Contains(providerCfg.APIKeyTemplate, "$"):
		slog.Debug("Received 401. Refreshing API Key template and retrying", "provider", providerCfg.ID)
		return c.refreshApiKeyTemplate(ctx, providerCfg)
	default:
		return nil
	}
}

// errNoInteractiveAuth is returned by an OnAuthRefresh callback when a
// provider needs interactive re-authentication but no notifier is available
// to drive it (e.g. headless runs). Returning it surfaces the original auth
// error rather than retrying.
var errNoInteractiveAuth = errors.New("interactive authentication unavailable")

// waitForInteractiveReauth blocks until interactive re-authentication for the
// provider completes (signalled via SignalAuthComplete) or the context is
// cancelled, then rebuilds models so the next attempt picks up fresh
// credentials. Returns nil when the caller should retry.
func (c *coordinator) waitForInteractiveReauth(ctx context.Context, providerID string) error {
	// Use a detached context with a generous timeout so the wait survives
	// agent run cancellation. The user needs time to complete browser-based
	// authentication.
	waitCtx, waitCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer waitCancel()
	slog.Info("Blocking on WaitForTokenChange", "provider", providerID)
	if waitErr := c.cfg.WaitForTokenChange(waitCtx, providerID); waitErr != nil {
		slog.Info("WaitForTokenChange returned error", "provider", providerID, "error", waitErr)
		return waitErr
	}
	// If the original context was cancelled during the wait, fantasy's retry
	// would fail immediately, so surface the cancellation instead.
	if ctx.Err() != nil {
		slog.Warn("Original context cancelled during auth wait, cannot retry",
			"provider", providerID, "ctx_err", ctx.Err())
		return ctx.Err()
	}
	// Rebuild models so ModelProvider picks up the fresh credentials.
	if updateErr := c.UpdateModels(waitCtx); updateErr != nil {
		slog.Error("Failed to update models after re-authentication", "error", updateErr)
		return updateErr
	}
	slog.Info("Models updated, returning nil to retry", "provider", providerID)
	return nil
}

// isUnauthorized reports whether err is an HTTP 401 from a provider.
func isUnauthorized(err error) bool {
	var providerErr *fantasy.ProviderError
	return errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized
}

// makeAuthRefreshCallback returns an OnAuthRefresh callback for fantasy that
// delegates to the coordinator's existing credential refresh logic. Returns
// nil if no refresh mechanism is configured for the provider.
func (c *coordinator) makeAuthRefreshCallback(providerCfg config.ProviderConfig) func(context.Context, *fantasy.ProviderError) error {
	if providerCfg.OAuthToken == nil &&
		!strings.Contains(providerCfg.APIKeyTemplate, "$") &&
		providerCfg.AWSAuthRefresh == "" {
		return nil
	}
	return func(ctx context.Context, _ *fantasy.ProviderError) error {
		return c.retryAfterUnauthorized(ctx, providerCfg)
	}
}

func (c *coordinator) refreshOAuth2Token(ctx context.Context, providerCfg config.ProviderConfig) error {
	if err := c.cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, providerCfg.ID); err != nil {
		slog.Error("Failed to refresh OAuth token after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}
	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

func (c *coordinator) refreshApiKeyTemplate(ctx context.Context, providerCfg config.ProviderConfig) error {
	newAPIKey, err := c.cfg.Resolve(providerCfg.APIKeyTemplate)
	if err != nil {
		slog.Error("Failed to re-resolve API key after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}

	providerCfg.APIKey = newAPIKey
	c.cfg.Config().Providers.Set(providerCfg.ID, providerCfg)

	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

// subAgentParams holds the parameters for running a sub-agent.
type subAgentParams struct {
	Agent          SessionAgent
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	// SessionSetup is an optional callback invoked after session creation
	// but before agent execution, for custom session configuration.
	SessionSetup func(sessionID string)
}

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, error) {
	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// Get model configuration
	model := params.Agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return fantasy.ToolResponse{}, errModelProviderNotConfigured
	}

	// Run the agent
	run := func() (*fantasy.AgentResult, error) {
		return params.Agent.Run(ctx, SessionAgentCall{
			SessionID:        session.ID,
			Prompt:           params.Prompt,
			MaxOutputTokens:  maxTokens,
			ProviderOptions:  getProviderOptions(model, providerCfg),
			Temperature:      model.ModelCfg.Temperature,
			TopP:             model.ModelCfg.TopP,
			TopK:             providers.CallTopK(providerCfg, model.ModelCfg.TopK),
			FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
			PresencePenalty:  model.ModelCfg.PresencePenalty,
			NonInteractive:   true,
			OnAuthRefresh:    c.makeAuthRefreshCallback(providerCfg),
		})
	}
	result, err := run()
	// Notify only if still unauthorized after retry. AWS SSO is handled
	// transparently inside OnAuthRefresh, so it needs no post-run notice.
	if err != nil && isUnauthorized(err) && c.notify != nil && model.ModelCfg.Provider == hyper.Name {
		c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			Type:       notify.TypeReAuthenticate,
			ProviderID: model.ModelCfg.Provider,
		})
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to generate response: %s", err)), nil
	}

	// Update parent session cost on a best-effort basis. A failure here must
	// not discard the sub-agent output that was already produced.
	if err := c.updateParentSessionCost(ctx, session.ID, params.SessionID); err != nil {
		slog.Warn(
			"Failed to update parent session cost",
			"child_session", session.ID,
			"parent_session", params.SessionID,
			"error", err,
		)
	}

	output := subAgentOutput(result)
	if output == "" {
		return fantasy.NewTextErrorResponse("Sub-agent completed but produced no text output."), nil
	}
	return fantasy.NewTextResponse(output), nil
}

func subAgentOutput(result *fantasy.AgentResult) string {
	if result == nil {
		return ""
	}
	return result.Response.Content.Text()
}

// updateParentSessionCost accumulates the cost from a child session to its parent session.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	parentSession, err := c.sessions.Get(ctx, parentSessionID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}

	parentSession.Cost += childSession.Cost

	if _, err := c.sessions.Save(ctx, parentSession); err != nil {
		return fmt.Errorf("save parent session: %w", err)
	}

	return nil
}

// discoverSkills is a thin fallback wrapper used only when no
// skills.Manager has been threaded through to the coordinator. All
// production call sites (backend.CreateWorkspace, setupLocalWorkspace)
// run discovery in advance and pass the results via the manager;
// reaching this path means a caller bypassed both. It deliberately does
// NOT publish to the package-level broker — there are no subscribers in
// that case, so doing so would be misleading without delivering the
// snapshot anywhere useful.
func discoverSkills(cfg *config.ConfigStore) (allSkills, activeSkills []*skills.Skill) {
	opts := cfg.Config().Options
	var paths, disabled []string
	if opts != nil {
		paths = opts.SkillsPaths
		disabled = opts.DisabledSkills
	}
	var resolver func(string) (string, error)
	if r := cfg.Resolver(); r != nil {
		resolver = r.ResolveValue
	}
	allSkills, activeSkills, states := skills.DiscoverFromConfig(skills.DiscoveryConfig{
		SkillsPaths:    paths,
		DisabledSkills: disabled,
		Resolver:       resolver,
	})
	logDiscoveryStats(states, paths, allSkills, activeSkills, disabled)
	return allSkills, activeSkills
}

// logTurnSkillUsage emits a per-turn diagnostic line showing which skills
// (if any) were loaded during this turn and which looked relevant based on
// a cheap keyword match against the user prompt. The goal is to surface
// "should-have-loaded but didn't" situations for later analysis.
//
// Logged at Info level under component=skills; heavy fields are elided when
// there is nothing interesting to report.
func logTurnSkillUsage(
	sessionID string,
	prompt string,
	activeSkills []*skills.Skill,
	tracker *skills.Tracker,
	before []string,
) {
	if tracker == nil || len(activeSkills) == 0 {
		return
	}

	after := tracker.LoadedNames()

	beforeSet := make(map[string]bool, len(before))
	for _, n := range before {
		beforeSet[n] = true
	}
	var loadedThisTurn []string
	for _, n := range after {
		if !beforeSet[n] {
			loadedThisTurn = append(loadedThisTurn, n)
		}
	}

	slog.Info(
		"Skill turn summary",
		"component", "skills",
		"session_id", sessionID,
		"prompt_len", len(prompt),
		"active_total", len(activeSkills),
		"loaded_total", len(after),
		"loaded_this_turn", loadedThisTurn,
	)
}

// logDiscoveryStats emits a single structured log line summarising skill
// discovery for the current session. It is intentionally low-volume: one
// line per session start. Builtin vs user counts are derived from the
// SkillState.Path — builtin states use the "builtin/" embed prefix.
func logDiscoveryStats(
	states []*skills.SkillState,
	userPaths []string,
	allSkills, activeSkills []*skills.Skill,
	disabled []string,
) {
	var builtinOK, builtinErr, userOK, userErr int
	for _, s := range states {
		isBuiltin := strings.HasPrefix(s.Path, "builtin/")
		switch {
		case isBuiltin && s.State == skills.StateNormal:
			builtinOK++
		case isBuiltin && s.State == skills.StateError:
			builtinErr++
		case !isBuiltin && s.State == skills.StateNormal:
			userOK++
		case !isBuiltin && s.State == skills.StateError:
			userErr++
		}
	}

	activeNames := make([]string, 0, len(activeSkills))
	for _, s := range activeSkills {
		activeNames = append(activeNames, s.Name)
	}

	xml := skills.ToPromptXML(activeSkills)

	slog.Info(
		"Skill discovery complete",
		"component", "skills",
		"builtin_ok", builtinOK,
		"builtin_errors", builtinErr,
		"user_ok", userOK,
		"user_errors", userErr,
		"user_paths", len(userPaths),
		"deduped_total", len(allSkills),
		"active", len(activeSkills),
		"disabled", len(disabled),
		"prompt_bytes", len(xml),
		"prompt_tok_est", skills.ApproxTokenCount(xml),
		"active_names", activeNames,
	)
}
