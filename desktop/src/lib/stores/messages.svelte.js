// Each loaded chat's messages (desktop-design 1.5.3), this window's prompts on
// their way (TYPE-042 to TYPE-049) and its interrupts (CHAT-026). Messages are
// kept exactly as the engine sent them; what an interrupt changes on screen is
// a copy (withInterrupt), so the stored object stays the event's.
import { SvelteMap, SvelteSet } from "svelte/reactivity";
import { api, onInterrupt, onSend, uuid } from "../engine/api.js";
import { onEvent, onFolderEvent, onResync } from "../engine/events.js";
import { prompts } from "./prompts.svelte.js";
import { queue } from "./queue.svelte.js";
import { noteChatMessage, sessions, settleBusy } from "./sessions.svelte.js";
import { notify } from "./ui.svelte.js";

// One per loaded chat; a class instance, so no store proxy wraps it.
export class ChatMessages {
  status = $state("idle"); // "idle" | "loading" | "ready" | "error"
  error = $state(null);
  ids = $state.raw([]); // message ids in chat order; replaced as a whole, never pushed into
  byId = new SvelteMap(); // mid → Message exactly as sent; replaced by key, never changed in place
  // Assistant messages a live event showed still being written (busy (c),
  // 1.5.3). A load starts it empty: an unfinished message in the snapshot
  // counts only once it gets an update, so a run killed mid-reply leaves no
  // ghost "working" state (internal/ui/model/session_load.go:498-506).
  active = $state.raw(new Set());
  // Older messages the engine still has: a chat opens with its newest
  // PAGE (owner, 2026-10-21: an 18,000-message chat took 10 s to open) and
  // reads older pages as the view nears its top (loadOlder).
  more = $state(false);
  older = $state(false); // an older page is being read
}

// About how many messages a chat opens with, and each older page.
export const PAGE = 300;

export const messages = $state({
  bySession: {}, // sid → ChatMessages (agent tools' chats "<mid>$$<tool call id>" too, CHAT-078)
  pending: {}, // submission id → this window's prompt until its saved copy arrives (TYPE-042)
  returned: {}, // sid → [{text, attachments}]: prompts given back to the field (TYPE-048, TYPE-049)
});

// Message ids this window interrupted (CHAT-026; the terminal's queue.canceled).
export const interrupted = new SvelteSet();

// -- Message parts (wire.js Part) --

const partsOf = (m, type) => (m?.parts ?? []).filter((p) => p?.type === type);
export const isFinished = (m) => (m?.parts ?? []).some((p) => p?.type === "finish");
const toolCalls = (m) => partsOf(m, "tool_call").map((p) => p.data ?? {});
const toolResults = (m) => partsOf(m, "tool_result").map((p) => p.data ?? {});
// Message.Content() is the first text part (internal/proto/message.go).
const submissionOf = (m) => (m?.role === "user" ? (partsOf(m, "text")[0]?.data?.submission_id ?? "") : "");

// The tools whose calls run in a chat of their own (internal/agent/agent_tool.go:23,
// internal/agent/tools/web_types.go:4), named "<message id>$$<tool call id>"
// (internal/session/session.go:544-556).
const AGENT_TOOLS = new Set(["agent", "agentic_fetch"]);
export const agentChatId = (messageId, toolCallId) => `${messageId}$$${toolCallId}`;
export function parseAgentChatId(sid) {
  const parts = String(sid ?? "").split("$$");
  return parts.length === 2 ? { messageId: parts[0], toolCallId: parts[1] } : null;
}

// The top-level chat an agent tool's chat belongs to (its message's chat, up
// through nested agents); a top-level chat is its own.
export function rootChatOf(sid) {
  let id = sid;
  for (let depth = 0; depth < 8; depth++) {
    const agent = parseAgentChatId(id);
    if (!agent) return id;
    const owner = Object.keys(messages.bySession).find((other) => messages.bySession[other]?.byId?.has(agent.messageId));
    if (!owner) return "";
    id = owner;
  }
  return "";
}

