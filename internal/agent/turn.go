package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// turn is the state of one call to sessionAgent.Run: how it was
// dispatched, what the model streams into, and what is published when it
// ends. The streaming callbacks are its methods; they run one at a time.
type turn struct {
	a    *sessionAgent
	call SessionAgentCall

	// ctx is the caller's context carrying the session ID. genCtx is the
	// run context the model streams under, canceled by cancel; ac is the
	// active-request entry that lets Cancel reach it.
	ctx    context.Context
	genCtx context.Context
	cancel context.CancelFunc
	ac     *activeCancel

	// largeModel is the model snapshot the whole turn runs on.
	largeModel   Model
	cliModel     *cliagent.Model
	isCLI        bool
	promptPrefix string
	fileReview   *turnFileReview

	// queuedAgain is set when the call went (back) into the queue, which
	// then owns its headless lifetime reservation.
	queuedAgain bool
	// skipRunComplete is set when a summarization continuation keeps the
	// terminal ownership until it finishes.
	skipRunComplete bool
	// notifyOnSuccess is set once the turn finished and may notify.
	notifyOnSuccess bool
	userMsgCreated  bool
	shouldSummarize bool

	// currentAssistant is the assistant message of the current step; the
	// final one is what the turn reports.
	currentAssistant *message.Message
	currentSession   session.Session
	// sessionLock guards currentSession's usage update and stepMessages.
	sessionLock  sync.Mutex
	stepMessages []fantasy.Message
	// consumedCalls are queued calls folded into this turn.
	consumedCalls []SessionAgentCall
	// folded are the queued prompts taken in at tool boundaries this turn.
	folded             []foldedPrompt
	sanitizedToolCalls map[string]bool
	// completedToolCalls are the full names of tool calls that completed
	// without error this turn. Written only from the streaming callbacks
	// (which run sequentially) and read after Stream returns, where
	// sendChannelReply uses it to tell whether the model already replied
	// on the originating channel.
	completedToolCalls map[string]struct{}
}

func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (result *fantasy.AgentResult, retErr error) {
	defer call.Accepted.Close()
	if call.sessionRun != nil {
		ctx = call.sessionRun.ctx
	}
	t := &turn{a: a, call: call}
	defer func() {
		if !t.queuedAgain {
			releaseQueuedCall(t.call, retErr)
			fillReports(t.call, result, retErr)
			for _, consumed := range t.consumedCalls {
				fillReports(consumed, result, retErr)
			}
		}
	}()
	if err := ValidateCall(t.call); err != nil {
		return nil, err
	}

	if t.call.Channel != "" && t.call.channelMeta == nil {
		t.call.channelMeta, _ = parseChannelMeta(t.call.Prompt)
	}

	switch t.dispatch(ctx) {
	case dispatchCanceled:
		return t.cancelOnEntry(ctx)
	case dispatchQueued:
		return nil, nil
	}

	defer t.cancel()
	// Conditional cleanup: only remove our entry if it hasn't been replaced
	// by a newer run. Without this guard, the deferred Del fires after a
	// concurrent run registers in the completion window, silently wiping
	// the new run's cancel and breaking cancellation.
	defer a.finishDispatch(ctx, t.call.SessionID, t.ac)

	// Add the session to the context. The run context (genCtx) and its
	// cancel func were already created and registered under the dispatch
	// mutex for both the accepted and in-process paths.
	t.ctx = context.WithValue(ctx, tools.SessionIDContextKey, t.call.SessionID)
	t.call.onQueuedInput = func(calls []SessionAgentCall) { t.consumedCalls = append(t.consumedCalls, calls...) }
	defer t.publishOutcome(&retErr)
	return t.run()
}

// dispatchOutcome is how dispatch settled a call.
type dispatchOutcome int

const (
	// dispatchActive: the call became the session's active run.
	dispatchActive dispatchOutcome = iota
	// dispatchQueued: the session was busy, so the call was queued.
	dispatchQueued
	// dispatchCanceled: a cancel covered the accepted call before it ran.
	dispatchCanceled
)

