package agent

// Pending work that outlives an engine restart. A project's data directory
// keeps the waits planned for its chats, the prompts waiting behind a chat's
// turn, and the fallback order's memory of accounts that ran out. An engine
// exits when its last app leaves and a new one starts the next time an app
// joins, so without this file a planned "continue" would vanish and the
// chat would sit there for good.
//
// The file follows tasks.json and background-jobs.json: an entry names the
// process that owns it, so only work whose Crush is gone comes back, and
// every change holds a lock shared by all Crush processes of the project.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/charmbracelet/crush/internal/message"
)

// savedPendingFile holds a project's pending work.
const savedPendingFile = "pending-work.json"

// What a saved wait continues.
const (
	// waitKindLimit is the plain retry after a usage limit.
	waitKindLimit = "limit"
	// waitKindFallback is a chat waiting for an account to refill, which
	// the fallback order moves along when the time comes.
	waitKindFallback = "fallback"
	// waitKindContinue is a chat continued a moment from now, after the
	// order moved it or spent a refill.
	waitKindContinue = "continue"
)

// maxSavedAttachments is the most attachment content saved with one queued
// prompt. A bigger prompt is not saved, since dropping its picture would run
// it without it.
const maxSavedAttachments = 4 << 20

// savedWait is a chat's planned continuation.
type savedWait struct {
	SessionID string `json:"session_id"`
	At        time.Time
	Kind      string `json:"kind"`
	// Provider, Model and Account are the block a fallback wait moves the
	// chat to; empty for the other kinds.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Account  string `json:"account,omitempty"`
	// Owner is the process ID of the Crush that planned the wait.
	Owner int `json:"owner"`
}

// savedOut is an account the fallback order counted as out.
type savedOut struct {
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Until time.Time `json:"until"`
	// Known is false when the provider named no reset time.
	Known bool `json:"known,omitempty"`
}

// savedReturn is the fallback order's planned way back to a block.
type savedReturn struct {
	Block int       `json:"block"`
	At    time.Time `json:"at"`
}

// savedPrompt is a prompt waiting behind a chat's turn.
type savedPrompt struct {
	SessionID   string               `json:"session_id"`
	Prompt      string               `json:"prompt"`
	Attachments []message.Attachment `json:"attachments,omitempty"`
	// Owner is the process ID of the Crush whose agent queue holds the
	// prompt; 0 once a shutdown released it for the next resume.
	Owner int `json:"owner"`
	// Saved is when the entry was last written; an old one is not run.
	Saved time.Time `json:"saved,omitzero"`
}

// pendingWork is what a project's data directory keeps across a restart.
type pendingWork struct {
	Waits []savedWait   `json:"waits,omitempty"`
	Queue []savedPrompt `json:"queue,omitempty"`
	Out   []savedOut    `json:"out,omitempty"`
	// Returns is each chat's planned way back, by chat. An older Crush's
	// single "return" named no chat and is not read.
	Returns map[string]savedReturn `json:"returns,omitempty"`
}

// clock is the coordinator's wall clock, the one the fallback order and the
// waits use.
func (c *coordinator) clock() time.Time {
	return c.fallback.clock()
}

