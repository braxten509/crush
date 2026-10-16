// Package agent is the core orchestration layer for Crush AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/vercel"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/version"
)

const (
	DefaultSessionName = "Untitled Session"
)

var userAgent = fmt.Sprintf("Charm-Crush/%s (https://charm.land/crush)", version.Version)

type SessionAgentCall struct {
	// batch retains the original submissions of an atomic queue drain. The
	// outer call supplies turn settings; identities and attachments stay here.
	batch               []SessionAgentCall
	userMessagesCreated bool
	// batchReservation keeps the drain-to-Run handoff busy, so a newer
	// submission cannot overtake the batch before its Run is scheduled.
	batchReservation *activeCancel
	// onQueuedInput transfers confirmed native steering into this turn's
	// completion ownership. Unconfirmed input remains owned by the queue.
	onQueuedInput func([]SessionAgentCall)
	// todoHandover is the last model's to-do list, handed to an agent CLI
	// with the prompt after a model switch cleared it.
	todoHandover string

	SessionID     string
	SubmissionID  string
	NotRecallable bool
	// RunID, when non-empty, is the caller-supplied correlator that
	// gets echoed back on the notify.RunComplete event emitted for
	// this turn. It is preserved when the call is enqueued behind a
	// busy session so the queued turn's terminal event is still
	// recognisable to the original caller. Callers that need a
	// reliable completion contract (e.g. `crush run` against a
	// session that may be busy) MUST set it; SessionID alone is
	// ambiguous when concurrent turns share the same session.
	RunID             string
	Channel           string
	HiddenUserMessage bool
	// CLIContinue shows a reply the agent CLI started on its own between
	// turns instead of sending Prompt (cliagent.Turn.Continue).
	CLIContinue      bool
	Prompt           string
	ProviderOptions  fantasy.ProviderOptions
	Attachments      []message.Attachment
	MaxOutputTokens  int64
	Temperature      *float64
	TopP             *float64
	TopK             *int64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	NonInteractive   bool
	// reportOnly disables tool use during a bounded sub-agent report recovery.
	reportOnly bool
	// report, when set, receives the result of the turn that finally ends
	// this call: a later continuation when the session was summarized with
	// work left, or the queued turn when the session was busy.
	report *reportBox
	// sessionRun follows a headless prompt through the queue, even when
	// the active turn that drains it started outside that session owner.
	sessionRun              *sessionRun
	queuedSessionRunRelease func(error)
	// OnComplete, when non-nil, replaces the default RunComplete
	// publish path: the inner Run hands the terminal payload to this
	// callback instead of emitting it on the RunComplete broker. The
	// coordinator uses this hook to coalesce the unauthorized →
	// re-auth → retry chain into a single user-visible terminal
	// event, so non-interactive clients (e.g. `crush run`) don't
	// exit on a stale failed-attempt RunComplete before the
	// successful retry. It is intentionally stripped when queueing
	// a busy-session call (see Run): the originating
	// coordinator.Run has long returned by the time the queued
	// recursion drains, so falling back to the default broker
	// publish keeps the event visible to subscribers. A sessionRun's
	// callback is retained because its owner waits through queue dispatch.
	OnComplete func(notify.RunComplete)
	// Accepted, when non-nil, is the accept reservation taken by
	// BeginAccepted before the call was dispatched onto a goroutine
	// (the client/server fire-and-forget path). Run consumes it under
	// dispatchMu[SessionID] once the accepted -> (cancel-on-entry |
	// queued | active) transition has been chosen. When nil
	// (in-process / local callers like AppWorkspace), behavior is
	// unchanged and no accept tracking applies.
	Accepted *AcceptedRun
	// acceptSeq carries the accept sequence of the handle that produced
	// this call after it has been enqueued and its Accepted handle
	// stripped. The queue-drain paths compare it against a session's
	// cancel mark so a follow-up queued before a cancel is dropped while
	// one queued after the cancel survives. 0 means untracked (an
	// in-process enqueue with no accept reservation), which the drain
	// paths treat as covered by any present mark, preserving the
	// pre-sequence behavior.
	acceptSeq uint64
	// queueOrder retains acceptance order when dispatch goroutines arrive out of order.
	queueOrder uint64
	handoff    *queueHandoff
	// OnAuthRefresh, when non-nil, is called by fantasy when a stream
	// fails with an authentication error (HTTP 401). The callback should
	// refresh credentials and return nil on success, in which case
	// fantasy retries the stream transparently. Returning an error
	// surfaces the original auth error without retry.
	OnAuthRefresh func(ctx context.Context, err *fantasy.ProviderError) error
	// channelMeta holds the attributes of the <channel> element a
	// channel-originated turn was started with, parsed once at Run
	// entry. It survives the auto-summarize continuation, whose Prompt
	// is rewritten and no longer carries the element, so the reply
	// target is not lost when a long channel turn is summarized.
	channelMeta map[string]string
}