// dispatch decides, under the session's dispatch mutex, whether the call
// is canceled on entry, queued behind a busy session, or becomes the
// active run. Cancel takes the same per-session lock, so every cancel
// observes at least one of: a cancel mark, an activeRequests entry, or a
// messageQueue entry it then clears. Holding the lock across the busy
// check and the active registration also makes them atomic, so two
// concurrent in-process callers — a burst of channel events, or a channel
// event racing a typed prompt — cannot both pass the busy check and start
// two runs on the same session.
func (t *turn) dispatch(ctx context.Context) dispatchOutcome {
	a := t.a
	sessMu := a.sessionMu(t.call.SessionID)
	sessMu.Lock()

	if t.call.Accepted != nil && (t.call.Accepted.canceled.Load() || a.canceledBySeq(t.call.SessionID, t.call.Accepted.seq)) {
		// Cancel-on-entry: a cancel arrived while this accepted run was
		// dispatched but not yet active, and this handle's accept sequence
		// is at or below the session's cancel mark. The mark is left in
		// place so sibling handles it also covers observe the same cancel;
		// release the accept reservation and drop the lock.
		t.call.Accepted.Close()
		sessMu.Unlock()
		return dispatchCanceled
	}

	active, _ := a.activeRequests.Get(t.call.SessionID)
	ownReservation := t.call.batchReservation != nil && active == t.call.batchReservation
	if a.IsSessionBusy(t.call.SessionID) && !ownReservation {
		// Busy: an earlier prompt is active. Queue this call so it is
		// folded into (or sequenced after) the active turn, and release any
		// accept reservation. A Cancel arriving after this point sees the
		// active entry and clears the queue.
		//
		// enqueueCall strips per-turn OnComplete: the caller that supplied the hook
		// (typically coordinator.Run) has its own retry/coalesce scope that
		// ends when it returns, so by the time the queue drains nobody is
		// left to consume the buffered terminal event. The queued turn falls
		// back to the default broker publish, which is what existing
		// subscribers expect. A headless session's completion callback and
		// lifetime reservation survive until the queued turn really ends.
		if t.call.sessionRun != nil && t.call.queuedSessionRunRelease == nil && t.call.sessionRun.reserve() {
			var once sync.Once
			run := t.call.sessionRun
			t.call.queuedSessionRunRelease = func(err error) {
				once.Do(func() { run.release(err) })
			}
		}
		t.queuedAgain = true
		t.call.report.handOn()
		a.enqueueCall(t.call)
		if t.call.Accepted != nil {
			t.call.Accepted.Close()
		}
		sessMu.Unlock()
		return dispatchQueued
	}

	// Idle: become the active run. Register the cancel func before dropping
	// the lock so a Cancel that arrives between here and assistant creation
	// is not lost.
	runCtx := context.WithValue(ctx, tools.SessionIDContextKey, t.call.SessionID)
	if a.tasks != nil {
		env := a.tasks.env(t.call.SessionID)
		if a.isSubAgent {
			env = []string{TasksDirEnv + "=" + a.tasks.dir, TasksSessionEnv + "="}
		}
		runCtx = context.WithValue(runCtx, tools.ShellEnvContextKey, env)
	}
	t.genCtx, t.cancel = context.WithCancel(runCtx)
	t.ac = &activeCancel{cancel: t.cancel}
	a.activeRequests.Set(t.call.SessionID, t.ac)
	if h, ok := a.handoffs.Get(t.call.SessionID); ok && h == t.call.handoff {
		a.handoffs.Del(t.call.SessionID)
	}
	if t.call.Accepted != nil {
		t.call.Accepted.Close()
	}
	sessMu.Unlock()
	return dispatchActive
}

