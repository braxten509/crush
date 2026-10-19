// The 60-second hint (desktop-design-5-6 2.15; LIVE-004): "ctrl+b to run in
// background" under a working tool group whose running bash command LIVE first
// saw 60 s ago or more, while the agent can move commands to the background
// (MOD's capabilities.can_background) and Ctrl+B was not pressed for that
// command. A port of internal/ui/chat/toolgroup.go:190-226, :421-423: the
// command is the group's last step, a bash call still running; the time
// starts at its first sight, as the terminal's runningSince does. ToolGroup
// (P3.6, S5's line) asks toolHint(item) through lib/toolhints.js. Only the
// shown chat's own groups get it: Ctrl+B moves that chat's command, never one
// in a helper's chat (the helper view's groups never offer it either,
// internal/ui/model/subagent_view.go:59).
import { SvelteSet } from "svelte/reactivity";
import { capabilities } from "../models/capabilities.svelte.js";
import { chatOf } from "../stores/messages.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";

export const HINT_TEXT = "ctrl+b to run in background";
export const HINT_AFTER_MS = 60_000; // backgroundHintAfter (toolgroup.go:75)

const firstSeen = new Map(); // call id → {messageId, at}: running bash commands seen in a group
const byGroup = new Map(); // group id → the call id it waits on
const due = new SvelteSet(); // call ids that have run HINT_AFTER_MS since their first sight
const pressed = new SvelteSet(); // call ids Ctrl+B moved (Backgrounded, toolgroup.go:222-226)

// The bash call the group waits on, or null (runningCommand, toolgroup.go:190-204):
// the group's last step is a bash call that still runs. ToolGroup asks only
// while the group works, which is the terminal's live group of a busy chat.
export function runningCommand(item) {
  const calls = item?.calls ?? [];
  const last = calls[calls.length - 1];
  if (!last || last.name !== "bash" || last.status !== "running") return null;
  const members = item.members ?? [];
  return members.length === 0 || members[members.length - 1] === last.id ? last : null;
}

// toolHint's function: the hint's text for a working group, else null.
export function hintFor(item, now = Date.now()) {
  const call = runningCommand(item);
  const before = byGroup.get(item?.groupId);
  if (before && before !== call?.id) {
    // The group moved on: its old command no longer runs.
    byGroup.delete(item.groupId);
    firstSeen.delete(before);
  }
  if (!call?.id || !sessions.currentId || chatOf(call.messageId) !== sessions.currentId) return null;
  if (!firstSeen.has(call.id)) {
    firstSeen.set(call.id, { messageId: call.messageId ?? "", at: now });
    if (item?.groupId) byGroup.set(item.groupId, call.id);
    setTimeout(() => {
      if (firstSeen.has(call.id)) due.add(call.id);
    }, HINT_AFTER_MS);
  }
  // Read every set so a change of any of them draws the group again.
  const ready = due.has(call.id);
  const moved = pressed.has(call.id);
  const can = capabilities.can_background === true;
  return can && ready && !moved ? HINT_TEXT : null;
}

// The commands this window saw running in groups of the given messages'
// chat: belongs(messageId) says whether a message is the shown chat's.
export function runningCalls(belongs) {
  const out = [];
  for (const [id, seen] of firstSeen) if (belongs(seen.messageId)) out.push(id);
  return out;
}

// Ctrl+B moved the command (moved: true): its hint goes at once (2.15).
export function markBackgrounded(ids) {
  for (const id of ids) pressed.add(id);
}

// For the probe: the commands whose hint shows now.
export function hintedCalls() {
  const can = capabilities.can_background === true;
  return can ? [...due].filter((id) => !pressed.has(id) && firstSeen.has(id)) : [];
}
