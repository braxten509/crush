// Package agent is the core orchestration layer for Crush AI agents.
//
// It provides session-based AI agent functionality for managing
// conversations, tool execution, and message handling. It coordinates
// interactions between language models, messages, sessions, and tools while
// handling features like automatic summarization, queuing, and token
// management.
package agent

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/charmbracelet/x/ansi"
)

const (
	DefaultSessionName = "Untitled Session"
)

var userAgent = fmt.Sprintf("Charm-Crush/%s (https://charm.land/crush)", version.Version)

//go:embed templates/title.md
var titlePrompt []byte

//go:embed templates/summary.md
var summaryPrompt []byte

// Used to remove <think> tags from generated titles.
var (
	thinkTagRegex       = regexp.MustCompile(`(?s)<think>.*?</think>`)
	orphanThinkTagRegex = regexp.MustCompile(`</?think>`)
)

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

// filterToolsForChannel scopes the tool list for a turn. A channel-originated
// turn (channel != "") sees only the originating channel server's tools plus
// all non-channel tools — the model's reach is restricted to the channel it
// is replying through, so it cannot accidentally send via a different
// messaging backend. A local turn (channel == "") keeps every tool,
// including channel server tools, so a user in the TUI can still ask the
// agent to send a message through Signal or any other enabled channel.
func filterToolsForChannel(agentTools []fantasy.AgentTool, channel string, states map[string]mcp.ClientInfo) []fantasy.AgentTool {
	if channel == "" {
		return agentTools
	}
	filtered := make([]fantasy.AgentTool, 0, len(agentTools))
	for _, agentTool := range agentTools {
		mcpName, ok := toolMCPName(agentTool)
		if !ok {
			filtered = append(filtered, agentTool)
			continue
		}
		state, found := states[mcpName]
		if !found || !state.Channel || channel == mcpName {
			filtered = append(filtered, agentTool)
		}
	}
	return filtered
}

// toolMCPName looks through safety wrappers without removing them from the
// tool list. Both channel routing and repository toggles need the server name.
func toolMCPName(tool fantasy.AgentTool) (string, bool) {
	for {
		switch wrapped := tool.(type) {
		case guardedTool:
			tool = wrapped.AgentTool
		case *hookedTool:
			tool = wrapped.inner
		default:
			mcpTool, ok := tool.(interface{ MCP() string })
			if !ok {
				return "", false
			}
			return mcpTool.MCP(), true
		}
	}
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

	messageQueue   *csync.Map[string, []SessionAgentCall]
	activeRequests *csync.Map[string, *activeCancel]
	// steering holds the running agent CLI turn per session, whose steered
	// prompts still count as queued until the CLI takes them in.
	steering *csync.Map[string, *cliSteps]
	// interrupting holds prompts an Interrupt will run once the canceled run
	// ends; they count as queued and keep the session busy meanwhile.
	interrupting *csync.Map[string, []SessionAgentCall]
	// handoffs owns a queue snapshot, including acceptance leases not dispatched yet.
	handoffs *csync.Map[string, *queueHandoff]

	// dispatchMu holds a per-session mutex that serializes the
	// accepted -> (cancel-on-entry | queued | active) transition in
	// Run against a concurrent Cancel. The lock is held only during
	// the brief handoff (no DB or LLM I/O under the lock).
	dispatchMu *csync.Map[string, *sync.Mutex]
	// acceptedRuns counts dispatched-but-not-yet-active runs per
	// session. A counter > 0 means a dispatched prompt is in flight
	// and has not yet completed the dispatch handoff in Run. Only
	// BeginAccepted increments it; only AcceptedRun.Close decrements
	// it.
	acceptedRuns *csync.Map[string, int]
	// cancelMark records, per session, a high-water accept sequence: an
	// accepted handle is canceled by it iff the handle's sequence is at
	// or below the mark. Cancel raises the mark to the latest sequence
	// assigned at cancel time, so a single Cancel covers every prompt
	// accepted-but-not-yet-active then, while a prompt accepted later
	// (higher sequence) is never poisoned. Absent or 0 means no pending
	// cancel. It is only raised by Cancel when acceptedRuns > 0, so an
	// idle Escape never records a mark.
	cancelMark *csync.Map[string, uint64]
	// dispatchMuCreate guards lazy creation of per-session entries in
	// dispatchMu so two goroutines can't race to lock different mutex
	// instances for the same session.
	dispatchMuCreate sync.Mutex
	// acceptedMu serializes increments/decrements of acceptedRuns and
	// the assignment of accept sequence numbers from acceptSeqGen. It
	// is separate from dispatchMu so AcceptedRun.Close (which may run
	// while Run holds dispatchMu for the same session) does not
	// deadlock by re-entering the dispatch lock.
	acceptedMu sync.Mutex
	// acceptSeqGen is the monotonic source of accept sequence numbers.
	// Acceptance and untracked enqueue increment it under acceptedMu,
	// so sequences strictly increase in acceptance/enqueue order
	// across the agent. Cancel uses its current value as the per-session
	// high-water mark.
	acceptSeqGen uint64
	// acceptedLeases is guarded by acceptedMu; Close removes the exact lease.
	acceptedLeases map[string]map[uint64]*AcceptedRun
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
	return &sessionAgent{
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
		messageQueue:         csync.NewMap[string, []SessionAgentCall](),
		activeRequests:       csync.NewMap[string, *activeCancel](),
		steering:             csync.NewMap[string, *cliSteps](),
		interrupting:         csync.NewMap[string, []SessionAgentCall](),
		handoffs:             csync.NewMap[string, *queueHandoff](),
		acceptedLeases:       make(map[string]map[uint64]*AcceptedRun),
		dispatchMu:           csync.NewMap[string, *sync.Mutex](),
		acceptedRuns:         csync.NewMap[string, int](),
		cancelMark:           csync.NewMap[string, uint64](),
	}
}

// AcceptedRun owns exactly one accept reservation taken by
// BeginAccepted. It is the only carrier of accept-state across the
// backend.runAgent / Coordinator.Run / sessionAgent.Run layers: a
// counter > 0 means a dispatched prompt is in flight and has not yet
// completed the dispatch handoff in Run. Close is the only way to
// release the reservation and is idempotent.
type AcceptedRun struct {
	agent     *sessionAgent
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
	r.agent.endAccepted(r)
}

// SessionID exposes the session this reservation is for so the run path
// can use it without an extra parameter.
func (r *AcceptedRun) SessionID() string {
	if r == nil {
		return ""
	}
	return r.sessionID
}

// BeginAccepted increments the accept counter for sessionID and returns
// a handle whose Close is the only way to decrement it. It is the only
// entry point that mutates acceptedRuns.
func (a *sessionAgent) BeginAccepted(sessionID string) *AcceptedRun {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return a.beginAcceptedLocked(sessionID)
}