// cancelOnEntry persists a canceled turn for a call canceled before it
// became active.
//
// This path returns before the streaming defer that publishes RunComplete
// is installed, so it emits the terminal event explicitly. Without it, a
// caller waiting on RunComplete for this RunID (e.g. `crush run`, which
// ignores message events and blocks on RunComplete) would hang on an
// immediately-canceled accepted run.
func (t *turn) cancelOnEntry(ctx context.Context) (*fantasy.AgentResult, error) {
	a := t.a
	call := t.call
	if call.batchReservation != nil {
		defer a.finishDispatch(ctx, call.SessionID, call.batchReservation)
	}
	complete := notify.RunComplete{
		SessionID: call.SessionID,
		RunID:     call.RunID,
		Cancelled: true,
	}
	if err := a.persistCanceledTurn(ctx, call, call.userMessagesCreated); err != nil {
		complete.Error = err.Error()
		a.publishRunComplete(ctx, call, complete)
		return nil, err
	}
	a.publishRunComplete(ctx, call, complete)
	return nil, nil
}

// publishOutcome drains any debounced message updates and then publishes
// the authoritative RunComplete event for the turn. message.Service
// already flushes synchronously on terminal updates, but running this at
// every Run exit (success, error, panic recovery upstream) guarantees the
// contract without callers needing to know.
//
// The flush completes — meaning all per-message Publish(UpdatedEvent)
// calls have fired and been buffered into every subscriber's channel —
// before RunComplete is published. The flush-then-publish order gives
// well-behaved clients the best chance of seeing the final message event
// before RunComplete; the embedded Text field reconciles for clients that
// observe the events out of order (the pubsub broker fan-in does not
// serialize publishes from different upstream brokers).
func (t *turn) publishOutcome(retErr *error) {
	a := t.a
	// Use a context detached from the run context: workspace
	// shutdown cancels ctx before this goroutine returns, but the
	// buffered streaming deltas must still land before the DB is
	// closed. A short timeout bounds the flush.
	flushCtx, flushCancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
	defer flushCancel()
	flushErr := a.messages.FlushAll(flushCtx)
	if flushErr != nil {
		slog.Error("Failed to flush pending message updates after run", "error", flushErr)
	}
	if t.skipRunComplete {
		return
	}
	complete := notify.RunComplete{SessionID: t.call.SessionID, RunID: t.call.RunID}
	if t.currentAssistant != nil {
		complete.MessageID = t.currentAssistant.ID
		complete.Text = t.currentAssistant.Content().String()
	}
	if *retErr != nil {
		complete.Error = (*retErr).Error()
		complete.Cancelled = errors.Is(*retErr, context.Canceled)
	} else if t.genCtx.Err() != nil {
		complete.Cancelled = true
	}
	// Prefer the per-call hook when supplied so the coordinator
	// can coalesce retries (e.g. unauthorized → re-auth → retry)
	// into a single user-visible terminal event. The fallback
	// must-deliver publish applies bounded-blocking semantics to
	// the authoritative terminal event so a momentarily-full
	// subscriber channel can't silently drop it and hang
	// non-interactive clients waiting on RunComplete.
	a.publishRunComplete(t.ctx, t.call, complete)
	for _, original := range t.consumedCalls {
		outcome := complete
		outcome.RunID = original.RunID
		a.publishRunComplete(context.Background(), original, outcome)
		releaseQueuedCall(original, *retErr)
	}
	if t.notifyOnSuccess && flushErr == nil && complete.Error == "" && !complete.Cancelled {
		a.notifySessionFinished(t.call, t.currentSession, t.currentAssistant, t.ac)
	}
}

