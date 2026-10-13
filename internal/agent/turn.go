package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/x/exp/charmtone"
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
	history, files := a.preparePrompt(msgs, t.largeModel.CatwalkCfg.SupportsImages, t.call.Attachments...)

	startTime := time.Now()
	a.eventPromptSent(t.call.SessionID)

	t.sanitizedToolCalls = make(map[string]bool)
	t.completedToolCalls = make(map[string]struct{})
	// Agent CLIs run their own tool loop; cliStream drives the same
	// callbacks from the CLI's events.
	stream := agent.Stream
	if t.isCLI {
		stream = a.cliStream(t.cliModel, t.call, msgs, t.largeModel.ModelCfg.ReasoningEffort, t.onCompacting, t.onActivity)
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
	agentTools := filterToolsForChannel(a.tools.Copy(), t.call.Channel, mcp.GetStates())
	t.largeModel = a.largeModel.Get()
	t.cliModel, t.isCLI = t.largeModel.Model.(*cliagent.Model)
	systemPrompt := a.systemPrompt.Get()
	systemPrompt = withCurrentSkillPolicy(systemPrompt, currentSkillPolicy(a.cfg))
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
	return fantasy.AgentStreamCall{
		Prompt:           prompt,
		Files:            files,
		Messages:         history,
		Headers:          sessionHeaders(call.SessionID),
		ProviderOptions:  call.ProviderOptions,
		MaxOutputTokens:  maxOutputTokens,
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

// onCompacting marks the assistant message while an agent CLI compacts.
func (t *turn) onCompacting(active bool) error {
	if t.currentAssistant == nil {
		return nil
	}
	t.currentAssistant.IsCompacting = active
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

// onActivity records that an agent CLI is still working.
func (t *turn) onActivity() error {
	if t.currentAssistant == nil {
		return nil
	}
	t.currentAssistant.ActivityAt = time.Now().Unix()
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

// prepareStep builds each step's messages and creates the step's
// assistant message.
func (t *turn) prepareStep(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
	a := t.a
	call := t.call
	largeModel := t.largeModel
	prepared.Messages = options.Messages
	for i := range prepared.Messages {
		prepared.Messages[i].ProviderOptions = nil
	}

	// Use latest tools (updated by SetTools when MCP tools
	// change), filtered for the session's channel and minus MCP
	// servers disabled for this repository.
	prepared.Tools = a.filterDisabledMCPTools(
		callContext,
		filterToolsForChannel(a.tools.Copy(), call.Channel, mcp.GetStates()),
	)

	// Prompts queued during the turn join it at the next tool
	// boundary: every step after the first follows tool results.
	// Fantasy rebuilds each step's messages, so earlier folds are
	// put back where they went in.
	if !t.isCLI && options.StepNumber > 0 {
		queued, err := a.takeQueuedForStep(callContext, call, largeModel.CatwalkCfg.SupportsImages)
		if err != nil {
			return callContext, prepared, err
		}
		for _, msg := range queued {
			t.folded = append(t.folded, foldedPrompt{at: len(options.Messages), msg: msg})
		}
	}
	for i, f := range t.folded {
		prepared.Messages = slices.Insert(prepared.Messages, min(f.at+i, len(prepared.Messages)), f.msg)
	}

	prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, largeModel)

	lastSystemRoleInx := 0
	systemMessageUpdated := false
	for i, msg := range prepared.Messages {
		// Only add cache control to the last message.
		if msg.Role == fantasy.MessageRoleSystem {
			lastSystemRoleInx = i
		} else if !systemMessageUpdated {
			prepared.Messages[lastSystemRoleInx].ProviderOptions = a.getCacheControlOptions()
			systemMessageUpdated = true
		}
		// Than add cache control to the last 2 messages.
		if i > len(prepared.Messages)-3 {
			prepared.Messages[i].ProviderOptions = a.getCacheControlOptions()
		}
	}

	if t.promptPrefix != "" {
		prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(t.promptPrefix)}, prepared.Messages...)
	}

	t.sessionLock.Lock()
	t.stepMessages = cloneFantasyMessages(prepared.Messages)
	t.sessionLock.Unlock()

	var assistantMsg message.Message
	assistantMsg, err = a.messages.Create(callContext, call.SessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{},
		Model:    largeModel.ModelCfg.Model,
		Provider: largeModel.ModelCfg.Provider,
	})
	if err != nil {
		return callContext, prepared, err
	}
	callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
	callContext = context.WithValue(callContext, tools.ChannelContextKey, call.Channel)
	callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, largeModel.CatwalkCfg.SupportsImages)
	callContext = context.WithValue(callContext, tools.ModelNameContextKey, largeModel.CatwalkCfg.Name)
	t.currentAssistant = &assistantMsg
	return callContext, prepared, err
}