type SessionAgent interface {
	Run(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)
	BeginAccepted(sessionID string) *AcceptedRun
	SetModels(large Model, small Model)
	SetTools(tools []fantasy.AgentTool)
	SetSystemPrompt(systemPrompt string)
	Cancel(sessionID string)
	Interrupt(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	RecallQueuedPrompt(sessionID string) *message.QueuedPrompt
	Summarize(context.Context, string, fantasy.ProviderOptions, func(context.Context, *fantasy.ProviderError) error) error
	Model() Model
	GenerateTitle(ctx context.Context, sessionID, userPrompt string)
}

type Model struct {
	Model      fantasy.LanguageModel
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
	FlatRate   bool
}

// activeCancel wraps a context.CancelFunc with a unique pointer identity.
// The pointer is used for compare-and-delete in the dispatch completion path:
// when a finishing run's deferred cleanup fires, it must only remove its own
// entry — not a newer run's entry that was installed in the window between
// the explicit Del and the function return.
type activeCancel struct {
	cancel context.CancelFunc
}

type sessionAgent struct {
	largeModel         *csync.Value[Model]
	smallModel         *csync.Value[Model]
	systemPromptPrefix *csync.Value[string]
	systemPrompt       *csync.Value[string]
	tools              *csync.Slice[fantasy.AgentTool]

	isSubAgent bool
	tasks      *taskHub
	sessions   session.Service
	messages   message.Service
	// cfg backs channel reply routing (config lookup + MCP tool
	// invocation). Nil in tests and sub-agents that never see channel
	// turns; sendChannelReply treats nil as "routing disabled".
	cfg                  *config.ConfigStore
	disableAutoSummarize bool
	isYolo               bool
	notify               pubsub.Publisher[notify.Notification]
	runComplete          pubsub.Publisher[notify.RunComplete]

	// sessionScheduler queues and dispatches the session's runs.
	sessionScheduler
}

type SessionAgentOptions struct {
	LargeModel         Model
	SmallModel         Model
	SystemPromptPrefix string
	SystemPrompt       string
	IsSubAgent         bool
	// Tasks lets the session's agent CLI spawn background sub-agents.
	Tasks                *taskHub
	DisableAutoSummarize bool
	IsYolo               bool
	Sessions             session.Service
	Messages             message.Service
	Cfg                  *config.ConfigStore
	Tools                []fantasy.AgentTool
	Notify               pubsub.Publisher[notify.Notification]
	RunComplete          pubsub.Publisher[notify.RunComplete]
}

func NewSessionAgent(
	opts SessionAgentOptions,
) SessionAgent {
	a := &sessionAgent{
		largeModel:           csync.NewValue(opts.LargeModel),
		smallModel:           csync.NewValue(opts.SmallModel),
		systemPromptPrefix:   csync.NewValue(opts.SystemPromptPrefix),
		systemPrompt:         csync.NewValue(opts.SystemPrompt),
		isSubAgent:           opts.IsSubAgent,
		tasks:                opts.Tasks,
		sessions:             opts.Sessions,
		messages:             opts.Messages,
		cfg:                  opts.Cfg,
		disableAutoSummarize: opts.DisableAutoSummarize,
		tools:                csync.NewSliceFrom(opts.Tools),
		isYolo:               opts.IsYolo,
		notify:               opts.Notify,
		runComplete:          opts.RunComplete,
	}
	a.sessionScheduler = newSessionScheduler(a)
	return a
}

// AcceptedRun owns exactly one accept reservation taken by
// BeginAccepted. It is the only carrier of accept-state across the
// backend.runAgent / Coordinator.Run / sessionAgent.Run layers: a
// counter > 0 means a dispatched prompt is in flight and has not yet
// completed the dispatch handoff in Run. Close is the only way to
// release the reservation and is idempotent.
type AcceptedRun struct {
	sched     *sessionScheduler
	sessionID string
	// seq is the monotonic accept sequence stamped by BeginAccepted. A
	// cancel covers this handle iff seq is at or below the session's
	// cancel mark, so a handle accepted after a cancel (higher seq) is
	// never poisoned by it.
	seq  uint64
	done atomic.Bool
	// followUp is fixed at acceptance, before backend scheduling/model setup.
	followUp bool
	canceled atomic.Bool
	// handoff is assigned under dispatchMu and acceptedMu; pending is released by Close.
	handoff *queueHandoff
}

// Close decrements the accept counter for this reservation. It is safe
// to call multiple times; only the first call has effect.
func (r *AcceptedRun) Close() {
	if r == nil {
		return
	}
	if !r.done.CompareAndSwap(false, true) {
		return
	}
	r.sched.endAccepted(r)
}

// SessionID exposes the session this reservation is for so the run path
// can use it without an extra parameter.
func (r *AcceptedRun) SessionID() string {
	if r == nil {
		return ""
	}
	return r.sessionID
}

// Use the same configured/discovered routing as sendChannelReply. When no
// route is known, equal metadata is the conservative scope boundary.
func sameQueuedChannelRoute(reply *config.MCPChannelReply, first, next map[string]string) bool {
	if reply != nil {
		firstTool, firstArgs, firstOK := resolveChannelReply(reply, first, "")
		nextTool, nextArgs, nextOK := resolveChannelReply(reply, next, "")
		if firstOK || nextOK {
			return firstOK && nextOK && firstTool == nextTool && maps.Equal(firstArgs, nextArgs)
		}
	}
	return maps.Equal(first, next)
}

// sharedBatchOwner preserves cancellation for a batch belonging to one
// headless session. Mixed owners keep their individual completion leases.
func sharedBatchOwner(calls []SessionAgentCall) *sessionRun {
	owner := calls[0].sessionRun
	for _, call := range calls[1:] {
		if call.sessionRun != owner {
			return nil
		}
	}
	return owner
}

func (call SessionAgentCall) originalCalls() []SessionAgentCall {
	if len(call.batch) > 0 {
		return call.batch
	}
	return []SessionAgentCall{call}
}

// releaseQueuedCall releases every original headless lifetime reservation,
// including originals carried through a summary continuation.
func releaseQueuedCall(call SessionAgentCall, err error) {
	if len(call.batch) > 0 {
		for _, original := range call.batch {
			releaseQueuedCall(original, err)
		}
		return
	}
	if call.queuedSessionRunRelease != nil {
		call.queuedSessionRunRelease(err)
	}
}

// fillReports gives every original call's report box the result of the turn
// that ended it.
func fillReports(call SessionAgentCall, result *fantasy.AgentResult, err error) {
	for _, original := range call.batch {
		fillReports(original, result, err)
	}
	call.report.fill(result, err)
}

// persistCanceledTurn writes the user/assistant records for a turn that
// was canceled before (or just as) streaming would have produced them.
// It creates the user message only when it was not already created by an
// earlier createUserMessage call (userMsgCreated), then writes an
// assistant message with FinishReasonCanceled. Both writes use
// context.WithoutCancel(ctx) so workspace shutdown (which cancels the run
// context) can't drop them.
func (a *sessionAgent) persistCanceledTurn(ctx context.Context, call SessionAgentCall, userMsgCreated bool) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if !userMsgCreated {
		if _, err := a.createUserMessage(writeCtx, call); err != nil {
			return err
		}
	}
	largeModel := a.largeModel.Get()
	assistant, err := a.messages.Create(writeCtx, call.SessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{},
		Model:    largeModel.ModelCfg.Model,
		Provider: largeModel.ModelCfg.Provider,
	})
	if err != nil {
		return err
	}
	assistant.AddFinish(message.FinishReasonCanceled, "User canceled request", "")
	return a.messages.Update(writeCtx, assistant)
}