// run streams the active turn: dispatch → buildSystemPrompt → loadHistory
// → stream → finish.
func (t *turn) run() (*fantasy.AgentResult, error) {
	a := t.a
	agentTools, systemPrompt := t.buildSystemPrompt()
	workingDir := ""
	if a.cfg != nil {
		workingDir = a.cfg.WorkingDir()
	}
	if reason := KimiUnusable(t.largeModel.ModelCfg.Provider, workingDir); reason != "" {
		return nil, errors.New(reason)
	}
	if note := a.handOverTodos(t.ctx, t.call.SessionID, t.largeModel.ModelCfg.Provider, t.largeModel.ModelCfg.Model); note != "" {
		if t.isCLI {
			t.call.todoHandover = note
		} else {
			systemPrompt += "\n\n" + note
		}
	}
	agent := fantasy.NewAgent(
		t.largeModel.Model,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(agentTools...),
		fantasy.WithUserAgent(userAgent),
	)

	msgs, err := t.loadHistory()
	if err != nil {
		return nil, err
	}
	a.memoryNotice(t.ctx, t.call.SessionID)
	history, files := a.preparePrompt(msgs, t.largeModel.CatwalkCfg.SupportsImages, t.call.Attachments...)
	if !t.isCLI {
		overhead := approxTokenCount(systemPrompt) + approxTokenCount(t.promptPrefix) + approxTokenCount(t.call.Prompt) + int64(len(agentTools)*300)
		history = a.trimPreparedPrompt(t.call.SessionID, msgs, history, t.largeModel, overhead)
	}

	startTime := time.Now()
	a.eventPromptSent(t.call.SessionID)

	t.sanitizedToolCalls = make(map[string]bool)
	t.completedToolCalls = make(map[string]struct{})
	// Agent CLIs run their own tool loop; cliStream drives the same
	// callbacks from the CLI's events.
	stream := agent.Stream
	if t.isCLI {
		stream = a.cliStream(t.cliModel, t.call, msgs, t.largeModel.ModelCfg.ReasoningEffort, t.onCompacting, t.onActivity, func() string {
			if t.fileReview == nil {
				return ""
			}
			return t.fileReview.lastTool
		})
	}
	streamCall := t.buildStreamCall(history, files)
	t.fileReview = a.startFileReview(t.genCtx, t.call.SessionID)
	defer t.fileReview.finish(t.ctx)
	result, err := stream(t.genCtx, streamCall)

	a.eventPromptResponded(t.call.SessionID, time.Since(startTime).Truncate(time.Second))

	if err != nil {
		return t.handleStreamError(result, err)
	}
	return t.finish(result)
}

// buildSystemPrompt snapshots the model and the tools for the turn and
// builds its system prompt.
func (t *turn) buildSystemPrompt() ([]fantasy.AgentTool, string) {
	a := t.a
	// Copy mutable fields under lock to avoid races with SetTools/SetModels.
	agentTools := a.callTools(t.call)
	t.largeModel = a.largeModel.Get()
	t.cliModel, t.isCLI = t.largeModel.Model.(*cliagent.Model)
	systemPrompt := a.systemPrompt.Get()
	systemPrompt = withCurrentSkillPolicy(systemPrompt, currentSkillPolicy(a.cfg))
	if note := memoryStatus().AgentNote(); note != "" && !t.isCLI {
		// CLI agents get the note with their turn's text instead.
		systemPrompt += "\n\n" + note
	}
	t.promptPrefix = a.systemPromptPrefix.Get()
	if !t.isCLI && a.cfg != nil && a.cfg.Config().Options != nil && a.cfg.Config().Options.DisableInstructionFiles {
		systemPrompt += "\n\n" + sharedCLIInstructions + "\n\n" + memoryInstructions(a.isSubAgent)
		if a.tasks != nil && !a.isSubAgent {
			systemPrompt += "\n\n" + a.tasks.instructions()
		}
	}
	var instructions strings.Builder

	for _, server := range mcp.GetStates() {
		if server.State != mcp.StateConnected {
			continue
		}
		if s := server.Client.InitializeResult().Instructions; s != "" {
			instructions.WriteString(s)
			instructions.WriteString("\n\n")
		}
	}

	if s := instructions.String(); s != "" {
		systemPrompt += "\n\n<mcp-instructions>\n" + s + "\n</mcp-instructions>"
	}

	if len(agentTools) > 0 {
		// Add Anthropic caching to the last tool.
		agentTools[len(agentTools)-1].SetProviderOptions(a.getCacheControlOptions())
	}

	if !t.isCLI {
		if kept := apiKeptSkillsText(a.sessionKeptSkills(t.ctx, t.call.SessionID, false)); kept != "" {
			systemPrompt += "\n\n" + kept
		}
	}
	return agentTools, systemPrompt
}

