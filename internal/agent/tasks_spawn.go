package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/google/uuid"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	agentprompt "github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/config"
)

// cliName is the short name agents use for a CLI provider type.
func cliName(t catwalk.Type) string {
	s := strings.TrimSuffix(string(t), "-cli")
	return strings.TrimSuffix(s, "-code")
}

// taskProviderName is the name agents pass to `crush spawn --cli`: an agent
// CLI's short name, or an API provider's ID.
func taskProviderName(provider config.ProviderConfig) string {
	if config.IsCLIProviderType(provider.Type) {
		return cliName(provider.Type)
	}
	return provider.ID
}

// taskProviders lists the configured providers a sub-agent can run on:
// every agent CLI and every API provider.
func (h *taskHub) taskProviders() []config.ProviderConfig {
	var out []config.ProviderConfig
	for _, p := range h.c.cfg.Config().Providers.Seq2() {
		if !p.Disable && len(p.Models) > 0 {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b config.ProviderConfig) int { return strings.Compare(taskProviderName(a), taskProviderName(b)) })
	return out
}

func (h *taskHub) spawn(req TaskRequest) (*Task, error) {
	ctx, release := h.reserveFollowUp(req.Session)
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("the task prompt is empty")
	}
	var provider *config.ProviderConfig
	var names []string
	for _, p := range h.taskProviders() {
		names = append(names, taskProviderName(p))
		if strings.EqualFold(req.CLI, taskProviderName(p)) || (config.IsCLIProviderType(p.Type) && strings.EqualFold(req.CLI, string(p.Type))) {
			provider = &p
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("unknown CLI %q; available: %s", req.CLI, strings.Join(names, ", "))
	}
	model := provider.Models[0]
	if req.Model != "" {
		i := slices.IndexFunc(provider.Models, func(m catwalk.Model) bool { return strings.EqualFold(m.ID, req.Model) })
		if i < 0 {
			return nil, fmt.Errorf("unknown %s model %q; available: %s", taskProviderName(*provider), req.Model, strings.Join(modelIDs(provider.Models), ", "))
		}
		model = provider.Models[i]
	}

	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return nil, fmt.Errorf("unknown session %q", req.Session)
	}
	selected, err := taskModel(*provider, model, req)
	if err != nil {
		return nil, err
	}
	selected.Account = h.taskAccount(req.Session, *provider, "")
	sub, m, err := h.subAgent(ctx, *provider, model, selected, req.ReadOnly)
	if err != nil {
		return nil, err
	}

	name := cmp.Or(strings.TrimSpace(req.Name), firstLine(prompt, 40))
	child, err := h.c.sessions.CreateTaskSession(ctx, uuid.NewString(), req.Session, name)
	if err != nil {
		return nil, err
	}
	// The worker's chat keeps the worker's own model, not its parent's.
	if choice, err := json.Marshal(selected); err == nil {
		if _, err := h.c.sessions.SetModel(ctx, child.ID, choice); err != nil {
			slog.Warn("Could not save the worker's model with its chat", "session_id", child.ID, "error", err)
		}
	}
	if !config.IsCLIProviderType(provider.Type) {
		h.c.permissions.AutoApproveSession(child.ID)
	}

	h.skipUsedIDs(req.Session)
	t := &Task{
		SessionID: req.Session, ChildID: child.ID, Name: name,
		CLI: taskProviderName(*provider), Model: model.ID, Effort: selected.ReasoningEffort, Fast: req.Fast, ReadOnly: req.ReadOnly, Status: TaskRunning, Started: time.Now(),
	}
	started = true
	snapshot, err := h.start(ctx, release, t, sub, m, *provider, subAgentPrompt(prompt, req.ReadOnly))
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// Same-provider workers follow the parent chat's current login, including
// its default login. Other providers retain their own account.
func (h *taskHub) taskAccount(parentSessionID string, provider config.ProviderConfig, previous string) string {
	cur, ok := h.c.chatSelection(context.Background(), parentSessionID)
	if !ok {
		return previous
	}
	parent, ok := h.c.cfg.Config().Providers.Get(cur.Provider)
	if ok && parent.Type == provider.Type {
		return cur.Account
	}
	return previous
}

