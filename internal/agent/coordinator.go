package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
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
	// MainAgentName returns the config name of the current main agent
	// ("coder" or "plan").
	MainAgentName() string
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
	// limitRetries continues chats whose usage limit resets.
	limitRetries limitRetries
	// fallback follows the fallback order between turns.
	fallback fallbackState

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
	c.watchCLIReplies()
	return c, nil
}

// watchCLIReplies handles what a kept agent CLI does between turns: a reply
// it starts on its own is shown in a turn, and the helpers it left running
// keep a headless run open. The returned func stops it.
func (c *coordinator) watchCLIReplies() (stop func()) {
	owns := func(sessionID string) bool {
		if c.sessions == nil {
			return false
		}
		_, err := c.sessions.Get(context.Background(), sessionID)
		return err == nil // otherwise another workspace's session
	}
	removeUnprompted := cliagent.OnUnprompted(func(sessionID string) bool {
		if !owns(sessionID) {
			return false
		}
		ctx := message.WithHiddenUserMessage(withCLIContinue(context.Background()))
		if _, err := c.Run(ctx, sessionID, "A background task finished."); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Showing an agent CLI's reply between turns", "session_id", sessionID, "error", err)
		}
		return true
	})
	removeHelpers := cliagent.OnHelpersRunning(func(sessionID string) (func(), bool) {
		if !owns(sessionID) || c.tasks == nil {
			return nil, false
		}
		_, release := c.tasks.reserveFollowUp(sessionID)
		return release, true
	})
	return func() {
		removeUnprompted()
		removeHelpers()
	}
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

// forgetChatModels drops every agent's built chat models.
func (c *coordinator) forgetChatModels() {
	c.agentMu.RLock()
	agents := make([]SessionAgent, 0, len(c.agents)+1)
	for _, a := range c.agents {
		agents = append(agents, a)
	}
	if c.mainAgent != nil {
		agents = append(agents, c.mainAgent)
	}
	c.agentMu.RUnlock()
	for _, a := range agents {
		a.ForgetChatModels()
	}
}

// currentAgent returns the current main agent.
func (c *coordinator) currentAgent() SessionAgent {
	agent, _ := c.activeAgent()
	return agent
}

// MainAgentName returns the config name of the current main agent.
func (c *coordinator) MainAgentName() string {
	_, name := c.activeAgent()
	return name
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
		c.tasks.cancelSessionRun(sessionID)
		// Stop in a sub-agent's own chat stops the sub-agent.
		if c.tasks.stopChild(sessionID) {
			return
		}
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
	// A sub-agent's chat is busy while the sub-agent runs.
	if c.tasks != nil && c.tasks.childRunning(sessionID) {
		return true
	}
	return c.currentAgent().IsSessionBusy(sessionID)
}

func (c *coordinator) Model() Model {
	return c.currentAgent().Model()
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