// beginAcceptedLocked requires dispatchMu, including for internal batch reservations.
func (a *sessionAgent) beginAcceptedLocked(sessionID string) *AcceptedRun {
	a.acceptedMu.Lock()
	defer a.acceptedMu.Unlock()
	count, _ := a.acceptedRuns.Get(sessionID)
	a.acceptedRuns.Set(sessionID, count+1)
	a.acceptSeqGen++
	followUp := a.IsSessionBusy(sessionID)
	for _, earlier := range a.acceptedLeases[sessionID] {
		if !earlier.done.Load() && !earlier.canceled.Load() && !a.canceledBySeq(sessionID, earlier.seq) {
			followUp = true
			break
		}
	}
	r := &AcceptedRun{agent: a, sessionID: sessionID, seq: a.acceptSeqGen, followUp: followUp}
	if a.acceptedLeases[sessionID] == nil {
		a.acceptedLeases[sessionID] = make(map[uint64]*AcceptedRun)
	}
	a.acceptedLeases[sessionID][r.seq] = r
	return r
}

// endAccepted decrements the accept counter for sessionID. It is only
// called via AcceptedRun.Close. It uses a dedicated lock (not the
// per-session dispatch mutex) so it can run while Run holds dispatchMu
// for the same session without deadlocking.
//
// When the count reaches zero the session's cancel mark is dropped: no
// accepted handle remains for it to cover, and any handle accepted later
// gets a strictly higher sequence that the mark would not match anyway.
// Handles canceled on entry never reach RunComplete, so this is the only
// place that clears the mark for an all-canceled batch. Sibling handles
// covered by the same mark are serialized on the per-session dispatch
// mutex and read the mark before they Close, so this never clears it out
// from under a covered handle still waiting to enter Run.
func (a *sessionAgent) endAccepted(r *AcceptedRun) {
	a.acceptedMu.Lock()
	defer a.acceptedMu.Unlock()
	sessionID := r.sessionID
	delete(a.acceptedLeases[sessionID], r.seq)
	if len(a.acceptedLeases[sessionID]) == 0 {
		delete(a.acceptedLeases, sessionID)
	}
	if r.handoff != nil {
		r.handoff.pending--
		r.handoff.signal()
	}
	count, ok := a.acceptedRuns.Get(sessionID)
	if !ok || count <= 1 {
		a.acceptedRuns.Del(sessionID)
		a.cancelMark.Del(sessionID)
		return
	}
	a.acceptedRuns.Set(sessionID, count-1)
}

// sessionMu returns the per-session dispatch mutex, creating it on first
// use. Creation is guarded so concurrent callers always observe the same
// mutex instance for a given session.
func (a *sessionAgent) sessionMu(sessionID string) *sync.Mutex {
	if mu, ok := a.dispatchMu.Get(sessionID); ok {
		return mu
	}
	a.dispatchMuCreate.Lock()
	defer a.dispatchMuCreate.Unlock()
	if mu, ok := a.dispatchMu.Get(sessionID); ok {
		return mu
	}
	mu := &sync.Mutex{}
	a.dispatchMu.Set(sessionID, mu)
	return mu
}

// enqueueCall appends call to the session's message queue. The
// OnComplete hook is stripped: the caller that supplied it (typically
// coordinator.Run) has its own retry/coalesce scope that ends when it
// returns, so by the time the queue drains nobody is left to consume the
// buffered terminal event. The recursive Run falls back to the default
// broker publish, which is what existing subscribers expect for queued
// turns.
func (a *sessionAgent) enqueueCall(call SessionAgentCall) {
	if len(call.batch) > 0 && !call.NotRecallable {
		for _, original := range call.batch {
			original.Accepted = call.Accepted
			a.enqueueCall(original)
		}
		return
	}
	existing, ok := a.messageQueue.Get(call.SessionID)
	if !ok {
		existing = []SessionAgentCall{}
	}
	queued := call
	if call.Accepted != nil {
		// Preserve the accept sequence after the handle is stripped so
		// the queue-drain paths can tell a follow-up queued before a
		// cancel (covered by the mark) from one queued after it.
		queued.acceptSeq = call.Accepted.seq
	}
	if call.sessionRun == nil {
		queued.OnComplete = nil
	}
	if queued.queueOrder == 0 {
		a.acceptedMu.Lock()
		if call.Accepted != nil {
			queued.queueOrder = call.Accepted.seq
		} else {
			a.acceptSeqGen++
			queued.queueOrder = a.acceptSeqGen
		}
		a.acceptedMu.Unlock()
	}
	queued.Accepted = nil
	if call.Accepted != nil && call.Accepted.handoff != nil {
		pending, _ := a.interrupting.Get(call.SessionID)
		a.interrupting.Set(call.SessionID, appendQueuedInOrder(pending, queued))
		return
	}
	existing = appendQueuedInOrder(existing, queued)
	a.messageQueue.Set(call.SessionID, existing)
	if s, _ := a.steering.Get(call.SessionID); s != nil {
		select {
		case s.steerReady <- struct{}{}:
		default:
		}
	}
}

// requeueFront puts calls back at the front of the session's queue.
func (a *sessionAgent) requeueFront(sessionID string, calls []SessionAgentCall) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	a.requeueFrontLocked(sessionID, calls)
}

// requeueFrontLocked requires the session's dispatch mutex.
func (a *sessionAgent) requeueFrontLocked(sessionID string, calls []SessionAgentCall) {
	queued, _ := a.messageQueue.Get(sessionID)
	a.messageQueue.Set(sessionID, append(slices.Clone(calls), queued...))
}

// compatibleQueuedCalls keeps channel tool scope and reply destinations
// separate. CLI continuations and summarization continuations must also keep
// their own dispatch semantics. Compatible adjacent submissions form one batch.
func (a *sessionAgent) compatibleQueuedCalls(first, next SessionAgentCall) bool {
	if first.CLIContinue || next.CLIContinue || first.NotRecallable || next.NotRecallable {
		return false
	}
	if first.Channel != next.Channel {
		return false
	}
	if first.Channel != "" {
		firstMeta, _ := parseChannelMeta(first.Prompt)
		nextMeta, _ := parseChannelMeta(next.Prompt)
		var reply *config.MCPChannelReply
		if a.cfg != nil {
			if channel, ok := a.cfg.Config().MCP[first.Channel]; ok {
				reply = channel.ChannelReply
				if reply == nil {
					reply = discoverChannelReply(first.Channel)
				}
			}
		}
		return sameQueuedChannelRoute(reply, firstMeta, nextMeta)
	}
	return true
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

// drainQueueForStep takes one ordered batch under the dispatch mutex. RunIDs
// do not split batches: completion is delivered to every original call.
func (a *sessionAgent) drainQueueForStep(sessionID string) (fold, canceled []SessionAgentCall) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return a.drainQueueForStepLocked(sessionID)
}