// subAgent builds the agent a task runs on.
func (h *taskHub) subAgent(ctx context.Context, provider config.ProviderConfig, model catwalk.Model, selected config.SelectedModel, readOnly bool) (SessionAgent, Model, error) {
	if h.subAgentHook != nil {
		return h.subAgentHook(provider, model), Model{CatwalkCfg: model, ModelCfg: selected}, nil
	}
	fp, err := h.c.providerBuilder().Provider(provider, selected, true)
	if err != nil {
		return nil, Model{}, err
	}
	lm, err := h.c.providerBuilder().LanguageModel(ctx, fp, provider, selected)
	if err != nil {
		return nil, Model{}, err
	}
	// Sub-agents work on their own, like Claude Code's, but within what
	// Crush's YOLO mode allows.
	if cm, ok := lm.(*cliagent.Model); ok {
		cm.Guarded = true
		cm.ReadOnly = readOnly
		// Marks what it starts, so leftovers show up as background
		// processes. Without a session it can't spawn sub-agents itself.
		// A read-only one isn't told where Crush is: Crush runs its
		// background jobs outside the read-only sandbox.
		if !readOnly {
			cm.Env = []string{TasksDirEnv + "=" + h.dir}
		}
	}
	m := Model{Model: lm, CatwalkCfg: model, ModelCfg: selected, FlatRate: provider.FlatRate}
	var subTasks *taskHub
	if !config.IsCLIProviderType(provider.Type) {
		subTasks = h
	}
	sub := NewSessionAgent(SessionAgentOptions{
		LargeModel:           m,
		SmallModel:           m,
		SystemPromptPrefix:   provider.SystemPromptPrefix,
		IsSubAgent:           true,
		DisableAutoSummarize: h.c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               h.c.permissions.SkipRequests(),
		Sessions:             h.c.sessions,
		Messages:             h.c.messages,
		Cfg:                  h.c.cfg,
		Tasks:                subTasks,
		Notify:               h.c.notify,
		RunComplete:          h.c.runComplete,
	})
	if !config.IsCLIProviderType(provider.Type) {
		agentCfg := apiSubAgentConfig(h.c.cfg.Config().Agents, readOnly)
		nativeTools, err := h.c.buildTools(ctx, agentCfg, true)
		if err != nil {
			return nil, Model{}, err
		}
		sub.SetTools(nativeTools)
		buildPrompt := taskPrompt
		if !readOnly {
			buildPrompt = coderPrompt
		}
		p, err := buildPrompt(agentprompt.WithWorkingDir(h.c.cfg.WorkingDir()))
		if err != nil {
			return nil, Model{}, err
		}
		text, err := p.Build(ctx, lm.Provider(), model.ID, h.c.cfg)
		if err != nil {
			return nil, Model{}, err
		}
		sub.SetSystemPrompt(text)
	}
	return sub, m, nil
}

// apiSubAgentConfig picks the tools of a sub-agent on an API provider. A
// read-only one gets the task agent's look-only tools; any other one does
// real work, so it gets the coder's tools like an agent CLI would, minus
// nested helpers and MCP servers.
func apiSubAgentConfig(agents map[string]config.Agent, readOnly bool) config.Agent {
	if readOnly {
		return agents[config.AgentTask]
	}
	cfg := agents[config.AgentCoder]
	cfg.AllowedTools = slices.DeleteFunc(slices.Clone(cfg.AllowedTools), func(name string) bool {
		return name == "agent" || name == "agentic_fetch"
	})
	cfg.AllowedMCP = map[string][]string{}
	return cfg
}

// errHubClosing refuses to start a task on a hub whose Crush, or whose
// project's workspace, is shutting down.
var errHubClosing = errors.New("Crush is closing")

// start runs a task in the background and returns its first snapshot. A
// task without an ID gets the next one. release is called once the task's
// result is delivered. On a closing hub it starts nothing: it calls release
// and returns errHubClosing.
func (h *taskHub) start(ctx context.Context, release func(), t *Task, sub SessionAgent, m Model, provider config.ProviderConfig, prompt string) (Task, error) {
	return h.startRun(ctx, release, t, sub, m, provider, prompt, false)
}