// publishRunComplete emits the authoritative terminal event for a turn.
// It honors the per-call OnComplete hook when set (so the coordinator can
// coalesce retries) and otherwise falls back to the RunComplete broker.
// ctx is used only for the bounded-blocking must-deliver publish; the
// terminal payload is supplied by the caller. This is the single emit path
// shared by the streaming defer and the cancel-on-entry early return so a
// caller waiting on RunComplete (e.g. `crush run` with a RunID) always
// observes exactly one terminal event regardless of which Run branch ends
// the turn.
func (a *sessionAgent) publishRunComplete(ctx context.Context, call SessionAgentCall, complete notify.RunComplete) {
	if len(call.batch) > 0 {
		for _, original := range call.batch {
			outcome := complete
			outcome.RunID = original.RunID
			a.publishRunComplete(context.Background(), original, outcome)
		}
		return
	}
	complete.SubmissionID = call.SubmissionID
	if call.OnComplete != nil {
		call.OnComplete(complete)
		return
	}
	if run := sessionRunFromContext(ctx, call.SessionID); run != nil {
		run.record(complete)
		return
	}
	if a.runComplete == nil {
		return
	}
	a.runComplete.PublishMustDeliver(ctx, pubsub.UpdatedEvent, complete)
}

// notifySessionFinished runs after the final message flush. Only a normal,
// nonempty main-session answer can finish the user's work. Child sessions
// and turns with running helpers or background commands never generate a
// completion alert. Queued, accepted, interrupting, and active turns also
// keep the session unfinished.
func (a *sessionAgent) notifySessionFinished(call SessionAgentCall, sess session.Session, assistant *message.Message, finished ...*activeCancel) {
	if a.notify == nil || a.isSubAgent || sess.ParentSessionID != "" || call.NonInteractive {
		return
	}
	if a.tasks.hasRunning(call.SessionID) || cliBackgroundRunning(call.SessionID) {
		return
	}
	if assistant == nil || assistant.Role != message.Assistant || assistant.IsSummaryMessage || assistant.IsCompacting || assistant.FinishReason() != message.FinishReasonEndTurn {
		return
	}
	text := assistant.Content()
	if text.Hidden || (strings.TrimSpace(text.String()) == "" && len(assistant.ImageURLContent()) == 0 && len(assistant.BinaryContent()) == 0) {
		return
	}
	mu := a.sessionMu(call.SessionID)
	mu.Lock()
	defer mu.Unlock()
	a.acceptedMu.Lock()
	defer a.acceptedMu.Unlock()
	accepted, _ := a.acceptedRuns.Get(call.SessionID)
	queued, _ := a.messageQueue.Get(call.SessionID)
	active, _ := a.activeRequests.Get(call.SessionID)
	if len(finished) > 0 && active == finished[0] {
		active = nil // this turn has flushed and published its terminal event
	}
	_, interrupting := a.interrupting.Get(call.SessionID)
	if active != nil || interrupting || accepted > 0 || len(queued) > 0 {
		return
	}
	a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
		SessionID:    call.SessionID,
		SessionTitle: sess.Title,
		Type:         notify.TypeAgentFinished,
		RunID:        call.RunID,
	})
}