func (a *sessionAgent) drainQueueForStepLocked(sessionID string) (fold, canceled []SessionAgentCall) {
	queued, _ := a.messageQueue.Get(sessionID)
	var keep []SessionAgentCall
	blocked := false
	for _, call := range queued {
		if a.canceledBySeq(sessionID, call.acceptSeq) || (call.sessionRun != nil && call.sessionRun.ctx.Err() != nil) {
			if len(call.batch) > 0 || call.RunID != "" || call.SubmissionID != "" || call.OnComplete != nil || call.queuedSessionRunRelease != nil {
				canceled = append(canceled, call)
			}
			continue
		}
		if len(fold) > 0 && !a.compatibleQueuedCalls(fold[0], call) {
			blocked = true
		}
		if blocked {
			keep = append(keep, call)
		} else {
			fold = append(fold, call)
		}
	}
	if len(keep) == 0 {
		a.messageQueue.Del(sessionID)
	} else {
		a.messageQueue.Set(sessionID, keep)
	}
	return fold, canceled
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

// reserveBatchLocked makes the whole drain observable to Cancel before Run
// registers its active request. A single fresh lease covers the entire batch;
// canceled pre-drain siblings are filtered using their original acceptSeq.
func (a *sessionAgent) reserveBatchLocked(calls []SessionAgentCall) SessionAgentCall {
	call := calls[0]
	if len(calls) > 1 {
		call.batch = slices.Clone(calls)
		call.sessionRun = sharedBatchOwner(calls)
		call.queuedSessionRunRelease = nil
		call.OnComplete = nil
		call.Prompt = ""
		call.Attachments = nil
		var texts []string
		for _, original := range calls {
			texts = append(texts, message.PromptWithTextAttachments(original.Prompt, original.Attachments))
			for _, attachment := range original.Attachments {
				if !attachment.IsText() {
					call.Attachments = append(call.Attachments, attachment)
				}
			}
		}
		call.Prompt = strings.Join(texts, "\n\n")
	}
	call.acceptSeq = 0
	call.Accepted = a.beginAcceptedLocked(call.SessionID)
	// Cancellation here is covered by Accepted's high-water sequence. Run
	// observes that mark before consuming the lease or entering the model.
	call.batchReservation = &activeCancel{cancel: func() {}}
	a.activeRequests.Set(call.SessionID, call.batchReservation)
	return call
}

// finishDispatch runs after terminal publication, on every active Run exit.
// Interrupt owns its replacement queue until its waiting dispatcher takes it.
func (a *sessionAgent) finishDispatch(ctx context.Context, sessionID string, ac *activeCancel) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	a.activeRequests.CompareAndDelete(sessionID, ac)
	handoff, interrupted := a.handoffs.Get(sessionID)
	if interrupted && handoff.reservation == ac && ac != nil {
		a.handoffs.Del(sessionID)
		interrupted = false
	}
	if interrupted || a.isRunning(sessionID) {
		if interrupted {
			handoff.signal()
		}
		mu.Unlock()
		return
	}
	// An accepted follow-up belongs to this drain even if its goroutine
	// has not entered Run yet. Close/validation failure releases the barrier.
	if a.hasAcceptedFollowUpsLocked(sessionID) {
		a.startQueueHandoffLocked(sessionID, false)
		mu.Unlock()
		return
	}
	calls, canceled := a.drainQueueForStepLocked(sessionID)
	var next SessionAgentCall
	if len(calls) > 0 {
		next = a.reserveBatchLocked(calls)
	} else {
		a.acceptedMu.Lock()
		inFlight, _ := a.acceptedRuns.Get(sessionID)
		if inFlight == 0 {
			a.cancelMark.Del(sessionID)
		}
		a.acceptedMu.Unlock()
	}
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	if len(calls) > 0 {
		// Each queued call carries its own owner and provenance. Do not
		// inherit the finished turn's sessionRun or submission context.
		if _, err := a.Run(context.Background(), next); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Queued batch failed", "session_id", sessionID, "error", err)
		}
	}
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

// publishCanceledQueueDrops emits a terminal cancelled RunComplete for
// every dropped queued call that carries a RunID. A queued prompt removed
// from the queue without ever running — covered by a pending cancel, or
// cleared by Cancel/ClearQueue — would otherwise leave a caller blocked on
// that RunID: `crush run` ignores live message events and exits only on a
// RunComplete whose RunID matches. Calls without a RunID had no such waiter
// and are dropped silently as before. A detached, bounded context keeps the
// must-deliver publish alive even when the run context that triggered the
// drop is already canceled.
func (a *sessionAgent) publishCanceledQueueDrops(drops []SessionAgentCall) {
	var needsCompletion bool
	for _, d := range drops {
		if len(d.batch) > 0 || d.RunID != "" || d.SubmissionID != "" || d.OnComplete != nil || d.queuedSessionRunRelease != nil {
			needsCompletion = true
			break
		}
	}
	if !needsCompletion {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, d := range drops {
		if len(d.batch) > 0 || d.RunID != "" || d.SubmissionID != "" || d.OnComplete != nil {
			a.publishRunComplete(ctx, d, notify.RunComplete{
				SessionID: d.SessionID,
				RunID:     d.RunID,
				Cancelled: true,
			})
		}
		releaseQueuedCall(d, context.Canceled)
	}
}

// clearQueueAndNotify removes all queued prompts for the session and
// publishes a terminal cancelled RunComplete for any that carried a RunID,
// so callers waiting on those RunIDs (e.g. `crush run`) are not left
// hanging when their queued prompt is discarded without running.
func (a *sessionAgent) clearQueueAndNotify(sessionID string) {
	if h, ok := a.handoffs.Get(sessionID); ok {
		h.aborted = true
		a.cancelHandoffLeasesLocked(sessionID, h)
		h.signal()
	}
	queued, ok := a.messageQueue.Get(sessionID)
	a.messageQueue.Del(sessionID)
	if s, _ := a.steering.Get(sessionID); s != nil {
		queued = append(s.takePendingSteered(), queued...)
		ok = ok || len(queued) > 0
	}
	if pending, took := a.interrupting.Take(sessionID); took {
		queued, ok = append(pending, queued...), true
	}
	if !ok {
		return
	}
	a.publishCanceledQueueDrops(queued)
}

// clearPendingCancel removes any pending-cancel mark for sessionID. It
// takes the per-session dispatch lock so it is ordered against Cancel
// and the dispatch handoff.
func (a *sessionAgent) clearPendingCancel(sessionID string) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	a.cancelMark.Del(sessionID)
}

