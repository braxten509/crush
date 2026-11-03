package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/session"
)

// ErrChatLocked refuses a prompt to a locked chat (owner, 2026-11-03).
var ErrChatLocked = session.ErrChatLocked

// chatHidden reports whether a chat, or the chat a sub-agent's chat
// belongs to, is locked or on its way in or out of the vault.
func chatHidden(ctx context.Context, sessions session.Service, sessionID string) bool {
	if sessions == nil {
		return false
	}
	id := sessionID
	for range 16 {
		s, err := sessions.Get(ctx, id)
		if err != nil {
			return false
		}
		if session.Hidden(s.LockState) {
			return true
		}
		if s.ParentSessionID == "" {
			return false
		}
		id = s.ParentSessionID
	}
	return false
}

// WithIdleChat runs fn while none of the chats (a chat and its sub-agent
// chats) has a reply, queued prompt, sub-agent or background job running,
// holding their run locks so no prompt starts meanwhile. It stops the
// Claude and Codex processes Crush keeps between turns first, so their
// copies are complete. Locking a chat marks it under this gate (PLAN §2).
func (c *coordinator) WithIdleChat(ctx context.Context, ids []string, fn func() error) error {
	a, ok := c.currentAgent().(*sessionAgent)
	if !ok {
		return fmt.Errorf("locking is unavailable for this agent")
	}
	for _, id := range ids {
		mu := a.sessionMu(id)
		mu.Lock()
		defer mu.Unlock()
	}
	for _, id := range ids {
		accepted, _ := a.acceptedRuns.Get(id)
		_, summarizing := a.activeRequests.Get(id + "-summarize")
		if accepted > 0 || summarizing || a.IsSessionBusy(id) || a.QueuedPrompts(id) > 0 {
			return treeBusyError{"wait for the reply and queued prompts to finish before locking the chat"}
		}
		for _, task := range SessionTasks(id) {
			if task.Status == TaskRunning || !task.Delivered {
				return treeBusyError{"wait for sub-agents and their results to finish before locking the chat"}
			}
		}
		if err := SessionBackgroundBlock(id); err != nil {
			return treeBusyError{err.Error()}
		}
	}
	for _, id := range ids {
		if err := cliagent.CloseForLock(id); err != nil {
			return treeBusyError{err.Error()}
		}
	}
	return fn()
}

// IdleChatGate is the coordinator's WithIdleChat, for callers holding a
// Coordinator.
type IdleChatGate interface {
	WithIdleChat(ctx context.Context, ids []string, fn func() error) error
}

// StopForLock cancels accepted, queued and active work and waits for the
// running turn's final writes. Shutdown must not use a short busy timeout.
func (c *coordinator) StopForLock(ctx context.Context, ids []string) error {
	a, ok := c.currentAgent().(*sessionAgent)
	if !ok {
		return fmt.Errorf("cannot drain this agent for locking")
	}
	for _, id := range ids {
		c.Cancel(id)
		c.ClearQueue(id)
	}
	for {
		busy := false
		for _, id := range ids {
			accepted, _ := a.acceptedRuns.Get(id)
			_, summarizing := a.activeRequests.Get(id + "-summarize")
			if accepted > 0 || summarizing || a.IsSessionBusy(id) {
				busy = true
				break
			}
		}
		if !busy {
			for _, id := range ids {
				if err := cliagent.CloseForLock(id); err != nil {
					return err
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

type LockShutdownGate interface {
	StopForLock(context.Context, []string) error
}