// savedDataPath is where the named file of pending work lives, or "" when
// this Crush doesn't keep one: a non-interactive run ends with its work.
func (c *coordinator) savedDataPath(name string) string {
	if c == nil || c.cfg == nil || !c.interactive {
		return ""
	}
	dir := c.cfg.Config().Options.DataDirectory
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// updatePending changes the project's pending work. It does nothing when
// this Crush keeps none.
func (c *coordinator) updatePending(edit func(*pendingWork) bool) error {
	path := c.savedDataPath(savedPendingFile)
	if path == "" {
		return nil
	}
	return jsonfile.Update(path, edit)
}

// planWait plans the chat's continuation at w.At and saves it, so a restart
// in between still continues the chat. The wait drops its saved entry when
// it fires.
func (c *coordinator) planWait(w savedWait, send func()) {
	w.Owner = os.Getpid()
	if err := c.updatePending(func(p *pendingWork) bool {
		p.Waits = append(slices.DeleteFunc(p.Waits, sameWaitSession(w.SessionID)), w)
		return true
	}); err != nil {
		slog.Warn("Failed to save a planned wait", "session_id", w.SessionID, "error", err)
	}
	c.limitRetries.plan(w.SessionID, w.At, func() {
		if c.tasks != nil && c.tasks.isClosing() {
			return // stays saved: the next engine continues it
		}
		c.clearWait(w.SessionID)
		send()
	})
}

func sameWaitSession(sessionID string) func(savedWait) bool {
	return func(s savedWait) bool { return s.SessionID == sessionID }
}

// clearWait drops the chat's saved wait, if it has one.
func (c *coordinator) clearWait(sessionID string) {
	err := c.updatePending(func(p *pendingWork) bool {
		left := slices.DeleteFunc(p.Waits, sameWaitSession(sessionID))
		changed := len(left) != len(p.Waits)
		p.Waits = left
		return changed
	})
	if err != nil {
		slog.Warn("Failed to drop a planned wait", "session_id", sessionID, "error", err)
	}
}

// restoreWaits continues the chats an earlier Crush was waiting on: a time
// still ahead is waited for, one that passed while Crush was closed is sent
// at once.
func (c *coordinator) restoreWaits() {
	if !c.interactive {
		return
	}
	claimed, err := c.claimWaits()
	if err != nil {
		slog.Warn("Failed to read the waits of an earlier Crush", "error", err)
	}
	for _, w := range claimed {
		slog.Info("Restored a planned wait", "session_id", w.SessionID, "at", w.At, "kind", w.Kind)
		c.planWait(w, c.waitSender(w))
	}
}

// claimWaits takes the saved waits this Crush should continue: those whose
// Crush is gone, and those this coordinator does not plan itself. It drops
// the ones too old or whose chat is gone.
func (c *coordinator) claimWaits() ([]savedWait, error) {
	self := os.Getpid()
	now := c.clock()
	var claimed []savedWait
	err := c.updatePending(func(p *pendingWork) bool {
		kept := p.Waits[:0]
		for _, w := range p.Waits {
			switch {
			case c.limitRetries.has(w.SessionID):
				kept = append(kept, w) // this Crush still waits on it
			case w.Owner != self && taskProcessAlive(w.Owner):
				kept = append(kept, w) // another running Crush owns it
			case now.Sub(w.At) > resumeWindow:
				slog.Info("Not continuing a chat for a wait that ended long ago", "session_id", w.SessionID, "at", w.At)
			case !c.sessionExists(w.SessionID):
				slog.Info("Dropping a wait whose chat is gone", "session_id", w.SessionID)
			default:
				w.Owner = self
				claimed = append(claimed, w)
				kept = append(kept, w)
			}
		}
		p.Waits = kept
		return true
	})
	return claimed, err
}

// sessionExists reports whether the chat a saved entry names is still here.
func (c *coordinator) sessionExists(sessionID string) bool {
	if c.sessions == nil {
		return true
	}
	_, err := c.sessions.Get(context.Background(), sessionID)
	return err == nil
}

// waitSender is what the saved wait w does when its time comes.
func (c *coordinator) waitSender(w savedWait) func() {
	if w.Kind == waitKindFallback {
		return func() { c.continueFallbackWait(w) }
	}
	return func() { c.sendContinue(w.SessionID) }
}

// continueFallbackWait moves a chat whose wait ended to the block it waited
// for, deciding again first when that account still has no room.
func (c *coordinator) continueFallbackWait(w savedWait) {
	ctx := context.Background()
	if order, at, block, ok := c.waitBlock(w); ok {
		c.waitForRoom(ctx, w.SessionID, order, at, block)
		return
	}
	// The order changed since: continue on the model it waited for, or
	// where the chat is if that is gone too.
	if w.Provider != "" {
		m := config.SelectedModel{Provider: w.Provider, Model: w.Model, Account: w.Account}
		if err := c.switchModel(ctx, w.SessionID, m); err != nil {
			slog.Warn("The fallback order could not switch the model a wait named", "error", err)
		}
	}
	c.sendContinue(w.SessionID)
}

// waitBlock finds the block a saved wait named in the chat's own order.
func (c *coordinator) waitBlock(w savedWait) (config.FallbackOrder, int, config.FallbackBlock, bool) {
	cur, ok := c.chatSelection(context.Background(), w.SessionID)
	if !ok {
		return config.FallbackOrder{}, 0, config.FallbackBlock{}, false
	}
	order, ok := c.fallbackOrder(cur)
	if !ok {
		return config.FallbackOrder{}, 0, config.FallbackBlock{}, false
	}
	for i, b := range order.Blocks {
		if b.Type == config.FallbackUse && b.Provider == w.Provider && b.Model == w.Model && b.Account == w.Account {
			return order, i, b, true
		}
	}
	return config.FallbackOrder{}, 0, config.FallbackBlock{}, false
}

// saveFallbackMemory saves the accounts the order counted as out and the
// planned way back to one that refills.
func (c *coordinator) saveFallbackMemory() {
	c.fallback.mu.Lock()
	out := make([]savedOut, 0, len(c.fallback.out))
	for a, o := range c.fallback.out {
		out = append(out, savedOut{Kind: string(a.Kind), ID: a.ID, Until: o.until, Known: o.known})
	}
	var ret map[string]savedReturn
	for sessionID, r := range c.fallback.ret {
		if ret == nil {
			ret = map[string]savedReturn{}
		}
		ret[sessionID] = savedReturn{Block: r.Block, At: r.At}
	}
	c.fallback.mu.Unlock()
	slices.SortFunc(out, func(a, b savedOut) int {
		return strings.Compare(string(a.Kind)+"\x00"+a.ID, string(b.Kind)+"\x00"+b.ID)
	})
	err := c.updatePending(func(p *pendingWork) bool {
		p.Out, p.Returns = out, ret
		return true
	})
	if err != nil {
		slog.Warn("Failed to save the fallback order's memory", "error", err)
	}
}

// loadFallbackMemory brings back the accounts an earlier Crush counted as
// out, and the planned way back, so the order doesn't try a dead account
// once after a restart.
func (c *coordinator) loadFallbackMemory() {
	path := c.savedDataPath(savedPendingFile)
	if path == "" {
		return
	}
	saved := jsonfile.Read[pendingWork](path)
	now := c.clock()
	c.fallback.mu.Lock()
	defer c.fallback.mu.Unlock()
	for _, o := range saved.Out {
		if !o.Until.After(now) {
			continue // its limit has reset since
		}
		if c.fallback.out == nil {
			c.fallback.out = map[fallback.Account]fallbackOut{}
		}
		c.fallback.out[fallback.Account{Kind: catwalk.Type(o.Kind), ID: o.ID}] = fallbackOut{until: o.Until, known: o.Known}
	}
	for sessionID, r := range saved.Returns {
		if c.fallback.ret == nil {
			c.fallback.ret = map[string]*fallback.Return{}
		}
		if _, ok := c.fallback.ret[sessionID]; !ok {
			c.fallback.ret[sessionID] = &fallback.Return{Block: r.Block, At: r.At}
		}
	}
}

// restorePrompts runs the prompts an earlier Crush left queued behind a
// chat's turn, in the order they were typed.
func (c *coordinator) restorePrompts() {
	if !c.interactive {
		return
	}
	claimed, err := c.claimPrompts()
	if err != nil {
		slog.Warn("Failed to read the queued prompts of an earlier Crush", "error", err)
	}
	for sessionID, prompts := range queuedBySession(claimed) {
		go func() {
			for _, p := range prompts {
				slog.Info("Running a prompt an earlier Crush left queued", "session_id", sessionID)
				if _, err := c.Run(context.Background(), sessionID, p.Prompt, p.Attachments...); err != nil && !errors.Is(err, context.Canceled) {
					slog.Warn("A restored queued prompt failed", "session_id", sessionID, "error", err)
				}
			}
		}()
	}
}

// queuedBySession groups saved prompts by chat, keeping each chat's order.
func queuedBySession(prompts []savedPrompt) map[string][]savedPrompt {
	grouped := make(map[string][]savedPrompt, len(prompts))
	for _, p := range prompts {
		grouped[p.SessionID] = append(grouped[p.SessionID], p)
	}
	return grouped
}

// claimPrompts takes the saved prompts whose Crush is gone or that a
// shutdown released (owner 0), and drops the ones too old or whose chat is
// gone. They are dropped from the file as they are taken, so only one Crush
// runs them. Prompts of a Crush that still runs, this one included, stay:
// its agent queue already holds them, so running them here would repeat
// them.
func (c *coordinator) claimPrompts() ([]savedPrompt, error) {
	self := os.Getpid()
	now := c.clock()
	var claimed []savedPrompt
	err := c.updatePending(func(p *pendingWork) bool {
		kept := p.Queue[:0]
		for _, q := range p.Queue {
			switch {
			case q.Owner != 0 && (q.Owner == self || taskProcessAlive(q.Owner)):
				kept = append(kept, q)
			case !q.Saved.IsZero() && now.Sub(q.Saved) > resumeWindow:
				slog.Info("Not running a prompt queued long ago", "session_id", q.SessionID, "saved", q.Saved)
			case !c.sessionExists(q.SessionID):
				slog.Info("Dropping a queued prompt whose chat is gone", "session_id", q.SessionID)
			default:
				claimed = append(claimed, q)
			}
		}
		p.Queue = kept
		return true
	})
	return claimed, err
}

// queueWatchEvery is how often the hub looks for prompts waiting behind a
// chat's turn. The queue lives in the agent's memory, so it is read rather
// than reported: a change never waits on the disk while holding a dispatch
// lock.
var queueWatchEvery = 2 * time.Second

// watchQueues saves the prompts waiting behind a chat's turn until the hub
// is torn down.
func (h *taskHub) watchQueues() {
	ticker := time.NewTicker(queueWatchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-h.queueStop:
			return
		case <-ticker.C:
			h.writeQueues(false)
		}
	}
}