// canceledBySeq reports whether an accepted handle or queued call with
// the given accept sequence is covered by a pending cancel for the
// session. Callers must hold the session's dispatch mutex. A tracked
// sequence (seq > 0) is covered only when it is at or below the cancel
// high-water mark, so a prompt accepted after the cancel (higher seq) is
// never poisoned. An untracked sequence (seq == 0, an in-process enqueue
// with no accept reservation) is covered whenever any mark is present,
// preserving the pre-sequence behavior. The mark is not consumed: it
// stays so every sibling handle it covers observes the same cancel, and
// a later handle (higher seq) ignores it regardless.
func (a *sessionAgent) canceledBySeq(sessionID string, seq uint64) bool {
	mark, ok := a.cancelMark.Get(sessionID)
	if !ok || mark == 0 {
		return false
	}
	return seq == 0 || seq <= mark
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
	if a.tasks.hasRunning(call.SessionID) {
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

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions, onAuthRefresh func(context.Context, *fantasy.ProviderError) error) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}
	// A summary replaces the agent's memory: resend kept skills in full.
	defer forgetKeptSkillsSent(sessionID)

	// Copy mutable fields under lock to avoid races with SetModels.
	largeModel := a.largeModel.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		// Nothing to summarize.
		return nil
	}

	aiMsgs, _ := a.preparePrompt(msgs, largeModel.CatwalkCfg.SupportsImages)

	genCtx, cancel := context.WithCancel(ctx)
	ac := &activeCancel{cancel: cancel}
	a.activeRequests.Set(sessionID, ac)
	defer a.activeRequests.CompareAndDelete(sessionID, ac)
	defer cancel()
	defer func() {
		if flushErr := a.messages.FlushAll(ctx); flushErr != nil {
			slog.Error("Failed to flush pending message updates after summarize", "error", flushErr)
		}
	}()

	agent := fantasy.NewAgent(
		largeModel.Model,
		fantasy.WithSystemPrompt(string(summaryPrompt)),
		fantasy.WithUserAgent(userAgent),
	)
	summaryMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            largeModel.ModelCfg.Model,
		Provider:         largeModel.ModelCfg.Provider,
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}

	summaryPromptText := buildSummaryPrompt(currentSession.Todos)

	var resp *fantasy.AgentResult
	if cliModel, ok := largeModel.Model.(*cliagent.Model); ok {
		resp, err = a.cliSummarize(genCtx, cliModel, sessionID, largeModel.ModelCfg.ReasoningEffort, msgs, summaryPromptText, &summaryMessage)
	}
	if resp == nil && err == nil {
		resp, err = agent.Stream(genCtx, fantasy.AgentStreamCall{
			Prompt:          summaryPromptText,
			Messages:        aiMsgs,
			Headers:         sessionHeaders(sessionID),
			ProviderOptions: opts,
			OnAuthRefresh:   onAuthRefresh,
			ModelProvider: func() fantasy.LanguageModel {
				return a.largeModel.Get().Model
			},
			PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
				prepared.Messages = options.Messages
				if systemPromptPrefix != "" {
					prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(systemPromptPrefix)}, prepared.Messages...)
				}
				return callContext, prepared, nil
			},
			OnReasoningDelta: func(id string, text string) error {
				summaryMessage.AppendReasoningContent(text)
				return a.messages.Update(genCtx, summaryMessage)
			},
			OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
				// Handle anthropic signature.
				if anthropicData, ok := reasoning.ProviderMetadata["anthropic"]; ok {
					if signature, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && signature.Signature != "" {
						summaryMessage.AppendReasoningSignature(signature.Signature)
					}
				}
				summaryMessage.FinishThinking()
				return a.messages.Update(genCtx, summaryMessage)
			},
			OnTextDelta: func(id, text string) error {
				summaryMessage.AppendContent(text)
				return a.messages.Update(genCtx, summaryMessage)
			},
		})
	}
	if err != nil {
		isCancelErr := errors.Is(err, context.Canceled)
		if isCancelErr {
			// User cancelled summarize we need to remove the summary message.
			deleteErr := a.messages.Delete(ctx, summaryMessage.ID)
			return deleteErr
		}
		// Mark the summary message as finished with an error so the UI
		// stops spinning.
		summaryMessage.AddFinish(message.FinishReasonError, "Summarization Error", err.Error())
		if updateErr := a.messages.Update(ctx, summaryMessage); updateErr != nil {
			return updateErr
		}
		return err
	}

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	err = a.messages.Update(genCtx, summaryMessage)
	if err != nil {
		return err
	}

	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	a.updateSessionUsage(largeModel, &currentSession, resp.TotalUsage, openrouterCost, false)

	// Just in case, get just the last usage info.
	usage := resp.Response.Usage
	currentSession.SummaryMessageID = summaryMessage.ID
	currentSession.CompletionTokens = summaryCompletionTokens(usage, summaryMessage)
	currentSession.PromptTokens = 0
	currentSession.EstimatedUsage = usageIsZero(usage)
	_, err = a.sessions.Save(genCtx, currentSession)
	if err != nil {
		return err
	}

	// Release the active request before processing queued messages so that
	// Run() does not see the session as busy.
	mu := a.sessionMu(sessionID)
	mu.Lock()
	a.activeRequests.Del(sessionID)
	cancel()

	// Process any messages that were queued while summarizing.
	queuedMessages, ok := a.messageQueue.Get(sessionID)
	if !ok || len(queuedMessages) == 0 {
		mu.Unlock()
		return nil
	}
	calls, canceled := a.drainQueueForStepLocked(sessionID)
	if len(calls) == 0 {
		mu.Unlock()
		a.publishCanceledQueueDrops(canceled)
		return nil
	}
	next := a.reserveBatchLocked(calls)
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	_, qErr := a.Run(ctx, next)
	return qErr
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

// foldedPrompt is a queued prompt taken into a native turn, and where it
// went in that step's messages (before later steps' additions).
type foldedPrompt struct {
	at  int
	msg fantasy.Message
}

// takeQueuedForStep takes the queued prompts that can join call's turn at a
// tool boundary, saves them as user messages and returns them for the step.
func (a *sessionAgent) takeQueuedForStep(ctx context.Context, call SessionAgentCall, supportsImages bool) ([]fantasy.Message, error) {
	mu := a.sessionMu(call.SessionID)
	mu.Lock()
	var fold, canceled []SessionAgentCall
	if queued, _ := a.messageQueue.Get(call.SessionID); len(queued) > 0 && a.compatibleQueuedCalls(call, queued[0]) {
		fold, canceled = a.drainQueueForStepLocked(call.SessionID)
	}
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	if len(fold) == 0 {
		return nil, nil
	}
	if call.onQueuedInput != nil {
		call.onQueuedInput(fold)
	}
	msgs := make([]fantasy.Message, 0, len(fold))
	for _, q := range fold {
		if _, err := a.createUserMessage(ctx, q); err != nil {
			return nil, err
		}
		_, files := a.preparePrompt(nil, supportsImages, q.Attachments...)
		var parts []fantasy.MessagePart
		if text := message.PromptWithTextAttachments(q.Prompt, q.Attachments); text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		parts = append(parts, filesAsParts(files)...)
		msgs = append(msgs, fantasy.Message{Role: fantasy.MessageRoleUser, Content: parts})
	}
	return msgs, nil
}