function chatFor(sid) {
  let chat = messages.bySession[sid];
  if (!chat) {
    chat = new ChatMessages();
    messages.bySession[sid] = chat;
  }
  return chat;
}

// The loaded chat that holds message mid, or "".
export function chatOf(mid) {
  for (const [sid, chat] of Object.entries(messages.bySession)) {
    if (chat.status === "ready" && chat.byId.has(mid)) return sid;
  }
  return "";
}

// -- Keeping one chat --

function setActive(chat, m) {
  const on = m.role === "assistant" && !isFinished(m) && !interrupted.has(m.id);
  if (on === chat.active.has(m.id)) return;
  const next = new Set(chat.active);
  if (on) next.add(m.id);
  else next.delete(m.id);
  chat.active = next;
}

// New ids go in by created_at, searching from the end (most land last).
function insertId(chat, m) {
  const ids = chat.ids;
  let i = ids.length;
  const at = m.created_at ?? 0;
  while (i > 0 && (chat.byId.get(ids[i - 1])?.created_at ?? 0) > at) i -= 1;
  chat.ids = [...ids.slice(0, i), m.id, ...ids.slice(i)];
}

// An upsert by key: applying the same event twice gives the same state.
function put(chat, m, { live = true } = {}) {
  const known = chat.byId.has(m.id);
  chat.byId.set(m.id, m);
  if (!known) insertId(chat, m);
  if (live) setActive(chat, m);
  settle(m);
}

function drop(chat, mid) {
  if (!chat.byId.has(mid)) return;
  chat.byId.delete(mid);
  chat.ids = chat.ids.filter((id) => id !== mid);
  if (chat.active.has(mid)) {
    const next = new Set(chat.active);
    next.delete(mid);
    chat.active = next;
  }
}

// What a saved message settles: a tool result overrides its call's status
// (1.5.3), and the saved copy of this window's prompt replaces its pending
// item (TYPE-042; confirmPendingPrompt, internal/ui/model/pending_prompt.go:70-80).
function settle(m) {
  for (const result of toolResults(m)) {
    if (result.tool_call_id && result.tool_call_id in prompts.callStatus) delete prompts.callStatus[result.tool_call_id];
  }
  const submission = submissionOf(m);
  if (submission && messages.pending[submission]) delete messages.pending[submission];
}

// -- Loading (LIST-034, LIST-036) --

const loads = new Map(); // sid → {events, promise}: the newest load of that chat
// Agent chats' events that came before their parent message, kept while loads
// run: message id → its latest event, as a buffer keeps only the latest of a
// message's updates (buffer below), so streaming agents cannot grow it. A
// message keeps the place of its first event, so a parent's events stay
// ahead of its own agents'.
const heldAgentEvents = new Map();

// GET messages; buffers that chat's events meanwhile; replays them on the
// snapshot. A newer load of the same chat supersedes this one (it keeps the
// buffer), and this call then resolves to the newer one's result. A chat
// that is already shown keeps taking events while it reloads; they are
// replayed on the new snapshot too. Resolves true when the snapshot landed,
// false when the chat was dropped meanwhile; rejects when the read failed.
export function load(sid) {
  if (!sid) return Promise.resolve(false);
  const chat = chatFor(sid);
  const token = { events: loads.get(sid)?.events ?? [], promise: null };
  loads.set(sid, token);
  const reload = chat.status === "ready";
  if (!reload) chat.status = "loading";
  chat.error = null;
  token.promise = (async () => {
    let list;
    // An agent tool's chat is read whole: it is short, and its parent needs it all.
    const paged = !parseAgentChatId(sid);
    try {
      list = await api.messages(sid, paged ? { last: PAGE } : {});
    } catch (error) {
      if (loads.get(sid) !== token) return newestLoad(sid);
      loads.delete(sid);
      if (messages.bySession[sid] === chat && !reload) {
        chat.status = "error";
        chat.error = error;
      }
      retryHeldAgentEvents();
      throw error;
    }
    if (loads.get(sid) !== token) return newestLoad(sid);
    loads.delete(sid);
    if (messages.bySession[sid] !== chat) return false;
    applySnapshot(sid, chat, Array.isArray(list) ? list : [], token.events, paged);
    retryHeldAgentEvents();
    // A top-level chat loads its agents' chats, level by level; an agent's
    // own chat leaves the next level to that walk.
    if (!parseAgentChatId(sid)) loadAgentChats(sid);
    return true;
  })();
  return token.promise;
}