// startRun is start; resumed marks a task ResumeInterruptedTasks restarts.
func (h *taskHub) startRun(ctx context.Context, release func(), t *Task, sub SessionAgent, m Model, provider config.ProviderConfig, prompt string, resumed bool) (Task, error) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		release()
		return Task{}, errHubClosing
	}
	if t.ID == "" {
		h.seq++
		t.ID = fmt.Sprintf("t%d", h.seq)
	}
	t.Account = m.ModelCfg.Account
	runCtx, cancel := context.WithCancel(ctx)
	h.cancels[t.ID] = cancel
	h.tasks[t.ID] = t
	if h.runs == nil {
		h.runs = map[string]*taskRun{}
	}
	run := &taskRun{ctx: runCtx, sub: sub, model: m, provider: provider}
	h.runs[t.ID] = run
	chat := t.chat
	t.chat = nil
	delete(h.interrupting, t.ID)
	if run := h.sessionRuns[t.SessionID]; run != nil {
		h.sessionRuns[t.ChildID] = run
	}
	h.mu.Unlock()
	// Registered, and no event about it yet: the owner comes with the first.
	owner := workStarted(WorkStart{Kind: WorkHelper, ID: t.ID, SessionID: t.SessionID, Resumed: resumed})
	h.mu.Lock()
	t.StartedBy = owner
	snapshot := *t
	h.mu.Unlock()
	if h.saves(ctx) {
		h.remember(snapshot, provider.ID)
	}
	h.publish(snapshot)

	go func() {
		defer release()
		defer cancel()
		removeCheckIn := h.addCheckIn(snapshot.SessionID, checkInWork{id: snapshot.ID, name: snapshot.Name, started: snapshot.Started, ctx: runCtx, progress: func() string {
			return h.subAgentProgress(runCtx, snapshot)
		}})
		first := SessionAgentCall{
			SessionID:       t.ChildID,
			Prompt:          prompt,
			MaxOutputTokens: subAgentMaxTokens(m),
			NonInteractive:  true,
			ProviderOptions: getProviderOptions(m, provider),
		}
		if chat != nil {
			first.SubmissionID, first.RunID = chat.submissionID, chat.runID
		}
		result, err := runSubAgentReport(runCtx, sub, first)
		// Out of usage: the order of the model it started on moves it,
		// and it continues there.
		start, cur := m.ModelCfg, m.ModelCfg
		for range subAgentFallbackMoves {
			next, ok := h.subAgentFallback(runCtx, t, start, cur, err)
			if !ok {
				break
			}
			cur = next.model.ModelCfg
			h.mu.Lock()
			run.sub, run.model, run.provider = next.sub, next.model, next.provider
			h.mu.Unlock()
			result, err = runSubAgentReport(runCtx, next.sub, SessionAgentCall{
				SessionID:       t.ChildID,
				Prompt:          limitRetryPrompt,
				MaxOutputTokens: subAgentMaxTokens(next.model),
				NonInteractive:  true,
				ProviderOptions: getProviderOptions(next.model, next.provider),
			})
		}
		result, err = h.awaitChatMessages(runCtx, t.ID, result, err)
		removeCheckIn()
		h.finishRun(ctx, t, subAgentOutput(result), err)
	}()
	return snapshot, nil
}

// taskModel is the model a sub-agent runs on, tuned as its request asks:
// the effort must be one of the model's levels, and only Claude and Codex
// have a fast mode.
func taskModel(provider config.ProviderConfig, model catwalk.Model, req TaskRequest) (config.SelectedModel, error) {
	selected := config.SelectedModel{Provider: provider.ID, Model: model.ID, ReasoningEffort: model.DefaultReasoningEffort}
	if effort := strings.ToLower(strings.TrimSpace(req.Effort)); effort != "" {
		if !slices.Contains(model.ReasoningLevels, effort) {
			if len(model.ReasoningLevels) == 0 {
				return selected, fmt.Errorf("%s model %q has no effort levels", taskProviderName(provider), model.ID)
			}
			return selected, fmt.Errorf("%s model %q takes effort %s, not %q", taskProviderName(provider), model.ID, strings.Join(model.ReasoningLevels, ", "), effort)
		}
		selected.ReasoningEffort = effort
	}
	if req.Fast {
		if provider.Type != config.TypeClaudeCode && !config.SupportsFastMode(provider, model) {
			return selected, fmt.Errorf("fast mode is only for claude and codex, not %s", taskProviderName(provider))
		}
		selected.ServiceTier = "fast"
	}
	return selected, nil
}

// subAgentPrompt is a new sub-agent's first prompt.
func subAgentPrompt(prompt string, readOnly bool) string {
	if readOnly {
		return subAgentPreamble + readOnlyPreamble + prompt
	}
	return subAgentPreamble + prompt
}

// readOnlyPreamble tells a read-only sub-agent where it stands, so it
// doesn't spend its turn fighting the sandbox.
const readOnlyPreamble = `You are read-only: you can read anything and run commands, but nothing can change this project's files (writes fail with a read-only file system error). Don't try to work around that. Where a change is needed, describe it in your report instead.

`

const subAgentPreamble = `You are a sub-agent that another AI agent started in the background from Crush. Do the task below on your own; nobody can answer questions while you work. Never ask the user anything (no "crush ask", no question tools): settle small details yourself. If the task says to check with the user first, or an open question would change the work substantially, don't guess: stop before that part and end with the questions (and the options you see) in your final report, so the agent that started you can answer them or ask the user. You end as soon as you stop replying, so finish what you start instead of leaving it running in the background. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Crush refuses a "sleep" longer than 10 seconds in the foreground; to wait for something, loop on a check (until <check>; do sleep 2; done). When you're done, end with a concise report of what you did and found: that final message is all the other agent will see.

`

func modelIDs(models []catwalk.Model) []string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}