func (a *sessionAgent) createUserMessage(ctx context.Context, call SessionAgentCall) (message.Message, error) {
	if len(call.batch) > 0 {
		var last message.Message
		for _, original := range call.batch {
			msg, err := a.createUserMessage(ctx, original)
			if err != nil {
				return message.Message{}, err
			}
			last = msg
		}
		return last, nil
	}
	parts := []message.ContentPart{message.TextContent{Text: call.Prompt, Hidden: call.HiddenUserMessage, SubmissionID: call.SubmissionID}}
	var attachmentParts []message.ContentPart
	for _, attachment := range call.Attachments {
		attachmentParts = append(attachmentParts, message.BinaryContent{Path: cmp.Or(attachment.FilePath, attachment.FileName), MIMEType: attachment.MimeType, Data: attachment.Content})
	}
	parts = append(parts, attachmentParts...)
	msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
	if err != nil {
		return message.Message{}, fmt.Errorf("failed to create user message: %w", err)
	}
	return msg, nil
}

func (a *sessionAgent) preparePrompt(msgs []message.Message, supportsImages bool, attachments ...message.Attachment) ([]fantasy.Message, []fantasy.FilePart) {
	var history []fantasy.Message
	if !a.isSubAgent {
		history = append(history, fantasy.NewUserMessage(
			fmt.Sprintf(
				"<system_reminder>%s</system_reminder>",
				`This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware.
If you are working on tasks that would benefit from a todo list please use the "todos" tool to create one.
If not, please feel free to ignore. Again do not mention this message to the user.`,
			),
		))
	}
	// Collect all tool call IDs present in assistant messages, then index
	// every tool result by its call ID. Tool results are re-emitted right
	// after the assistant message that requested them instead of at their
	// stored position: messages can be written to a session concurrently
	// (e.g. resuming while a tool is still running), which interleaves
	// user messages between a tool call and its result. LLM APIs require
	// every tool call to be followed by its results before any other
	// message, and strict-adjacency providers (e.g. Kimi, DeepSeek) reject
	// the request otherwise, permanently locking the session.
	knownToolCallIDs := make(map[string]struct{})
	for _, m := range msgs {
		if m.Role != message.Assistant {
			continue
		}
		for _, tc := range m.ToolCalls() {
			knownToolCallIDs[tc.ID] = struct{}{}
		}
	}
	toolResultsByCall := make(map[string][]fantasy.MessagePart)
	for _, m := range msgs {
		if m.Role != message.Tool {
			continue
		}
		for _, aiMsg := range m.ToAIMessage() {
			for _, part := range aiMsg.Content {
				tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
				if !ok {
					// Tool-role ToAIMessage only emits ToolResultParts today;
					// log so unexpected parts do not vanish silently.
					slog.Warn(
						"Dropping unexpected non-tool-result part from tool message",
						"part_type", fmt.Sprintf("%T", part),
					)
					continue
				}
				if _, known := knownToolCallIDs[tr.ToolCallID]; !known {
					slog.Warn(
						"Dropping orphaned tool result with no matching tool call",
						"tool_call_id", tr.ToolCallID,
					)
					continue
				}
				toolResultsByCall[tr.ToolCallID] = append(toolResultsByCall[tr.ToolCallID], part)
			}
		}
	}

	for _, m := range msgs {
		if len(m.Parts) == 0 {
			continue
		}
		// Assistant message without content or tool calls (cancelled before it returned anything).
		if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().String() == "" {
			continue
		}
		// Tool results are emitted right after their assistant message.
		if m.Role == message.Tool {
			continue
		}
		aiMsgs := m.ToAIMessage()
		if !supportsImages {
			for i := range aiMsgs {
				if aiMsgs[i].Role == fantasy.MessageRoleUser {
					aiMsgs[i].Content = filterFileParts(aiMsgs[i].Content)
				}
			}
		}
		history = append(history, aiMsgs...)

		if m.Role == message.Assistant && len(m.ToolCalls()) > 0 {
			history = append(history, toolResultsForCalls(m, toolResultsByCall))
		}
	}

	var files []fantasy.FilePart
	for _, attachment := range attachments {
		if attachment.IsText() {
			continue
		}
		if !supportsImages {
			continue
		}
		files = append(files, fantasy.FilePart{
			Filename:  attachment.FileName,
			Data:      attachment.Content,
			MediaType: attachment.MimeType,
		})
	}

	return history, files
}

func filesAsParts(files []fantasy.FilePart) []fantasy.MessagePart {
	parts := make([]fantasy.MessagePart, 0, len(files))
	for _, f := range files {
		parts = append(parts, f)
	}
	return parts
}

