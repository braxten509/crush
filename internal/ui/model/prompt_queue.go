package model

import (
	"context"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/google/uuid"
)

// promptQueue tracks the prompts sent but not answered yet: the session's
// queue behind the running turn, mirrored from the workspace, and the
// prompts on their way into it.
type promptQueue struct {
	// count, items and entries mirror the session's queued prompts. They
	// are event-driven with a TTL backstop, fetched off-thread by
	// dispatchPromptQueueRefresh (see workspace_cache.go); count is always
	// len(items).
	count     int
	items     []string
	entries   []message.QueuedPromptSummary
	checkedAt time.Time
	inFlight  bool
	// gen is bumped by every queue state transition; an in-flight fetch
	// captures it at dispatch and its result is discarded if the
	// generation has moved on (see workspace_cache.go).
	gen            uint64
	recallInFlight bool
	// recalledDrafts keeps recalled drafts scoped to their chat if a recall
	// races a switch.
	recalledDrafts map[string][]message.QueuedPrompt
	// pending are prompts sent until their saved copy arrives.
	pending []message.Message
	// submitting are the submissions in flight, by ID.
	submitting        map[string]promptSubmission
	interruptInFlight map[string]bool
	confirmedCopies   map[string]message.Message
	failedRecalls     map[string]bool
	// canceled are the messages an interrupt cancelled.
	canceled map[string]struct{}
	// held are pending prompts sent mid-turn. They wait in the queue list
	// and join the chat only when their turn starts.
	held map[string]bool
}

// promptSubmission is a prompt being handed to the agent.
type promptSubmission struct {
	sessionID string
	cancel    context.CancelFunc
	queued    bool
}

// invalidate bumps the generation so any in-flight queue fetch result is
// discarded when it lands (and re-fetched) instead of overwriting newer
// optimistic or cleared queue state.
func (q *promptQueue) invalidate() {
	q.gen++
}

// clear empties the mirrored queue, as of now.
func (q *promptQueue) clear() {
	q.count = 0
	q.items = nil
	q.checkedAt = time.Now()
}

// submittingFor reports whether a prompt for sessionID is being submitted.
func (q *promptQueue) submittingFor(sessionID string) bool {
	for _, submission := range q.submitting {
		if submission.sessionID == sessionID {
			return true
		}
	}
	return false
}

// track records a submission in flight and returns its ID, a new one when
// id is empty.
func (q *promptQueue) track(id, sessionID string, cancel context.CancelFunc, queued bool) string {
	if id == "" {
		id = uuid.NewString()
	}
	if q.submitting == nil {
		q.submitting = make(map[string]promptSubmission)
	}
	q.submitting[id] = promptSubmission{sessionID: sessionID, cancel: cancel, queued: queued}
	return id
}

// take removes the pending prompt id and returns it, or nil.
func (q *promptQueue) take(id string) *message.Message {
	if id == "" {
		return nil
	}
	i := slices.IndexFunc(q.pending, func(p message.Message) bool { return p.ID == id })
	if i < 0 {
		return nil
	}
	pending := q.pending[i]
	q.pending = slices.Delete(q.pending, i, i+1)
	delete(q.held, id)
	return &pending
}

// hold marks the pending prompt id as held.
func (q *promptQueue) hold(id string) {
	if q.held == nil {
		q.held = make(map[string]bool)
	}
	q.held[id] = true
}

// recallDraft keeps a draft to put back into sessionID's editor.
func (q *promptQueue) recallDraft(sessionID string, draft message.QueuedPrompt) {
	if q.recalledDrafts == nil {
		q.recalledDrafts = make(map[string][]message.QueuedPrompt)
	}
	q.recalledDrafts[sessionID] = append(q.recalledDrafts[sessionID], draft)
}

// unlistedHeld are sessionID's held prompts the agent's queue does not list
// yet, so a prompt shows in the queue list from the moment it is sent.
func (q *promptQueue) unlistedHeld(sessionID string) []string {
	var result []string
	for _, pending := range q.pending {
		if pending.SessionID != sessionID || !q.held[pending.ID] {
			continue
		}
		if !slices.ContainsFunc(q.entries, func(e message.QueuedPromptSummary) bool { return e.SubmissionID == pending.ID }) {
			result = append(result, pending.Content().Text)
		}
	}
	return result
}
