package agent

import (
	"sync"
	"time"

	"github.com/aymanbagabas/go-udiff"
)

// A steady system prompt (owner, 2026-11-14): an API chat keeps sending
// the system prompt its first turn sent. Claude 5 binds each thinking
// block to the system prompt it was written under, so every edit (an
// add-on server that connected mid-chat, a skill turned on or off, the
// memory note, a new day) cost the model its earlier thinking and, before
// drop_block, failed the request. What changed is told to the model in a
// note ahead of the next prompt instead, stored with that prompt so later
// requests replay it unchanged. Kept in memory only: after a restart each
// chat starts a new steady prompt on its next turn.

// steadyPromptMaxChange is the largest change sent as a note. A bigger one
// (another agent's prompt, an edited prompt template) is sent as the new
// system prompt instead, which costs the earlier thinking once.
const steadyPromptMaxChange = 24000

// steadyPromptChats is how many chats keep a steady prompt; the least
// recently used one is forgotten past it.
const steadyPromptChats = 64

// steadyPrompts holds each chat's steady system prompt. The zero value is
// ready to use.
type steadyPrompts struct {
	mu    sync.Mutex
	chats map[string]*steadyPrompt
}

type steadyPrompt struct {
	// summaryID is the chat's summary when the prompt was frozen: a
	// compaction restarts the history, and with it the steady prompt.
	summaryID string
	// sent is the system prompt every request of the chat sends.
	sent string
	// known is the system prompt the model has been told about: sent plus
	// every change noted since.
	known string
	used  time.Time
}

// settle returns the system prompt a chat's turn sends, given the one the
// turn built. When the built one differs from what the model knows, it
// also returns a note telling the model what changed, and a commit to call
// once the note is stored with the turn's prompt.
func (s *steadyPrompts) settle(sessionID, summaryID string, hasHistory bool, built string) (send, note string, commit func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chats == nil {
		s.chats = make(map[string]*steadyPrompt)
	}
	chat, ok := s.chats[sessionID]
	if ok && chat.summaryID == summaryID && built == chat.known {
		chat.used = time.Now()
		return chat.sent, "", nil
	}
	if ok && chat.summaryID == summaryID {
		if change := promptChange(chat.known, built); len(change) <= steadyPromptMaxChange {
			chat.used = time.Now()
			return chat.sent, change, func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.chats[sessionID] == chat {
					chat.known = built
				}
			}
		}
	}
	// A new chat, a compaction, a restart or a change too big to note: the
	// built prompt becomes the steady one. Updates noted before it in the
	// same history no longer apply.
	if hasHistory && (!ok || chat.summaryID == summaryID) {
		note = promptReplaced
	}
	s.reset(sessionID, summaryID, built)
	return built, note, nil
}

// reset makes built the chat's steady prompt.
func (s *steadyPrompts) reset(sessionID, summaryID, built string) {
	s.chats[sessionID] = &steadyPrompt{summaryID: summaryID, sent: built, known: built, used: time.Now()}
	s.forgetOldest()
}

// rebase makes built the chat's steady prompt from outside settle: the
// turn sends built after all, as when trimming dropped part of the history
// and with it updates the model was told.
func (s *steadyPrompts) rebase(sessionID, summaryID, built string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chats == nil {
		s.chats = make(map[string]*steadyPrompt)
	}
	s.reset(sessionID, summaryID, built)
}

// forgetOldest drops the least recently used chat past steadyPromptChats.
func (s *steadyPrompts) forgetOldest() {
	for len(s.chats) > steadyPromptChats {
		var oldest string
		var at time.Time
		for id, chat := range s.chats {
			if oldest == "" || chat.used.Before(at) {
				oldest, at = id, chat.used
			}
		}
		delete(s.chats, oldest)
	}
}

// promptChange is the note telling the model how its system prompt changed:
// a unified diff from what it knew to the prompt the turn built, with a few
// lines of context around each change. It is "" when nothing changed.
func promptChange(before, after string) string {
	diff := udiff.Unified("system prompt you have", "system prompt now", before, after)
	if diff == "" {
		return ""
	}
	return "<system_prompt_update>\nYour system prompt changed since it was sent. Treat the change below (a unified diff: - lines no longer apply, + lines are new) as part of your system prompt.\n" +
		diff + "</system_prompt_update>\n\n"
}

// promptReplaced is the note for a turn whose system prompt was sent anew:
// the updates noted earlier in the chat were about the old one.
const promptReplaced = "<system_prompt_update>\nYour system prompt was sent anew. Earlier system_prompt_update notes in this chat no longer apply; follow the current system prompt.\n</system_prompt_update>\n\n"

// storesModelContext reports whether the call's stored prompt keeps the
// context sent ahead of it (createUserMessage): a subagent report doesn't.
func storesModelContext(call SessionAgentCall) bool {
	if len(call.batch) > 0 {
		return storesModelContext(call.batch[0])
	}
	return call.Report == nil
}