// Reads the page before the chat's oldest shown message and puts it ahead.
// Resolves true when older messages were added.
export async function loadOlder(sid) {
  const chat = messages.bySession[sid];
  if (!chat || chat.status !== "ready" || !chat.more || chat.older) return false;
  const oldest = chat.ids[0];
  if (!oldest) return false;
  chat.older = true;
  let list;
  try {
    list = await api.messages(sid, { last: PAGE, before: oldest });
  } catch (error) {
    console.error("older messages read failed", errorText(error));
    if (messages.bySession[sid] === chat) chat.older = false;
    return false;
  }
  if (messages.bySession[sid] !== chat || chat.ids[0] !== oldest) {
    chat.older = false;
    return false;
  }
  const added = (Array.isArray(list) ? list : []).filter((m) => m?.id && !chat.byId.has(m.id));
  for (const m of added) {
    chat.byId.set(m.id, m);
    settle(m);
  }
  chat.ids = [...added.map((m) => m.id), ...chat.ids];
  chat.more = (Array.isArray(list) ? list.length : 0) >= PAGE;
  chat.older = false;
  // The added page's agent tools' chats, which no walk has read yet.
  if (added.length) loadAgentChats(sid, { onlyNew: true });
  return added.length > 0;
}

function newestLoad(sid) {
  return loads.get(sid)?.promise ?? messages.bySession[sid]?.status === "ready";
}

function applySnapshot(sid, chat, list, events, paged = false) {
  const snapshot = new Map();
  for (const m of list) if (m?.id) snapshot.set(m.id, m);
  // A reload of a paged chat keeps the older pages already read: the
  // messages before the snapshot's first one stay ahead of it.
  const first = list.find((m) => m?.id);
  const kept = [];
  if (paged && first && chat.status === "ready") {
    for (const id of chat.ids) {
      const m = chat.byId.get(id);
      if (!m || snapshot.has(id)) continue;
      if ((m.created_at ?? 0) < (first.created_at ?? 0)) kept.push(m);
    }
  }
  chat.byId.clear();
  for (const m of kept) chat.byId.set(m.id, m);
  for (const m of snapshot.values()) chat.byId.set(m.id, m);
  chat.ids = [...kept.map((m) => m.id), ...snapshot.keys()];
  // A full page says older ones may be left; kept pages keep what they knew.
  if (!paged) chat.more = false;
  else if (!kept.length) chat.more = list.length >= PAGE;
  chat.active = new Set();
  for (const m of snapshot.values()) settle(m);
  // Replay with the terminal's guard (snapshotIsNewer, session_load.go:567-572):
  // a buffered event never rolls back a message the snapshot shows finished,
  // nor one the snapshot has in a newer version.
  for (const { kind, m } of events) {
    if (m.session_id !== sid) continue;
    const snap = snapshot.get(m.id);
    if (snap && kind !== "deleted" && snapshotIsNewer(snap, m)) continue;
    if (kind === "deleted") drop(chat, m.id);
    else put(chat, m);
  }
  chat.status = "ready";
  chat.error = null;
}

function snapshotIsNewer(snap, ev) {
  if (isFinished(snap) && !isFinished(ev)) return true;
  return Boolean(snap.updated_at) && Boolean(ev.updated_at) && snap.updated_at > ev.updated_at;
}

// Streaming sends one update per chunk: a buffer keeps only the latest of a
// run of updates to one message (holdSessionLoadEvent, session_load.go:208-216).
function buffer(token, kind, m) {
  const events = token.events;
  const last = events[events.length - 1];
  if (kind === "updated" && last?.kind === "updated" && last.m.id === m.id) events[events.length - 1] = { kind, m };
  else events.push({ kind, m });
}