// queuedPrompts is every chat's prompts waiting behind its turn, as the
// agents of this coordinator see them now.
func (c *coordinator) queuedPrompts() map[string][]message.QueuedPrompt {
	c.agentMu.RLock()
	agents := make([]SessionAgent, 0, len(c.agents))
	for _, a := range c.agents {
		agents = append(agents, a)
	}
	c.agentMu.RUnlock()
	out := map[string][]message.QueuedPrompt{}
	for _, a := range agents {
		reader, ok := a.(interface {
			queuedPromptSnapshots() map[string][]message.QueuedPrompt
		})
		if !ok {
			continue
		}
		for sessionID, prompts := range reader.queuedPromptSnapshots() {
			if len(prompts) > 0 {
				out[sessionID] = prompts
			}
		}
	}
	return out
}

// writeQueues saves every chat's queued prompts, replacing this Crush's own
// entries and leaving other Crush processes' alone. Nothing is written when
// the queue is as it was, nor once the hub closes: release is the shutdown's
// last write, which saves the queue with no owner so the next resume, in
// this process or the next Crush, runs it.
func (h *taskHub) writeQueues(release bool) {
	if h.c.savedDataPath(savedPendingFile) == "" || !release && h.isClosing() {
		return
	}
	self := os.Getpid()
	owner := self
	if release {
		owner = 0
	}
	entries := make([]savedPrompt, 0)
	for sessionID, prompts := range h.c.queuedPrompts() {
		for _, p := range prompts {
			var size int
			for _, a := range p.Attachments {
				size += len(a.Content)
			}
			if size > maxSavedAttachments {
				slog.Warn("Not saving a queued prompt whose files are too big", "session_id", sessionID, "bytes", size)
				continue
			}
			entries = append(entries, savedPrompt{SessionID: sessionID, Prompt: p.Prompt, Attachments: p.Attachments, Owner: owner})
		}
	}
	slices.SortStableFunc(entries, func(a, b savedPrompt) int {
		return strings.Compare(a.SessionID, b.SessionID)
	})
	encoded, err := json.Marshal(entries)
	if err != nil {
		return
	}
	h.queueMu.Lock()
	unchanged := h.queueSaved != "" && h.queueSaved == string(encoded)
	h.queueMu.Unlock()
	if unchanged && !release {
		return
	}
	err = h.c.updatePending(func(p *pendingWork) bool {
		kept := slices.DeleteFunc(p.Queue, func(s savedPrompt) bool { return s.Owner == self })
		now := h.c.clock()
		for i := range entries {
			entries[i].Saved = now
		}
		p.Queue = append(kept, entries...)
		return true
	})
	if err != nil {
		slog.Warn("Failed to save queued prompts", "error", err)
		return
	}
	h.queueMu.Lock()
	h.queueSaved = string(encoded)
	h.queueMu.Unlock()
}