// filterDisabledMCPTools removes tools from MCP servers disabled via the
// "Toggle MCPs" dialog. The override set is repository-scoped and shared
// by every session in the repository, including sub-agent sessions.
// Connections are process-global and left untouched; only the tool list
// changes.
func (a *sessionAgent) filterDisabledMCPTools(ctx context.Context, toolList []fantasy.AgentTool) []fantasy.AgentTool {
	disabledServers, err := a.sessions.MCPDisabledServers(ctx)
	if err != nil {
		slog.Error("Failed to list disabled MCP servers", "error", err)
		return toolList
	}
	if len(disabledServers) == 0 {
		return toolList
	}
	disabled := make(map[string]struct{}, len(disabledServers))
	for _, name := range disabledServers {
		disabled[name] = struct{}{}
	}
	filtered := make([]fantasy.AgentTool, 0, len(toolList))
	for _, t := range toolList {
		if mcpName, ok := toolMCPName(t); ok {
			if _, off := disabled[mcpName]; off {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// filterFileParts removes fantasy.FilePart entries from a slice of message
// parts. Used to strip image attachments from historical user messages when
// the current model does not support them.
func filterFileParts(parts []fantasy.MessagePart) []fantasy.MessagePart {
	filtered := make([]fantasy.MessagePart, 0, len(parts))
	for _, part := range parts {
		if _, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}

// toolResultsForCalls builds the tool message that must immediately follow
// an assistant message with tool calls. LLM APIs require every tool call to
// be followed by its results before any other message; strict-adjacency
// providers reject the request otherwise. Results are taken from
// toolResultsByCall and consumed, so a result stored in a message that also
// holds results for calls of other assistant messages is emitted exactly
// once, next to the assistant that requested it. Tool calls without any
// stored result (e.g. an interrupted session) receive a synthetic error
// response so the conversation keeps working.
func toolResultsForCalls(m message.Message, toolResultsByCall map[string][]fantasy.MessagePart) fantasy.Message {
	content := make([]fantasy.MessagePart, 0, len(m.ToolCalls()))
	for _, tc := range m.ToolCalls() {
		parts := toolResultsByCall[tc.ID]
		delete(toolResultsByCall, tc.ID)
		if len(parts) > 0 {
			content = append(content, parts...)
			continue
		}
		slog.Warn(
			"Injecting synthetic tool result for orphaned tool call",
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
		)
		content = append(content, fantasy.ToolResultPart{
			ToolCallID: tc.ID,
			Output: fantasy.ToolResultOutputContentError{
				Error: errors.New("tool call was interrupted and did not produce a result, you may retry this call if the result is still needed"),
			},
		})
	}
	return fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: content,
	}
}

func (a *sessionAgent) getSessionMessages(ctx context.Context, session session.Session) ([]message.Message, error) {
	// Read only the tail a compacted session actually sends. The full
	// transcript can be tens of megabytes on the single shared connection.
	msgs, err := a.messages.ListFromSummary(ctx, session.ID, session.SummaryMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if session.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgIndex = i
				break
			}
		}
		if summaryMsgIndex != -1 {
			msgs = msgs[summaryMsgIndex:]
			msgs[0].Role = message.User
		}
	}
	return msgs, nil
}

// hasUserTextMessage reports whether any user message in msgs contains
// text content (as opposed to only shell commands or other non-text parts).
func hasUserTextMessage(msgs []message.Message) bool {
	for _, msg := range msgs {
		if msg.Role != message.User {
			continue
		}
		for _, part := range msg.Parts {
			if tc, ok := part.(message.TextContent); ok && tc.Text != "" {
				return true
			}
		}
	}
	return false
}

// GenerateTitle generates a session title based on the initial prompt.
func (a *sessionAgent) GenerateTitle(ctx context.Context, sessionID string, userPrompt string) {
	if userPrompt == "" {
		return
	}

	// Ensure the session always gets a title even if every path below
	// fails or the context is cancelled before we finish.
	var titleSaved bool
	defer func() {
		if !titleSaved {
			fallbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := a.sessions.Rename(fallbackCtx, sessionID, DefaultSessionName); err != nil {
				slog.Error("Failed to save fallback session title", "error", err)
			}
		}
	}()

	smallModel := a.smallModel.Get()
	largeModel := a.largeModel.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	newAgent := func(m fantasy.LanguageModel, p []byte, tok int64) fantasy.Agent {
		return fantasy.NewAgent(
			m,
			fantasy.WithSystemPrompt(string(p)+"\n /no_think"),
			fantasy.WithMaxOutputTokens(tok),
			fantasy.WithUserAgent(userAgent),
		)
	}

	streamCall := fantasy.AgentStreamCall{
		Prompt:  fmt.Sprintf("Generate a concise title for the following content:\n\n%s\n <think>\n\n</think>", userPrompt),
		Headers: sessionHeaders(sessionID),
		PrepareStep: func(callCtx context.Context, opts fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = opts.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{
					fantasy.NewSystemMessage(systemPromptPrefix),
				}, prepared.Messages...)
			}
			return callCtx, prepared, nil
		},
	}

	type modelAttempt struct {
		name  string
		model Model
	}
	attempts := []modelAttempt{
		{"small", smallModel},
		{"large", largeModel},
	}

	var resp *fantasy.AgentResult
	var err error
	var model Model
	var success bool
	for _, attempt := range attempts {
		tok := int64(40)
		if attempt.model.CatwalkCfg.CanReason {
			tok = attempt.model.CatwalkCfg.DefaultMaxTokens
		}
		agent := newAgent(attempt.model.Model, titlePrompt, tok)
		resp, err = agent.Stream(ctx, streamCall)
		if err == nil && resp.Response.FinishReason != fantasy.FinishReasonLength {
			model = attempt.model
			slog.Debug("Generated title with " + attempt.name + " model")
			success = true
			break
		}
		if err != nil {
			slog.Error("Error generating title with "+attempt.name+" model; trying next", "err", err)
		} else {
			slog.Error("Title generation hit token limit with " + attempt.name + " model; trying next")
		}
	}
	if !success {
		// The deferred fallback will save the default session name.
		return
	}

	// Clean up title.
	var title string
	title = strings.ReplaceAll(resp.Response.Content.Text(), "\n", " ")

	// Remove thinking tags if present.
	title = thinkTagRegex.ReplaceAllString(title, "")
	title = orphanThinkTagRegex.ReplaceAllString(title, "")

	title = strings.TrimSpace(title)
	if title == "" {
		// LLM returned empty content. Use the prompt itself as a
		// fallback title, truncated to 50 chars, before resorting to
		// the generic default.
		fallback := strings.ReplaceAll(userPrompt, "\n", " ")
		fallback = strings.TrimSpace(fallback)
		if len(fallback) > 50 {
			fallback = ansi.Truncate(fallback, 50, "…")
		}
		title = cmp.Or(fallback, DefaultSessionName)
	}

	// Calculate usage and cost.
	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(resp.TotalUsage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(resp.TotalUsage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(resp.TotalUsage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(resp.TotalUsage.OutputTokens)

	// Use override cost if available (e.g., from OpenRouter).
	if openrouterCost != nil {
		cost = *openrouterCost
	}

	// Skip cost accumulation
	if model.FlatRate {
		cost = 0
	}

	promptTokens := contextTokens(resp.TotalUsage)
	completionTokens := resp.TotalUsage.OutputTokens

	// Atomically update only title and usage fields to avoid overriding other
	// concurrent session updates.
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, promptTokens, completionTokens, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
		return
	}
	titleSaved = true
}

func (a *sessionAgent) openrouterCost(metadata fantasy.ProviderMetadata) *float64 {
	openrouterMetadata, ok := metadata[openrouter.Name]
	if !ok {
		return nil
	}

	opts, ok := openrouterMetadata.(*openrouter.ProviderMetadata)
	if !ok {
		return nil
	}
	return &opts.Usage.Cost
}

// extractPrismModel returns the ID and name of the model that actually
// served the turn, as reported by the Hyper Prism model router headers,
// or empty strings when the turn was not routed through a Prism model.
func extractPrismModel(metadata fantasy.ProviderMetadata) (modelID, modelName string) {
	openaiMeta, ok := metadata[openai.Name]
	if !ok {
		return "", ""
	}
	pm, ok := openaiMeta.(*openai.ProviderMetadata)
	if !ok {
		return "", ""
	}
	_ = pm.ExtraField(hyper.PrismModelIDField, &modelID)
	_ = pm.ExtraField(hyper.PrismModelNameField, &modelName)
	return modelID, modelName
}

// extractPrismSavings returns the hypercredit and dollar savings from
// routing the turn through the Hyper Prism model router, as reported by
// its savings trailers, or nil when not reported or malformed.
func extractPrismSavings(metadata fantasy.ProviderMetadata) (hypercredits, dollars *float64) {
	openaiMeta, ok := metadata[openai.Name]
	if !ok {
		return nil, nil
	}
	pm, ok := openaiMeta.(*openai.ProviderMetadata)
	if !ok {
		return nil, nil
	}
	return extraFieldFloat(pm, hyper.PrismHypercreditSavingsField), extraFieldFloat(pm, hyper.PrismDollarSavingsField)
}

func extraFieldFloat(pm *openai.ProviderMetadata, key string) *float64 {
	var value string
	if !pm.ExtraField(key, &value) {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		slog.Warn("Could not parse Prism savings", "key", key, "value", value, "error", err)
		return nil
	}
	return &parsed
}

func (a *sessionAgent) updateSessionUsage(model Model, session *session.Session, usage fantasy.Usage, overrideCost *float64, estimated bool) {
	if !usageIsZero(usage) {
		session.EstimatedUsage = estimated
	}

	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	if !estimated {
		a.eventTokensUsed(session.ID, model, usage, cost)
	}

	if estimated {
		cost = 0
	} else {
		// Use override cost if available (e.g., from OpenRouter).
		if overrideCost != nil {
			cost = *overrideCost
		}

		// Skip cost accumulation
		if model.FlatRate {
			cost = 0
		}
	}

	session.Cost += cost
	updateSessionTokenCounters(session, usage)
}

// contextTokens returns the size of the prompt the provider processed for
// a step. Providers with prompt caching (Anthropic, Bedrock, Vercel) report
// the prompt as three disjoint buckets: tokens served from cache, tokens
// newly written to the cache, and the uncached remainder. All three occupy
// the context window, so all three count. Providers without cache writes
// leave CacheCreationTokens at zero, and the OpenAI-style providers already
// subtract cached tokens from InputTokens, so nothing is counted twice.
func contextTokens(usage fantasy.Usage) int64 {
	return usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

func updateSessionTokenCounters(session *session.Session, usage fantasy.Usage) {
	if usage.OutputTokens != 0 {
		session.CompletionTokens = usage.OutputTokens
	}
	if promptTokens := contextTokens(usage); promptTokens != 0 {
		session.PromptTokens = promptTokens
	}
}

func summaryCompletionTokens(usage fantasy.Usage, summaryMessage message.Message) int64 {
	if usage.OutputTokens != 0 {
		return usage.OutputTokens
	}
	return approxTokenCount(summaryMessage.Content().Text) + approxTokenCount(summaryMessage.ReasoningContent().String())
}

func (a *sessionAgent) Cancel(sessionID string) {
	// Serialize against the dispatch handoff in Run so the accepted ->
	// (cancel-on-entry | queued | active) transition is atomic against
	// this cancel. Every cancel observes at least one of: an active
	// request, an accepted run (recorded as a pending cancel), or a
	// queue entry it then clears. If none of those hold, an idle Escape
	// is a true no-op and must not poison the next prompt.
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	a.cancelLocked(sessionID)
}

// cancelLocked requires the session's dispatch mutex.
func (a *sessionAgent) cancelLocked(sessionID string) {
	a.cancelActiveLocked(sessionID)

	// Record a pending cancel only when a dispatched-but-not-yet-active
	// run exists. This catches runs still in the goroutine scheduler or
	// about to enter Run's busy-queue branch, while leaving an idle
	// session untouched. Active and accepted are not mutually exclusive:
	// when a run is active and a follow-up has been accepted, both the
	// cancel above and this pending record fire.
	//
	// Raise the session's cancel mark to the latest accept sequence
	// assigned so far. Every prompt currently accepted-but-not-yet-
	// active has a sequence at or below that value, so one cancel covers
	// all of them; a prompt accepted after this cancel gets a strictly
	// higher sequence and is never poisoned. Using max keeps repeated
	// cancels idempotent while the same prompts are in flight and lets a
	// later cancel extend coverage to prompts accepted since.
	a.acceptedMu.Lock()
	count, ok := a.acceptedRuns.Get(sessionID)
	mark := a.acceptSeqGen
	a.acceptedMu.Unlock()
	if ok && count > 0 {
		slog.Debug("Recording cancel mark for accepted runs", "session_id", sessionID, "count", count, "mark", mark)
		existing, _ := a.cancelMark.Get(sessionID)
		a.cancelMark.Set(sessionID, max(existing, mark))
	}

	a.clearQueueAndNotify(sessionID)
}

// cancelActiveLocked requires the session dispatch mutex.
func (a *sessionAgent) cancelActiveLocked(sessionID string) {
	// Cancel regular requests. Don't use Take() here - we need the entry to
	// remain in activeRequests so IsBusy() returns true until the goroutine
	// fully completes (including error handling that may access the DB).
	// The defer in processRequest will clean up the entry.
	if ac, ok := a.activeRequests.Get(sessionID); ok && ac != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		ac.cancel()
	}

	// Also check for summarize requests.
	if ac, ok := a.activeRequests.Get(sessionID + "-summarize"); ok && ac != nil {
		slog.Debug("Summarize cancellation initiated", "session_id", sessionID)
		ac.cancel()
	}
}

// Interrupt preserves follow-ups and returns immediately. Repeats while the
// canceled turn and its replacement exchange ownership are idempotent.
func (a *sessionAgent) Interrupt(sessionID string) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	if _, transitioning := a.handoffs.Get(sessionID); transitioning {
		return
	}
	a.startQueueHandoffLocked(sessionID, true)
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	a.clearQueueAndNotify(sessionID)
}

// RecallQueuedPrompt withdraws the newest prompt still owned by Crush.
// Prompts already handed to a CLI cannot be unsent, even before its echo.
func (a *sessionAgent) RecallQueuedPrompt(sessionID string) *message.QueuedPrompt {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	var recalled *SessionAgentCall
	for _, queue := range []*csync.Map[string, []SessionAgentCall]{a.messageQueue, a.interrupting} {
		calls, _ := queue.Get(sessionID)
		for i := len(calls) - 1; i >= 0; i-- {
			call := calls[i]
			if call.NotRecallable || call.HiddenUserMessage || call.CLIContinue || call.Channel != "" {
				continue
			}
			// Queue readers retain the old slice; publish a new one rather
			// than changing their backing array in place.
			calls = slices.Concat(calls[:i], calls[i+1:])
			if len(calls) == 0 {
				queue.Del(sessionID)
			} else {
				queue.Set(sessionID, calls)
			}
			recalled = &call
			break
		}
		if recalled != nil {
			break
		}
	}
	mu.Unlock()
	if recalled == nil {
		return nil
	}
	// Complete the original queued submission so headless callers and their
	// reservations cannot hang; a later editor submission is a new request.
	a.publishCanceledQueueDrops([]SessionAgentCall{*recalled})
	return &message.QueuedPrompt{Prompt: recalled.Prompt, Attachments: recalled.Attachments, SubmissionID: recalled.SubmissionID}
}

func (a *sessionAgent) CancelAll() {
	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *sessionAgent) IsBusy() bool {
	busy := a.handoffs.Len() > 0 || a.interrupting.Len() > 0
	for ac := range a.activeRequests.Seq() {
		if ac != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, pending := a.interrupting.Get(sessionID)
	_, transitioning := a.handoffs.Get(sessionID)
	return a.isRunning(sessionID) || pending || transitioning
}

func (a *sessionAgent) isRunning(sessionID string) bool {
	_, ok := a.activeRequests.Get(sessionID)
	return ok
}

func (a *sessionAgent) QueuedPrompts(sessionID string) int {
	return len(a.QueuedPromptsList(sessionID))
}

// QueuedPromptsList lists the queued prompts, led by any an agent CLI has
// been handed but not taken in yet, so they stay visible until they land.
func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	var prompts []string
	if s, _ := a.steering.Get(sessionID); s != nil {
		prompts = s.pendingSteered()
	}
	pending, _ := a.interrupting.Get(sessionID)
	for _, call := range pending {
		prompts = append(prompts, call.Prompt)
	}
	l, _ := a.messageQueue.Get(sessionID)
	for _, call := range l {
		prompts = append(prompts, call.Prompt)
	}
	return prompts
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

// convertToToolResult converts a fantasy tool result to a message tool result.
func (a *sessionAgent) convertToToolResult(result fantasy.ToolResultContent) message.ToolResult {
	baseResult := message.ToolResult{
		ToolCallID: result.ToolCallID,
		Name:       result.ToolName,
		Metadata:   result.ClientMetadata,
	}

	switch result.Result.GetType() {
	case fantasy.ToolResultContentTypeText:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Result); ok {
			baseResult.Content = r.Text
		}
	case fantasy.ToolResultContentTypeError:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Result); ok {
			baseResult.Content = r.Error.Error()
			baseResult.IsError = true
		}
	case fantasy.ToolResultContentTypeMedia:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Result); ok {
			if !stringext.IsValidBase64(r.Data) {
				slog.Warn(
					"Tool returned media with invalid base64 data, discarding image",
					"tool", result.ToolName,
					"tool_call_id", result.ToolCallID,
				)
				baseResult.Content = "Tool returned image data with invalid encoding"
				baseResult.IsError = true
			} else {
				content := r.Text
				if content == "" {
					content = fmt.Sprintf("Loaded %s content", r.MediaType)
				}
				baseResult.Content = content
				baseResult.Data = r.Data
				baseResult.MIMEType = r.MediaType
			}
		}
	}

	return baseResult
}