func (t *turn) onReasoningStart(id string, reasoning fantasy.ReasoningContent) error {
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.AppendReasoningContent(reasoning.Text)
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onReasoningDelta(id string, text string) error {
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.AppendReasoningContent(text)
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onReasoningEnd(id string, reasoning fantasy.ReasoningContent) error {
	// handle anthropic signature
	if anthropicData, ok := reasoning.ProviderMetadata[anthropic.Name]; ok {
		if reasoning, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok {
			t.currentAssistant.AppendReasoningSignature(reasoning.Signature)
		}
	}
	if googleData, ok := reasoning.ProviderMetadata[google.Name]; ok {
		if reasoning, ok := googleData.(*google.ReasoningMetadata); ok {
			t.currentAssistant.AppendThoughtSignature(reasoning.Signature, reasoning.ToolID)
		}
	}
	if openaiData, ok := reasoning.ProviderMetadata[openai.Name]; ok {
		if reasoning, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
			t.currentAssistant.SetReasoningResponsesData(reasoning)
		}
	}
	t.currentAssistant.FinishThinking()
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onTextDelta(id string, text string) error {
	t.currentAssistant.Activity = "responding"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	// Strip leading newline from initial text content. This is is
	// particularly important in non-interactive mode where leading
	// newlines are very visible.
	if len(t.currentAssistant.Parts) == 0 {
		text = strings.TrimPrefix(text, "\n")
	}

	t.currentAssistant.AppendContent(text)
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onToolInputStart(id string, toolName string) error {
	toolCall := message.ToolCall{
		ID:               id,
		Name:             toolName,
		ProviderExecuted: false,
		Finished:         false,
	}
	t.currentAssistant.AddToolCall(toolCall)
	// Use parent ctx instead of genCtx to ensure the update succeeds
	// even if the request is canceled mid-stream
	return t.a.messages.Update(t.ctx, *t.currentAssistant)
}

func (t *turn) onRetry(err *fantasy.ProviderError, delay time.Duration) {
	slog.Warn("Provider request failed, retrying", providerRetryLogFields(err, delay)...)
	// Reset streamed content so the retried response doesn't
	// concatenate with partial content from the failed attempt.
	// On the final attempt (no more retries), any partial content
	// stays in the message as useful context beneath the error.
	t.currentAssistant.ResetStreamedContent()
	if updateErr := t.a.messages.Update(t.genCtx, *t.currentAssistant); updateErr != nil {
		slog.Error("Failed to reset message on retry", "error", updateErr)
	}
}

func (t *turn) modelProvider() fantasy.LanguageModel {
	m := t.a.largeModel.Get()
	slog.Info("ModelProvider called",
		"provider", m.ModelCfg.Provider,
		"model", m.ModelCfg.Model)
	return m.Model
}

func (t *turn) onToolCall(tc fantasy.ToolCallContent) error {
	t.fileReview.track(tc.ToolCallID, tc.ToolName, tc.Input)
	input, wasSanitized := sanitizeToolInput(tc.ToolName, tc.ToolCallID, tc.Input)
	if wasSanitized {
		t.sanitizedToolCalls[tc.ToolCallID] = true
	}
	toolCall := message.ToolCall{
		ID:               tc.ToolCallID,
		Name:             tc.ToolName,
		Input:            input,
		ProviderExecuted: false,
		Finished:         true,
	}
	t.currentAssistant.AddToolCall(toolCall)
	// Use parent ctx instead of genCtx to ensure the update succeeds
	// even if the request is canceled mid-stream
	return t.a.messages.Update(t.ctx, *t.currentAssistant)
}

func (t *turn) onToolResult(result fantasy.ToolResultContent) error {
	toolResult := t.a.convertToToolResult(result)
	if t.sanitizedToolCalls[result.ToolCallID] {
		toolResult.Content = "Tool call failed: arguments were not valid JSON. Please check your tool call format and try again."
		toolResult.IsError = true
	}
	if !toolResult.IsError && toolResult.Name != "" {
		t.completedToolCalls[toolResult.Name] = struct{}{}
	}
	// Use parent ctx instead of genCtx to ensure the message is created
	// even if the request is canceled mid-stream
	if err := t.fileReview.save(t.ctx, toolResult); err != nil {
		return err
	}
	tools.PersistBackgroundReview(t.ctx, t.a.messages, t.call.SessionID, toolResult)
	return nil
}

func (t *turn) onStepFinish(stepResult fantasy.StepResult) error {
	a := t.a
	for _, w := range stepResult.Warnings {
		slog.Warn("Provider warning", "type", w.Type, "message", w.Message)
	}
	finishReason := message.FinishReasonUnknown
	switch stepResult.FinishReason {
	case fantasy.FinishReasonLength:
		finishReason = message.FinishReasonMaxTokens
	case fantasy.FinishReasonStop:
		finishReason = message.FinishReasonEndTurn
	case fantasy.FinishReasonToolCalls:
		finishReason = message.FinishReasonToolUse
	case fantasy.FinishReasonContentFilter:
		// Provider safety classifier stopped the model
		// (Anthropic stop_reason=refusal, OpenAI content_filter).
		// The TUI owns the display copy; we only persist the
		// reason so the UI can show a REFUSED banner.
		finishReason = message.FinishReasonContentFilter
		slog.Warn(
			"Provider content filter stopped the model",
			"session_id", t.call.SessionID,
			"finish_reason", string(stepResult.FinishReason),
		)
	}
	// If a tool result halted the turn (e.g. a hook halt or a
	// permission denial), the step ends on FinishReasonToolCalls but
	// the model will not be called again. Treat it as the end of the
	// turn so the UI can render the assistant footer.
	if finishReason == message.FinishReasonToolUse {
		for _, tr := range stepResult.Content.ToolResults() {
			if tr.StopTurn {
				finishReason = message.FinishReasonEndTurn
				break
			}
		}
	}
	t.currentAssistant.AddFinish(finishReason, "", "")
	t.currentAssistant.PrismModelID, t.currentAssistant.PrismModelName = extractPrismModel(stepResult.ProviderMetadata)
	t.currentAssistant.PrismHypercreditSavings, t.currentAssistant.PrismDollarSavings = extractPrismSavings(stepResult.ProviderMetadata)
	t.sessionLock.Lock()
	defer t.sessionLock.Unlock()

	updatedSession, getSessionErr := a.sessions.Get(t.ctx, t.call.SessionID)
	if getSessionErr != nil {
		return getSessionErr
	}
	usage, estimated := fallbackStepUsage(t.stepMessages, stepResult)
	a.updateSessionUsage(t.largeModel, &updatedSession, usage, a.openrouterCost(stepResult.ProviderMetadata), estimated)
	_, sessionErr := a.sessions.Save(t.ctx, updatedSession)
	if sessionErr != nil {
		return sessionErr
	}
	t.currentSession = updatedSession
	return a.messages.Update(t.genCtx, *t.currentAssistant)
}

// shouldAutoSummarize stops the turn once the session nears the model's
// context window, so it can be summarized before continuing.
func (t *turn) shouldAutoSummarize(_ []fantasy.StepResult) bool {
	a := t.a
	if t.isCLI && config.NativeCompaction(t.cliModel.Kind) {
		return false
	}
	cw := int64(t.largeModel.CatwalkCfg.ContextWindow)
	tokens := t.currentSession.CompletionTokens + t.currentSession.PromptTokens
	limit := config.DefaultAutoCompactTokenLimit
	if a.cfg != nil {
		limit = a.cfg.Config().Options.GetAutoCompactTokenLimit()
	}
	if tokens >= config.FallbackCompactionLimit(cw, limit) && !a.disableAutoSummarize {
		t.shouldSummarize = true
		return true
	}
	return false
}

// handleStreamError records a failed stream: it closes the dangling tool
// calls, finishes the assistant message with the classified error, and
// tells the originating channel.
func (t *turn) handleStreamError(result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
	a := t.a
	isHyper := t.largeModel.ModelCfg.Provider == hyper.Name
	isCancelErr := errors.Is(err, context.Canceled)
	slog.Info("Agent stream returned error",
		"error", err.Error(),
		"error_type", fmt.Sprintf("%T", err),
		"is_hyper", isHyper,
		"is_cancel", isCancelErr)
	if t.currentAssistant == nil {
		// Cancel-before-assistant-creation window: the run was
		// canceled after activeRequests.Set but before PrepareStep
		// created the assistant message. Without this, the turn
		// would return with no FinishReasonCanceled marker and no
		// user-visible record. The user message was already created
		// above, so persistCanceledTurn only writes the assistant
		// record.
		if isCancelErr {
			if persistErr := a.persistCanceledTurn(t.ctx, t.call, t.userMsgCreated); persistErr != nil {
				return nil, persistErr
			}
		}
		return result, err
	}
	// Persist final state with a context detached from the run
	// context. The run context (ctx) is derived from the
	// workspace context, which workspace shutdown cancels before
	// agent goroutines finish; using ctx here would drop the
	// final assistant state. WithoutCancel keeps the values
	// (e.g. session ID) while ignoring cancellation, and a short
	// timeout bounds the cleanup writes.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
	defer cleanupCancel()
	if err := t.closeDanglingToolCalls(cleanupCtx, isCancelErr); err != nil {
		return nil, err
	}
	reason, title, details := classifyRunError(err, isHyper, t.largeModel.CatwalkCfg.Name)
	t.currentAssistant.AddFinish(reason, title, details)
	// Note: we use the cleanup context here because the genCtx has been
	// cancelled.
	updateErr := a.messages.Update(cleanupCtx, *t.currentAssistant)
	if updateErr != nil {
		return nil, updateErr
	}
	// A channel-originated turn has no caller watching the error, so
	// tell the channel side something went wrong instead of leaving
	// the sender hanging. sendChannelReply detaches from the run
	// context so the notice survives a provider error that tore it down.
	if channelErrorReplyWanted(t.call.Channel, err) {
		a.sendChannelReply(t.ctx, t.call,
			"Something went wrong while handling your message. Please try again.",
			t.completedToolCalls)
	}
	return nil, err
}

// closeDanglingToolCalls finishes the assistant's unfinished tool calls and
// gives every call without a result an error result, so the history stays
// valid for the next request.
func (t *turn) closeDanglingToolCalls(cleanupCtx context.Context, isCancelErr bool) error {
	a := t.a
	// Ensure we finish thinking on error to close the reasoning state.
	t.currentAssistant.FinishThinking()
	toolCalls := t.currentAssistant.ToolCalls()
	// INFO: we use the cleanup context here because the genCtx has been cancelled.
	msgs, createErr := a.messages.List(cleanupCtx, t.currentAssistant.SessionID)
	if createErr != nil {
		return createErr
	}
	for _, tc := range toolCalls {
		if !tc.Finished {
			tc.Finished = true
			tc.Input = "{}"
			t.currentAssistant.AddToolCall(tc)
			updateErr := a.messages.Update(cleanupCtx, *t.currentAssistant)
			if updateErr != nil {
				return updateErr
			}
		}

		found := false
		for _, msg := range msgs {
			if msg.Role == message.Tool {
				for _, tr := range msg.ToolResults() {
					if tr.ToolCallID == tc.ID {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		if found {
			continue
		}
		content := "There was an error while executing the tool"
		if isCancelErr {
			content = "Error: user cancelled assistant tool calling"
		}
		toolResult := message.ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    content,
			IsError:    true,
		}
		createErr = t.fileReview.save(cleanupCtx, toolResult)
		if createErr != nil {
			return createErr
		}
	}
	return nil
}

// classifyRunError turns the error a turn's stream ended with into the
// finish reason, title and details its assistant message records.
// modelName names the model in errors about it.
func classifyRunError(err error, isHyper bool, modelName string) (message.FinishReason, string, string) {
	var fantasyErr *fantasy.Error
	var providerErr *fantasy.ProviderError
	var requestTimedOutErr *providers.RequestTimeoutError
	const defaultTitle = "Provider Error"
	switch {
	case errors.Is(err, context.Canceled):
		return message.FinishReasonCanceled, "User canceled request", ""
	case errors.As(err, &requestTimedOutErr):
		// Checked before the provider branches so a deadline our own
		// request timeout imposed is never reported as a provider error.
		return message.FinishReasonError, "Request timed out", requestTimedOutErr.UserMessage()
	case isHyper && errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized:
		return message.FinishReasonError, "Unauthorized", `Please re-authenticate with Hyper. You can also run "crush auth" to re-authenticate.`
	case errors.As(err, &providerErr):
		if providerErr.Message == "The requested model is not supported." {
			url := "https://github.com/settings/copilot/features"
			linkStyle := lipgloss.NewStyle().Foreground(charmtone.Guac).Underline(true)
			link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
			return message.FinishReasonError,
				"Copilot model not enabled",
				fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", modelName, link)
		}
		return message.FinishReasonError, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message
	case errors.As(err, &fantasyErr):
		return message.FinishReasonError, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message
	case fantasy.IsTransportError(err):
		wrapped := fantasy.NewTransportError(err)
		return message.FinishReasonError, stringext.Capitalize(wrapped.Title), wrapped.Message
	default:
		return message.FinishReasonError, defaultTitle, err.Error()
	}
}

// finish wraps up a turn whose stream succeeded: it summarizes the session
// when it grew too long, queueing a continuation when work was left, and
// replies on the originating channel.
func (t *turn) finish(result *fantasy.AgentResult) (*fantasy.AgentResult, error) {
	a := t.a
	// Finish before summarization or a recursive queued turn starts touching
	// the folder; its changes belong to that turn's own review.
	t.fileReview.finish(t.ctx)
	if t.shouldSummarize {
		a.activeRequests.Del(t.call.SessionID)
		if summarizeErr := a.Summarize(t.genCtx, t.call.SessionID, t.call.ProviderOptions, t.call.OnAuthRefresh); summarizeErr != nil {
			return nil, summarizeErr
		}
		// If the agent wasn't done...
		if len(t.currentAssistant.ToolCalls()) > 0 {
			t.queueContinuation()
		}
	}

	// Route the finished turn's response back to the channel it came
	// from, unless the turn was cut short for summarization with work
	// still pending — the queued continuation carries the channel and
	// replies when it actually finishes. Runs before the busy state is
	// released so a follow-up push queued behind this turn cannot
	// overtake its reply.
	if t.currentAssistant != nil && (!t.shouldSummarize || len(t.currentAssistant.ToolCalls()) == 0) {
		a.sendChannelReply(t.ctx, t.call, t.currentAssistant.Content().String(), t.completedToolCalls)
	}

	// Terminal publication precedes the deferred atomic batch dispatch.
	t.notifyOnSuccess = true
	return result, nil
}

// queueContinuation queues the rest of a turn that was summarized with
// work left, carrying the originals it took in so the continuation
// completes them.
func (t *turn) queueContinuation() {
	a := t.a
	queueMu := a.sessionMu(t.call.SessionID)
	queueMu.Lock()
	existing, ok := a.messageQueue.Get(t.call.SessionID)
	if !ok {
		existing = []SessionAgentCall{}
	}
	t.call.Prompt = fmt.Sprintf("The previous session was interrupted because it got too long, the initial user request was: `%s`", t.call.Prompt)
	t.call.NotRecallable = true
	t.call.userMessagesCreated = true
	t.call.onQueuedInput = nil
	if len(t.consumedCalls) > 0 {
		t.call.batch = slices.Concat(t.call.originalCalls(), t.consumedCalls)
		t.call.sessionRun = sharedBatchOwner(t.call.batch)
		t.call.queuedSessionRunRelease = nil
		t.call.OnComplete = nil
		t.consumedCalls = nil
	}
	t.skipRunComplete = true
	t.queuedAgain = true
	existing = append(existing, t.call)
	a.messageQueue.Set(t.call.SessionID, existing)
	queueMu.Unlock()
}