// ValidateCall performs the cheap structural validation that
// sessionAgent.Run requires before a call can be dispatched: a call must
// carry either a non-empty prompt or an attachment, and it must name a
// session. It is exported so callers that accept a run before dispatching it
// (e.g. backend.SendMessage) can apply the same checks and keep the error
// contract consistent.
func ValidateCall(call SessionAgentCall) error {
	if call.Prompt == "" && len(call.Attachments) == 0 {
		return ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return ErrSessionMissing
	}
	return nil
}

func (a *sessionAgent) getCacheControlOptions() fantasy.ProviderOptions {
	if t, _ := strconv.ParseBool(os.Getenv("CRUSH_DISABLE_ANTHROPIC_CACHE")); t {
		return fantasy.ProviderOptions{}
	}
	return fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		bedrock.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
		vercel.Name: &anthropic.ProviderCacheControlOptions{
			CacheControl: anthropic.CacheControl{Type: "ephemeral"},
		},
	}
}

// sessionHeaders returns the HTTP headers we use for cache affinity on
// every LLM request for a given session.
//
// We use the session hash is used instead of the raw UUID so the header
// value is deterministic and opaque.
func sessionHeaders(sessionID string) map[string]string {
	hash := session.HashID(sessionID)
	return map[string]string{
		"x-session-id":       hash,
		"x-session-affinity": hash,
	}
}

func (a *sessionAgent) SetModels(large Model, small Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
}

func providerRetryLogFields(err *fantasy.ProviderError, delay time.Duration) []any {
	fields := []any{
		"retry_delay", delay.String(),
	}
	if err == nil {
		return fields
	}
	fields = append(fields, "status_code", err.StatusCode)
	if err.Title != "" {
		fields = append(fields, "title", err.Title)
	}
	if err.Message != "" {
		fields = append(fields, "message", err.Message)
	}
	return fields
}

// sanitizeToolInput validates tool call JSON from the provider.
// Malformed input is replaced with an empty object to prevent
// stuck conversations from truncated or malformed model output.
// The second return value indicates whether sanitization occurred.
func sanitizeToolInput(toolName, toolCallID, input string) (string, bool) {
	if !json.Valid([]byte(input)) {
		slog.Warn(
			"Malformed tool call JSON from provider, replacing with empty object",
			"tool", toolName,
			"id", toolCallID,
			"input_len", len(input),
		)
		return "{}", true
	}
	return input, false
}