// Drops a chat this window no longer shows, with its agent tools' chats.
export function unload(sid) {
  const chat = messages.bySession[sid];
  loads.delete(sid);
  if (!chat) return;
  const children = agentCallsIn(sid);
  delete messages.bySession[sid];
  for (const child of children) unload(child);
}

// An empty chat that is loaded as it is (a chat this window just created).
export function readyChat(sid) {
  const chat = chatFor(sid);
  if (chat.status !== "ready") chat.status = "ready";
  return chat;
}

// -- Agent tools' chats (CHAT-078; loadNestedTools, session_load.go:401-476) --

function agentCallsIn(sid) {
  const chat = messages.bySession[sid];
  if (!chat) return [];
  const out = [];
  for (const id of chat.ids) {
    const m = chat.byId.get(id);
    if (m?.role !== "assistant") continue;
    for (const call of toolCalls(m)) {
      if (call.id && AGENT_TOOLS.has(call.name)) out.push(agentChatId(m.id, call.id));
    }
  }
  return out;
}

const AGENT_CHAT_FETCHES = 8; // nestedFetchConcurrency, session_load.go:27-29

// One level at a time, at most 8 reads at once; agents within agents follow.
async function loadAgentChats(sid, { onlyNew = false } = {}) {
  let level = agentCallsIn(sid);
  if (onlyNew) level = level.filter((child) => !messages.bySession[child]);
  while (level.length > 0) {
    const next = [];
    let i = 0;
    const worker = async () => {
      while (i < level.length) {
        const child = level[i++];
        const loaded = await load(child).catch(() => false);
        if (loaded) next.push(...agentCallsIn(child));
      }
    };
    await Promise.all(Array.from({ length: Math.min(AGENT_CHAT_FETCHES, level.length) }, worker));
    level = next;
  }
}

// The loaded chat whose message started this agent chat, or "".
function parentChat(sid) {
  const parsed = parseAgentChatId(sid);
  return parsed ? chatOf(parsed.messageId) : "";
}

// A live event of an agent chat (handleChildSessionMessage, internal/ui/model/
// transcript.go:225-264): it lands when the agent's call is in a loaded chat,
// and it shows that the agent's reply is still being written.
function applyAgentChatMessage(kind, m) {
  const sid = m.session_id;
  if (!messages.bySession[sid] && !loads.has(sid)) {
    if (!parentChat(sid)) {
      if (loads.size > 0) heldAgentEvents.set(m.id, { kind, m });
      return;
    }
    readyChat(sid);
  }
  const parent = parentChat(sid);
  if (parent) {
    const { messageId } = parseAgentChatId(sid);
    const chat = messages.bySession[parent];
    const call = chat.byId.get(messageId);
    if (call && kind !== "deleted") setActive(chat, call);
  }
  applyToChat(sid, kind, m);
}

function retryHeldAgentEvents() {
  if (heldAgentEvents.size === 0) return;
  const held = [...heldAgentEvents.values()];
  heldAgentEvents.clear();
  for (const { kind, m } of held) applyAgentChatMessage(kind, m);
  if (loads.size === 0) heldAgentEvents.clear();
}

// -- Events --

function applyToChat(sid, kind, m) {
  const token = loads.get(sid);
  const chat = messages.bySession[sid];
  if (token) buffer(token, kind, m);
  if (chat?.status === "ready") {
    if (kind === "deleted") drop(chat, m.id);
    else put(chat, m);
    return true;
  }
  return Boolean(token);
}

// A `message` event (1.5.3): upsert or remove in a loaded chat (or buffered
// while it loads); for a known chat that is not loaded, only the live flag and
// the list place; a chat this window has not seen is dropped.
export function applyMessage(event, m) {
  const sid = m?.session_id;
  if (!m?.id || !sid) return;
  if (parseAgentChatId(sid)) {
    applyAgentChatMessage(event, m);
    return;
  }
  const taken = applyToChat(sid, event, m);
  noteChatMessage(sid, event, m, { loaded: taken });
}

onEvent("message", applyMessage);
// Another folder's drawn chats stay current (TimelineStack).
onFolderEvent("message", (event, m) => applyMessage(event, m));