// workaroundProviderMediaLimitations converts media content in tool results to
// user messages for providers that don't natively support images in tool results.
//
// Problem: OpenAI, Google, OpenRouter, and other OpenAI-compatible providers
// don't support sending images/media in tool result messages - they only accept
// text in tool results. However, they DO support images in user messages.
//
// If we send media in tool results to these providers, the API returns an error.
//
// Solution: For these providers, we:
//  1. Replace the media in the tool result with a text placeholder
//  2. Inject a user message immediately after with the image as a file attachment
//  3. This maintains the tool execution flow while working around API limitations
//
// Anthropic and Bedrock support images natively in tool results, so we skip
// this workaround for them.
//
// Example transformation:
//
//	BEFORE: [tool result: image data]
//	AFTER:  [tool result: "Image loaded - see attached"], [user: image attachment]
func (a *sessionAgent) workaroundProviderMediaLimitations(messages []fantasy.Message, largeModel Model) []fantasy.Message {
	providerSupportsMedia := largeModel.ModelCfg.Provider == string(catwalk.InferenceProviderAnthropic) ||
		largeModel.ModelCfg.Provider == string(catwalk.InferenceProviderBedrock) ||
		largeModel.ModelCfg.Provider == string(catwalk.InferenceProviderBedrockEurope)

	if providerSupportsMedia {
		return messages
	}

	supportsImages := largeModel.CatwalkCfg.SupportsImages

	convertedMessages := make([]fantasy.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Role != fantasy.MessageRoleTool {
			convertedMessages = append(convertedMessages, msg)
			continue
		}

		textParts := make([]fantasy.MessagePart, 0, len(msg.Content))
		var mediaFiles []fantasy.FilePart

		for _, part := range msg.Content {
			toolResult, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				textParts = append(textParts, part)
				continue
			}

			if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](toolResult.Output); ok {
				if !supportsImages {
					// Model cannot process images. Replace with a text
					// placeholder and skip creating a synthetic user
					// message with FilePart, which would brick the
					// session on text-only models.
					textParts = append(textParts, fantasy.ToolResultPart{
						ToolCallID: toolResult.ToolCallID,
						Output: fantasy.ToolResultOutputContentText{
							Text: "[Image/media content not supported by this model]",
						},
						ProviderOptions: toolResult.ProviderOptions,
					})
					continue
				}

				decoded, err := base64.StdEncoding.DecodeString(media.Data)
				if err != nil {
					slog.Warn("Failed to decode media data", "error", err)
					textParts = append(textParts, part)
					continue
				}

				mediaFiles = append(mediaFiles, fantasy.FilePart{
					Data:      decoded,
					MediaType: media.MediaType,
					Filename:  fmt.Sprintf("tool-result-%s", toolResult.ToolCallID),
				})

				textParts = append(textParts, fantasy.ToolResultPart{
					ToolCallID: toolResult.ToolCallID,
					Output: fantasy.ToolResultOutputContentText{
						Text: "[Image/media content loaded - see attached file]",
					},
					ProviderOptions: toolResult.ProviderOptions,
				})
			} else {
				textParts = append(textParts, part)
			}
		}

		convertedMessages = append(convertedMessages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: textParts,
		})

		if len(mediaFiles) > 0 {
			convertedMessages = append(convertedMessages, fantasy.NewUserMessage(
				"Here is the media content from the tool result:",
				mediaFiles...,
			))
		}
	}

	return convertedMessages
}

// buildSummaryPrompt constructs the prompt text for session summarization.
func buildSummaryPrompt(todos []session.Todo) string {
	var sb strings.Builder
	sb.WriteString("Provide a detailed summary of our conversation above.")
	if len(todos) > 0 {
		sb.WriteString("\n\n## Current Todo List\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\nInclude these tasks and their statuses in your summary. ")
		sb.WriteString("Instruct the resuming assistant to use the `todos` tool to continue tracking progress on these tasks.")
	}
	return sb.String()
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