// loadHistory loads the session and its messages, starts titling a new
// session, and persists the turn's user message.
func (t *turn) loadHistory() ([]message.Message, error) {
	a := t.a
	var err error
	t.currentSession, err = a.sessions.Get(t.ctx, t.call.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	msgs, err := a.getSessionMessages(t.ctx, t.currentSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get session messages: %w", err)
	}

	// Generate title from the first real (non-shell) user prompt.
	// can take tens of seconds. Blocking Run on it delays the
	// response to the caller. Use a detached context so the title
	// goroutine survives Run's cancel.
	// Sub-agent sessions are titled by whoever started them.
	if !hasUserTextMessage(msgs) && !a.isSubAgent {
		titleCtx := context.WithoutCancel(t.ctx)
		go a.GenerateTitle(titleCtx, t.call.SessionID, t.call.Prompt)
	}

	// Persist each original submission exactly once, including continuations.
	if !t.call.userMessagesCreated {
		if _, err := a.createUserMessage(t.ctx, t.call); err != nil {
			return nil, err
		}
	}
	t.userMsgCreated = true
	return msgs, nil
}

// buildStreamCall assembles the stream request: the prompt (or a batch of
// queued prompts), the call settings, and the turn's callbacks.
func (t *turn) buildStreamCall(history []fantasy.Message, files []fantasy.FilePart) fantasy.AgentStreamCall {
	a := t.a
	call := t.call
	// Don't send MaxOutputTokens if 0 — some providers (e.g. LM Studio) reject it
	var maxOutputTokens *int64
	if call.MaxOutputTokens > 0 {
		maxOutputTokens = &call.MaxOutputTokens
	}
	prompt := message.PromptWithTextAttachments(call.Prompt, call.Attachments)
	if len(call.batch) > 0 && !t.isCLI && !call.NotRecallable {
		for _, original := range call.batch {
			_, originalFiles := a.preparePrompt(nil, t.largeModel.CatwalkCfg.SupportsImages, original.Attachments...)
			parts := []fantasy.MessagePart{}
			if text := message.PromptWithTextAttachments(original.Prompt, original.Attachments); text != "" {
				parts = append(parts, fantasy.TextPart{Text: text})
			}
			parts = append(parts, filesAsParts(originalFiles)...)
			history = append(history, fantasy.Message{Role: fantasy.MessageRoleUser, Content: parts})
		}
		prompt, files = "", nil
	}
	if prompt == "" && len(files) > 0 && !t.isCLI {
		// Fantasy refuses files without a prompt, so an image-only
		// message goes in as its own user message.
		history = append(history, fantasy.Message{Role: fantasy.MessageRoleUser, Content: filesAsParts(files)})
		files = nil
	}
	var toolChoice *fantasy.ToolChoice
	if call.reportOnly {
		none := fantasy.ToolChoiceNone
		toolChoice = &none
	}
	return fantasy.AgentStreamCall{
		Prompt:           prompt,
		Files:            files,
		Messages:         history,
		Headers:          sessionHeaders(call.SessionID),
		ProviderOptions:  call.ProviderOptions,
		MaxOutputTokens:  maxOutputTokens,
		ToolChoice:       toolChoice,
		TopP:             call.TopP,
		Temperature:      call.Temperature,
		PresencePenalty:  call.PresencePenalty,
		TopK:             call.TopK,
		FrequencyPenalty: call.FrequencyPenalty,
		PrepareStep:      t.prepareStep,
		OnReasoningStart: t.onReasoningStart,
		OnReasoningDelta: t.onReasoningDelta,
		OnReasoningEnd:   t.onReasoningEnd,
		OnTextDelta:      t.onTextDelta,
		OnToolInputStart: t.onToolInputStart,
		OnRetry:          t.onRetry,
		OnAuthRefresh:    call.OnAuthRefresh,
		ModelProvider:    t.modelProvider,
		OnToolCall:       t.onToolCall,
		OnToolResult:     t.onToolResult,
		OnStepFinish:     t.onStepFinish,
		StopWhen: []fantasy.StopCondition{
			t.shouldAutoSummarize,
			func(steps []fantasy.StepResult) bool {
				return hasRepeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats)
			},
		},
	}
}