// tree_changed for the shown chat reloads it (1.5.3); the path may differ.
onEvent("tree_changed", (_kind, change) => {
  const sid = change?.session_id;
  if (sid && sid === sessions.currentId && sessions.byId[sid]) load(sid).catch(() => {});
});

// agent_event needs nothing: an `error` shows no notice, as in the terminal
// (internal/ui/model/agent_events.go:167-170), because the run's assistant
// message carries the outcome; the engine sends no `summarize` type over the
// stream, and compaction shows from the messages themselves.

// -- Interrupts (CHAT-026; interruptAgent and CancelRunning,
// internal/ui/model/send.go:243-302, internal/ui/model/chat.go:568-594) --

// The shown copy of a message: an interrupted message that has no finish part
// gets a canceled one, so a late chunk of that run reopens nothing
// (withImmediateCancel, internal/ui/model/immediate_cancel.go:12-24). The
// copy is made once per message object.
const interruptedCopies = new WeakMap();
export function withInterrupt(m) {
  if (!m || !interrupted.has(m.id) || isFinished(m)) return m;
  let copy = interruptedCopies.get(m);
  if (!copy) {
    const finish = { type: "finish", data: { reason: "canceled", time: Math.floor(Date.now() / 1000), message: "", details: "" } };
    copy = { ...m, parts: [...(m.parts ?? []), finish] };
    interruptedCopies.set(m, copy);
  }
  return copy;
}

export function isInterrupted(mid) {
  return interrupted.has(mid);
}

// A step of an interrupted run that has only tool calls and no text stays
// hidden when it first shows after the interrupt (transcript.go:170-174).
export function isHiddenInterruptedStep(m, shownBefore) {
  return interrupted.has(m?.id) && !shownBefore && !(partsOf(m, "text")[0]?.data?.text ?? "") && toolCalls(m).length > 0;
}

// Runs just before this window posts the interrupt: the run's unfinished
// replies and its tools without a result stop at once; with prompts to send
// next (queued in the engine, or this window's that the engine took or that
// were sent while the chat was busy, `queued`) the chat stays busy and the held ones join
// the chat, else it is idle now. As the terminal cancels only the submissions
// in flight that were not queued (send.go:257-270), a send that would start a
// run and still waits for its 202 counts for nothing; a held one counts.
export function interruptChat(sid) {
  const chat = messages.bySession[sid];
  if (chat) {
    const answered = new Set();
    for (const id of chat.ids) for (const r of toolResults(chat.byId.get(id))) if (r.tool_call_id) answered.add(r.tool_call_id);
    for (const id of chat.ids) {
      const m = chat.byId.get(id);
      if (m?.role !== "assistant") continue;
      let stopped = !isFinished(m);
      for (const call of toolCalls(m)) {
        if (!call.id || answered.has(call.id) || prompts.callStatus[call.id] === "cancelled") continue;
        prompts.callStatus[call.id] = "cancelled";
        stopped = true;
      }
      if (stopped && !interrupted.has(m.id)) interrupted.add(m.id);
    }
    if ([...chat.active].some((id) => interrupted.has(id))) chat.active = new Set([...chat.active].filter((id) => !interrupted.has(id)));
  }
  const continuing = (queue.bySession?.[sid]?.count ?? 0) > 0 || Object.values(messages.pending).some((p) => p.sessionId === sid && (p.acknowledged || p.queued));
  if (continuing) releaseHeld(sid);
  else settleBusy(sid);
}

onInterrupt(interruptChat);

// -- This window's prompts (TYPE-042, TYPE-044, TYPE-048) --

// A sent prompt, shown at once with "Working" (or only in the tray's queue
// when it was held: sent while the chat was busy, TYPE-044) until its saved
// copy arrives. Returns its submission id, which the send carries.
export function addPending({ sessionId, text = "", attachments = [], held = false, submissionId = uuid() }) {
  messages.pending[submissionId] = { submissionId, sessionId, text, attachments, held, queued: held, state: "working", acknowledged: false, createdAt: Date.now() };
  return submissionId;
}

// queued: sent while the chat was busy; it stays when the item is released
// (the terminal's submission.queued, prompt_queue.go:47-52). acknowledged: the
// send carrying this submission id got its 202. interruptChat reads both.
onSend((body, call) => {
  const id = body?.submission_id;
  if (!id) return;
  Promise.resolve(call).then(
    () => {
      if (messages.pending[id]) messages.pending[id].acknowledged = true;
    },
    () => {},
  );
});

function pendingOf(sid, held) {
  return Object.values(messages.pending)
    .filter((p) => p.sessionId === sid && p.held === held)
    .sort((a, b) => a.createdAt - b.createdAt);
}

// The chat's pending items in the timeline, in send order.
export function pendingFor(sid) {
  return pendingOf(sid, false);
}

// The chat's held prompts, which only the tray's queue shows.
export function heldFor(sid) {
  return pendingOf(sid, true);
}

// TYPE-048: the send's answer was lost on the way ("This message may not have
// been sent"); the item stays.
export function markUncertain(submissionId) {
  if (messages.pending[submissionId]) messages.pending[submissionId].state = "uncertain";
}

export function takePending(submissionId) {
  const item = messages.pending[submissionId];
  if (!item) return null;
  delete messages.pending[submissionId];
  return { ...item, attachments: [...(item.attachments ?? [])] };
}

// An interrupt sends the queue now: held prompts join the chat
// (releaseHeldPrompts, pending_prompt.go:45-68).
export function releaseHeld(sid) {
  for (const item of Object.values(messages.pending)) {
    if (item.sessionId === sid && item.held) item.held = false;
  }
}

// Gives a prompt back to the field of chat sid ("" is the landing page); the
// composer takes it when it shows that chat (recallDraft and
// restoreRecalledDrafts, internal/ui/model/prompt_queue.go:115-120,
// history.go:155-181).
export function returnDraft(sid, draft) {
  messages.returned[sid] = [...(messages.returned[sid] ?? []), { text: draft?.text ?? "", attachments: [...(draft?.attachments ?? [])] }];
}

export function takeReturned(sid) {
  const drafts = messages.returned[sid] ?? [];
  delete messages.returned[sid];
  return drafts.map((d) => ({ text: d.text, attachments: [...d.attachments] }));
}

// finishPendingPrompt (pending_prompt.go:82-100): removes the item, and with
// restore gives its prompt back.
export function finishPending(submissionId, restore) {
  const item = takePending(submissionId);
  if (item && restore) returnDraft(item.sessionId, item);
  return item;
}

function errorText(error) {
  return error?.message ?? String(error);
}

// resolvePendingPrompt and applyResolvedPrompt (pending_prompt.go:110-148):
// after a run failed, the saved messages tell whether the prompt was saved;
// only a prompt that was never saved goes back (restoreIfMissing).
export async function resolvePending(submissionId, restoreIfMissing) {
  const item = messages.pending[submissionId];
  if (!item) return;
  let list;
  try {
    list = await api.messages(item.sessionId);
  } catch (error) {
    notify("error", errorText(error));
    return;
  }
  const saved = (Array.isArray(list) ? list : []).find((m) => m?.role === "user" && submissionOf(m) === submissionId);
  if (saved) {
    const chat = messages.bySession[item.sessionId];
    if (chat?.status === "ready") put(chat, saved, { live: false });
    delete messages.pending[submissionId];
    return;
  }
  if (restoreIfMissing) finishPending(submissionId, true);
}

// A run's end settles this window's prompt (internal/ui/model/
// update_workspace.go:33-38): a stop removes it; a failure checks whether it
// was saved. Neither shows a notice (1.5.3).
onEvent("run_complete", (_kind, done) => {
  const submission = done?.submission_id;
  if (!submission || !messages.pending[submission]) return;
  if (done.cancelled) finishPending(submission, false);
  else if (done.error) resolvePending(submission, true);
});

// After a reconnect the shown chat reloads (SIDE-030); a chat being opened is
// opened again by the sessions store.
onResync(async () => {
  const sid = sessions.currentId;
  if (sid && !sessions.opening) await load(sid);
});
